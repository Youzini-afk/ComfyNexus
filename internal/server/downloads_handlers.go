package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/Youzini-afk/ComfyNexus/internal/errs"
	"github.com/Youzini-afk/ComfyNexus/internal/sftpx"
	"github.com/Youzini-afk/ComfyNexus/internal/sshmgr"
)

type createDownloadRequest struct {
	URL      string `json:"url"`
	DestPath string `json:"destPath"`
}

type downloadPayload struct {
	URL      string `json:"url"`
	DestPath string `json:"destPath"`
	PIDPath  string `json:"pidPath"`
	ExitPath string `json:"exitPath"`
	LogPath  string `json:"logPath"`
}

type downloadStatus struct {
	JobID      int64  `json:"jobId"`
	ID         int64  `json:"id"`
	Type       string `json:"type"`
	Status     string `json:"status"`
	URL        string `json:"url"`
	DestPath   string `json:"destPath"`
	Progress   int64  `json:"progress"`
	Total      int64  `json:"total"`
	Error      string `json:"error,omitempty"`
	Message    string `json:"message,omitempty"`
	CreatedAt  string `json:"createdAt,omitempty"`
	StartedAt  string `json:"startedAt,omitempty"`
	FinishedAt string `json:"finishedAt,omitempty"`
}

func (s *Server) createDownload(w http.ResponseWriter, r *http.Request) {
	var req createDownloadRequest
	if err := decodeJSON(r, &req); err != nil {
		errs.Write(w, err)
		return
	}
	if err := validateDownloadURL(req.URL); err != nil {
		errs.Write(w, err)
		return
	}
	dest, err := cleanRequestPath(req.DestPath, true)
	if err != nil {
		errs.Write(w, err)
		return
	}
	active, err := s.loadActiveInstance(r.Context())
	if err != nil {
		errs.Write(w, err)
		return
	}
	payload := downloadPayload{URL: req.URL, DestPath: dest}
	payloadJSON, _ := json.Marshal(payload)
	res, err := s.DB.ExecContext(r.Context(), `
		INSERT INTO jobs(type, status, payload_json, progress, total, message, instance_id)
		VALUES('download', 'pending', ?, 0, 0, 'queued', ?)`, string(payloadJSON), active.Target.ID)
	if err != nil {
		errs.Write(w, err)
		return
	}
	id, _ := res.LastInsertId()
	payload.PIDPath, payload.ExitPath, payload.LogPath = downloadMarkerPaths(dest, id)
	payloadJSON, _ = json.Marshal(payload)
	if _, err := s.DB.ExecContext(r.Context(), `UPDATE jobs SET payload_json=? WHERE id=?`, string(payloadJSON), id); err != nil {
		errs.Write(w, err)
		return
	}
	go s.runDownloadJob(context.Background(), id, active.Target, payload)
	writeJSON(w, http.StatusCreated, map[string]any{"jobId": id, "status": "pending"})
}

