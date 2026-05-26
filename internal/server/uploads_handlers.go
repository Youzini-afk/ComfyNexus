package server

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/Youzini-afk/ComfyNexus/internal/errs"
	"github.com/Youzini-afk/ComfyNexus/internal/sftpx"
	"github.com/go-chi/chi/v5"
)

type createUploadRequest struct {
	Path      string `json:"path"`
	TotalSize int64  `json:"totalSize"`
	ChunkSize int64  `json:"chunkSize,omitempty"`
}

type uploadPayload struct {
	Path      string `json:"path"`
	TempPath  string `json:"tempPath"`
	ChunkSize int64  `json:"chunkSize,omitempty"`
	NextChunk int64  `json:"nextChunk"`
}

type uploadStatus struct {
	JobID        int64  `json:"jobId"`
	ID           int64  `json:"id"`
	Type         string `json:"type"`
	Status       string `json:"status"`
	Path         string `json:"path"`
	TempPath     string `json:"tempPath,omitempty"`
	UploadedSize int64  `json:"uploadedSize"`
	TotalSize    int64  `json:"totalSize"`
	Progress     int64  `json:"progress"`
	ChunkSize    int64  `json:"chunkSize,omitempty"`
	NextChunk    int64  `json:"nextChunk"`
	Error        string `json:"error,omitempty"`
	Message      string `json:"message,omitempty"`
	CreatedAt    string `json:"createdAt,omitempty"`
	StartedAt    string `json:"startedAt,omitempty"`
	FinishedAt   string `json:"finishedAt,omitempty"`
}

func (s *Server) createUpload(w http.ResponseWriter, r *http.Request) {
	var req createUploadRequest
	if err := decodeJSON(r, &req); err != nil {
		errs.Write(w, err)
		return
	}
	p, err := cleanRequestPath(req.Path, true)
	if err != nil {
		errs.Write(w, err)
		return
	}
	if req.TotalSize < 0 {
		errs.Write(w, errs.New(errs.CodeBadRequest, http.StatusBadRequest, "totalSize must be non-negative"))
		return
	}
	active, err := s.loadActiveInstance(r.Context())
	if err != nil {
		errs.Write(w, err)
		return
	}
	payload := uploadPayload{Path: p, TempPath: p + ".comfynexus-upload", ChunkSize: req.ChunkSize, NextChunk: 0}
	if req.ChunkSize < 0 {
		errs.Write(w, errs.New(errs.CodeBadRequest, http.StatusBadRequest, "chunkSize must be non-negative"))
		return
	}
	if req.ChunkSize > s.Cfg.MaxUploadChunkBytes {
		errs.Write(w, errs.New(errs.CodeBadRequest, http.StatusBadRequest, fmt.Sprintf("chunkSize exceeds maximum of %d bytes", s.Cfg.MaxUploadChunkBytes)))
		return
	}
	payloadJSON, _ := json.Marshal(payload)
	res, err := s.DB.ExecContext(r.Context(), `
		INSERT INTO jobs(type, status, payload_json, progress, total, message, instance_id, started_at)
		VALUES('upload', 'running', ?, 0, ?, 'upload created', ?, CURRENT_TIMESTAMP)`, string(payloadJSON), req.TotalSize, active.Target.ID)
	if err != nil {
		errs.Write(w, err)
		return
	}
	id, _ := res.LastInsertId()
	writeJSON(w, http.StatusCreated, map[string]any{"jobId": id, "uploadedSize": 0})
}

func (s *Server) putUploadChunk(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDParam(r, "id")
	if err != nil {
		errs.Write(w, err)
		return
	}
	n, err := strconv.ParseInt(chi.URLParam(r, "n"), 10, 64)
	if err != nil || n < 0 {
		errs.Write(w, errs.New(errs.CodeBadRequest, http.StatusBadRequest, "bad chunk number"))
		return
	}
	job, err := s.loadUploadJob(r, id)
	if err != nil {
		errs.Write(w, err)
		return
	}
	if job.Status != "running" && job.Status != "pending" {
		errs.Write(w, errs.New(errs.CodeConflict, http.StatusConflict, "upload is not running"))
		return
	}
	if n != job.Payload.NextChunk {
		errs.Write(w, errs.New(errs.CodeConflict, http.StatusConflict, "chunks must be uploaded sequentially"))
		return
	}
	if r.ContentLength > s.Cfg.MaxUploadChunkBytes {
		errs.Write(w, errs.New(errs.CodeBadRequest, http.StatusBadRequest, fmt.Sprintf("upload chunk exceeds maximum of %d bytes", s.Cfg.MaxUploadChunkBytes)))
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, s.Cfg.MaxUploadChunkBytes)
	c, err := s.sftpForInstance(r.Context(), job.InstanceID)
	if err != nil {
		errs.Write(w, err)
		return
	}
	defer c.Close()
	if err := sftpx.EnsureParent(c.Client, job.Payload.TempPath); err != nil {
		errs.Write(w, errs.New(errs.CodeBadRequest, http.StatusBadRequest, err.Error()))
		return
	}
	f, err := sftpx.OpenAppend(c.Client, job.Payload.TempPath, int(n))
	if err != nil {
		errs.Write(w, errs.New(errs.CodeBadRequest, http.StatusBadRequest, err.Error()))
		return
	}
	written, copyErr := io.Copy(f, r.Body)
	closeErr := f.Close()
	if copyErr != nil {
		if strings.Contains(copyErr.Error(), "http: request body too large") {
			errs.Write(w, errs.New(errs.CodeBadRequest, http.StatusBadRequest, fmt.Sprintf("upload chunk exceeds maximum of %d bytes", s.Cfg.MaxUploadChunkBytes)))
			return
		}
		errs.Write(w, copyErr)
		return
	}
	if closeErr != nil {
		errs.Write(w, closeErr)
		return
	}
	job.Payload.NextChunk++
	newProgress := job.Progress + written
	if job.Total > 0 && newProgress > job.Total {
		errs.Write(w, errs.New(errs.CodeBadRequest, http.StatusBadRequest, "uploaded bytes exceed totalSize"))
		return
	}
	if err := s.updateUploadProgress(r, id, job.Payload, newProgress); err != nil {
		errs.Write(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"jobId": id, "uploadedSize": newProgress, "nextChunk": job.Payload.NextChunk})
}

func (s *Server) completeUpload(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDParam(r, "id")
	if err != nil {
		errs.Write(w, err)
		return
	}
	job, err := s.loadUploadJob(r, id)
	if err != nil {
		errs.Write(w, err)
		return
	}
	c, err := s.sftpForInstance(r.Context(), job.InstanceID)
	if err != nil {
		errs.Write(w, err)
		return
	}
	defer c.Close()
	size, err := sftpx.StatSize(c.Client, job.Payload.TempPath)
	if err != nil {
		errs.Write(w, errs.New(errs.CodeBadRequest, http.StatusBadRequest, err.Error()))
		return
	}
	if size != job.Total {
		errs.Write(w, errs.New(errs.CodeConflict, http.StatusConflict, "uploaded size does not match totalSize"))
		return
	}
	if err := sftpx.EnsureParent(c.Client, job.Payload.Path); err != nil {
		errs.Write(w, errs.New(errs.CodeBadRequest, http.StatusBadRequest, err.Error()))
		return
	}
	_ = c.Remove(job.Payload.Path)
	if err := c.Rename(job.Payload.TempPath, job.Payload.Path); err != nil {
		errs.Write(w, errs.New(errs.CodeBadRequest, http.StatusBadRequest, err.Error()))
		return
	}
	if _, err := s.DB.ExecContext(r.Context(), `UPDATE jobs SET status='done', progress=?, message='upload complete', finished_at=CURRENT_TIMESTAMP WHERE id=?`, size, id); err != nil {
		errs.Write(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"jobId": id, "status": "done", "uploadedSize": size})
}

func (s *Server) getUpload(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDParam(r, "id")
	if err != nil {
		errs.Write(w, err)
		return
	}
	job, err := s.loadUploadJob(r, id)
	if err != nil {
		errs.Write(w, err)
		return
	}
	writeJSON(w, http.StatusOK, job.status())
}

type uploadJob struct {
	ID         int64
	InstanceID int64
	Status     string
	Payload    uploadPayload
	Progress   int64
	Total      int64
	Message    string
	Error      string
	CreatedAt  string
	StartedAt  string
	FinishedAt string
}

func (j uploadJob) status() uploadStatus {
	return uploadStatus{JobID: j.ID, ID: j.ID, Type: "upload", Status: j.Status, Path: j.Payload.Path, TempPath: j.Payload.TempPath, UploadedSize: j.Progress, TotalSize: j.Total, Progress: j.Progress, ChunkSize: j.Payload.ChunkSize, NextChunk: j.Payload.NextChunk, Error: j.Error, Message: j.Message, CreatedAt: j.CreatedAt, StartedAt: j.StartedAt, FinishedAt: j.FinishedAt}
}

func (s *Server) loadUploadJob(r *http.Request, id int64) (uploadJob, error) {
	row := s.DB.QueryRowContext(r.Context(), `SELECT status, payload_json, progress, total, COALESCE(instance_id,0), COALESCE(message,''), COALESCE(error,''), created_at, COALESCE(started_at,''), COALESCE(finished_at,'') FROM jobs WHERE id=? AND type='upload'`, id)
	var j uploadJob
	j.ID = id
	var payloadJSON string
	if err := row.Scan(&j.Status, &payloadJSON, &j.Progress, &j.Total, &j.InstanceID, &j.Message, &j.Error, &j.CreatedAt, &j.StartedAt, &j.FinishedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return uploadJob{}, errs.New(errs.CodeNotFound, http.StatusNotFound, "upload not found")
		}
		return uploadJob{}, err
	}
	if err := json.Unmarshal([]byte(payloadJSON), &j.Payload); err != nil {
		return uploadJob{}, err
	}
	return j, nil
}

func (s *Server) updateUploadProgress(r *http.Request, id int64, payload uploadPayload, progress int64) error {
	payloadJSON, _ := json.Marshal(payload)
	_, err := s.DB.ExecContext(r.Context(), `UPDATE jobs SET status='running', payload_json=?, progress=?, message='uploading' WHERE id=?`, string(payloadJSON), progress, id)
	return err
}

func parseIDParam(r *http.Request, name string) (int64, error) {
	id, err := strconv.ParseInt(chi.URLParam(r, name), 10, 64)
	if err != nil || id <= 0 {
		return 0, errs.New(errs.CodeBadRequest, http.StatusBadRequest, "bad id")
	}
	return id, nil
}