func (s *Server) listDownloads(w http.ResponseWriter, r *http.Request) {
	rows, err := s.DB.QueryContext(r.Context(), `SELECT id, status, payload_json, progress, total, COALESCE(instance_id,0), COALESCE(message,''), COALESCE(error,''), created_at, COALESCE(started_at,''), COALESCE(finished_at,'') FROM jobs WHERE type='download' ORDER BY id DESC`)
	if err != nil {
		errs.Write(w, err)
		return
	}
	defer rows.Close()
	out := []downloadStatus{}
	for rows.Next() {
		j, err := scanDownloadJob(rows)
		if err != nil {
			errs.Write(w, err)
			return
		}
		out = append(out, j.status())
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) getDownload(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDParam(r, "id")
	if err != nil {
		errs.Write(w, err)
		return
	}
	j, err := s.loadDownloadJob(r.Context(), id)
	if err != nil {
		errs.Write(w, err)
		return
	}
	writeJSON(w, http.StatusOK, j.status())
}

func (s *Server) cancelDownload(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDParam(r, "id")
	if err != nil {
		errs.Write(w, err)
		return
	}
	j, err := s.loadDownloadJob(r.Context(), id)
	if err != nil {
		errs.Write(w, err)
		return
	}
	if j.InstanceID > 0 {
		if target, err := s.loadTarget(r.Context(), j.InstanceID); err == nil {
			_ = s.killRemoteDownload(r.Context(), target, j.Payload.PIDPath)
		}
	}
	_, err = s.DB.ExecContext(r.Context(), `UPDATE jobs SET status='canceled', message='canceled', finished_at=CURRENT_TIMESTAMP WHERE id=? AND status IN ('pending','running')`, id)
	if err != nil {
		errs.Write(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"jobId": id, "status": "canceled"})
}

type downloadJob struct {
	ID         int64
	InstanceID int64
	Status     string
	Payload    downloadPayload
	Progress   int64
	Total      int64
	Message    string
	Error      string
	CreatedAt  string
	StartedAt  string
	FinishedAt string
}

func (j downloadJob) status() downloadStatus {
	return downloadStatus{JobID: j.ID, ID: j.ID, Type: "download", Status: j.Status, URL: j.Payload.URL, DestPath: j.Payload.DestPath, Progress: j.Progress, Total: j.Total, Error: j.Error, Message: j.Message, CreatedAt: j.CreatedAt, StartedAt: j.StartedAt, FinishedAt: j.FinishedAt}
}

type downloadScanner interface {
	Scan(dest ...any) error
}

func scanDownloadJob(scanner downloadScanner) (downloadJob, error) {
	var j downloadJob
	var payloadJSON string
	if err := scanner.Scan(&j.ID, &j.Status, &payloadJSON, &j.Progress, &j.Total, &j.InstanceID, &j.Message, &j.Error, &j.CreatedAt, &j.StartedAt, &j.FinishedAt); err != nil {
		return downloadJob{}, err
	}
	if err := json.Unmarshal([]byte(payloadJSON), &j.Payload); err != nil {
		return downloadJob{}, err
	}
	return j, nil
}

func (s *Server) loadDownloadJob(ctx context.Context, id int64) (downloadJob, error) {
	row := s.DB.QueryRowContext(ctx, `SELECT id, status, payload_json, progress, total, COALESCE(instance_id,0), COALESCE(message,''), COALESCE(error,''), created_at, COALESCE(started_at,''), COALESCE(finished_at,'') FROM jobs WHERE id=? AND type='download'`, id)
	j, err := scanDownloadJob(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return downloadJob{}, errs.New(errs.CodeNotFound, http.StatusNotFound, "download not found")
		}
		return downloadJob{}, err
	}
	return j, nil
}

func (s *Server) runDownloadJob(ctx context.Context, id int64, target sshmgr.Target, payload downloadPayload) {
	_, _ = s.DB.ExecContext(ctx, `UPDATE jobs SET status='running', message='starting remote download', started_at=CURRENT_TIMESTAMP WHERE id=?`, id)
	sshClient, err := s.SSH.Get(ctx, target)
	if err != nil {
		s.failDownload(ctx, id, "cannot connect to active instance")
		return
	}
	sftpClient, err := sftpx.NewClient(sshClient)
	if err != nil {
		s.failDownload(ctx, id, "cannot open sftp")
		return
	}
	if err := sftpx.EnsureParent(sftpClient, payload.DestPath); err != nil {
		_ = sftpClient.Close()
		s.failDownload(ctx, id, err.Error())
		return
	}
	_ = sftpClient.MkdirAll(path.Dir(payload.PIDPath))
	_ = sftpClient.Close()
	if err := s.startRemoteDownload(ctx, target, payload); err != nil {
		s.failDownload(ctx, id, "cannot start remote download")
		return
	}
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			s.failDownload(context.Background(), id, "download worker stopped")
			return
		case <-ticker.C:
		}
		j, err := s.loadDownloadJob(ctx, id)
		if err != nil || j.Status == "canceled" {
			return
		}
		progress := s.remoteFileSize(ctx, target, payload.DestPath)
		_, _ = s.DB.ExecContext(ctx, `UPDATE jobs SET progress=? WHERE id=? AND status='running'`, progress, id)
		exitCode, done := s.remoteExitCode(ctx, target, payload.ExitPath)
		if !done {
			continue
		}
		if strings.TrimSpace(exitCode) == "0" {
			_, _ = s.DB.ExecContext(ctx, `UPDATE jobs SET status='done', progress=?, message='download complete', finished_at=CURRENT_TIMESTAMP WHERE id=?`, progress, id)
		} else {
			s.failDownload(ctx, id, "remote download failed")
		}
		return
	}
}

func (s *Server) startRemoteDownload(ctx context.Context, target sshmgr.Target, payload downloadPayload) error {
	sshClient, err := s.SSH.Get(ctx, target)
	if err != nil {
		return err
	}
	sess, err := sshClient.NewSession()
	if err != nil {
		return err
	}
	defer sess.Close()
	dir := path.Dir(payload.DestPath)
	base := path.Base(payload.DestPath)
	inner := fmt.Sprintf(`echo $$ > %s; if command -v aria2c >/dev/null 2>&1; then aria2c -x 4 -s 4 -c -d %s -o %s -- %s; elif command -v curl >/dev/null 2>&1; then curl -L --fail -o %s -- %s; else wget -O %s -- %s; fi; rc=$?; echo $rc > %s; exit $rc`, shellQuote(payload.PIDPath), shellQuote(dir), shellQuote(base), shellQuote(payload.URL), shellQuote(payload.DestPath), shellQuote(payload.URL), shellQuote(payload.DestPath), shellQuote(payload.URL), shellQuote(payload.ExitPath))
	script := fmt.Sprintf(`mkdir -p %s %s; rm -f %s; nohup sh -c %s > %s 2>&1 &`, shellQuote(dir), shellQuote(path.Dir(payload.PIDPath)), shellQuote(payload.ExitPath), shellQuote(inner), shellQuote(payload.LogPath))
	return sess.Run(script)
}

func (s *Server) killRemoteDownload(ctx context.Context, target sshmgr.Target, pidPath string) error {
	sshClient, err := s.SSH.Get(ctx, target)
	if err != nil {
		return err
	}
	sess, err := sshClient.NewSession()
	if err != nil {
		return err
	}
	defer sess.Close()
	return sess.Run("pid=$(cat " + shellQuote(pidPath) + " 2>/dev/null || true); if [ -n \"$pid\" ]; then kill \"$pid\" 2>/dev/null || true; fi")
}

func (s *Server) remoteFileSize(ctx context.Context, target sshmgr.Target, p string) int64 {
	sshClient, err := s.SSH.Get(ctx, target)
	if err != nil {
		return 0
	}
	c, err := sftpx.NewClient(sshClient)
	if err != nil {
		return 0
	}
	defer c.Close()
	size, err := sftpx.StatSize(c, p)
	if err != nil {
		return 0
	}
	return size
}

func (s *Server) remoteExitCode(ctx context.Context, target sshmgr.Target, p string) (string, bool) {
	sshClient, err := s.SSH.Get(ctx, target)
	if err != nil {
		return "", false
	}
	c, err := sftpx.NewClient(sshClient)
	if err != nil {
		return "", false
	}
	defer c.Close()
	f, err := c.Open(p)
	if err != nil {
		return "", false
	}
	defer f.Close()
	buf := make([]byte, 32)
	n, _ := f.Read(buf)
	return string(buf[:n]), true
}

func (s *Server) failDownload(ctx context.Context, id int64, msg string) {
	_, _ = s.DB.ExecContext(ctx, `UPDATE jobs SET status='failed', error=?, message='download failed', finished_at=CURRENT_TIMESTAMP WHERE id=? AND status<>'canceled'`, msg, id)
}

func validateDownloadURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return errs.New(errs.CodeBadRequest, http.StatusBadRequest, "url must be absolute")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return errs.New(errs.CodeBadRequest, http.StatusBadRequest, "url scheme must be http or https")
	}
	return nil
}

func downloadMarkerPaths(dest string, id int64) (pidPath, exitPath, logPath string) {
	markerDir := path.Join(path.Dir(dest), ".comfynexus-downloads")
	base := strconv.FormatInt(id, 10)
	return path.Join(markerDir, base+".pid"), path.Join(markerDir, base+".exit"), path.Join(markerDir, base+".log")
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}
