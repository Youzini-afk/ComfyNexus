package server

import (
	"archive/zip"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/Youzini-afk/ComfyNexus/internal/errs"
	"github.com/Youzini-afk/ComfyNexus/internal/sftpx"
	"github.com/Youzini-afk/ComfyNexus/internal/sshmgr"
	"github.com/pkg/sftp"
)

var comfyModelDirs = []struct {
	Type string
	Rel  string
}{
	{Type: "checkpoint", Rel: "/models/checkpoints"},
	{Type: "lora", Rel: "/models/loras"},
	{Type: "vae", Rel: "/models/vae"},
	{Type: "controlnet", Rel: "/models/controlnet"},
	{Type: "upscale", Rel: "/models/upscale_models"},
	{Type: "embedding", Rel: "/models/embeddings"},
	{Type: "unet", Rel: "/models/unet"},
	{Type: "clip", Rel: "/models/clip"},
	{Type: "clip_vision", Rel: "/models/clip_vision"},
}

var comfyModelExts = map[string]bool{
	".safetensors": true,
	".ckpt":        true,
	".pt":          true,
	".pth":         true,
	".bin":         true,
	".gguf":        true,
}

var comfyImageExts = map[string]bool{
	".png":  true,
	".jpg":  true,
	".jpeg": true,
	".webp": true,
}

type apiModel struct {
	ID               int64           `json:"id"`
	InstanceID       int64           `json:"instanceId"`
	Type             string          `json:"type"`
	ModelType        string          `json:"modelType"`
	Filename         string          `json:"filename"`
	RelPath          string          `json:"relPath"`
	Path             string          `json:"path"`
	SizeBytes        int64           `json:"sizeBytes"`
	SHA256           string          `json:"sha256,omitempty"`
	CivitaiModelID   *int64          `json:"civitaiModelId,omitempty"`
	CivitaiVersionID *int64          `json:"civitaiVersionId,omitempty"`
	HFRepo           string          `json:"hfRepo,omitempty"`
	HFFile           string          `json:"hfFile,omitempty"`
	TriggerWords     []string        `json:"triggerWords,omitempty"`
	BaseModel        string          `json:"baseModel,omitempty"`
	Metadata         json.RawMessage `json:"metadata,omitempty"`
	ThumbnailPath    string          `json:"thumbnailPath,omitempty"`
	Tags             []string        `json:"tags,omitempty"`
	MTime            string          `json:"mtime,omitempty"`
	ScannedAt        string          `json:"scannedAt,omitempty"`
	CivitaiSyncedAt  string          `json:"civitaiSyncedAt,omitempty"`
}

type apiImage struct {
	ID             int64           `json:"id"`
	InstanceID     int64           `json:"instanceId"`
	Filename       string          `json:"filename"`
	RelPath        string          `json:"relPath"`
	Path           string          `json:"path"`
	SizeBytes      int64           `json:"sizeBytes"`
	MTime          string          `json:"mtime"`
	Width          *int64          `json:"width,omitempty"`
	Height         *int64          `json:"height,omitempty"`
	Prompt         string          `json:"prompt,omitempty"`
	NegativePrompt string          `json:"negativePrompt,omitempty"`
	WorkflowJSON   json.RawMessage `json:"workflowJson,omitempty"`
	ParamsJSON     json.RawMessage `json:"paramsJson,omitempty"`
	UsedModels     []string        `json:"usedModels,omitempty"`
	ThumbnailPath  string          `json:"thumbnailPath,omitempty"`
	Favorited      bool            `json:"favorited"`
	Tags           []string        `json:"tags,omitempty"`
	CreatedAt      string          `json:"createdAt,omitempty"`
}

type remoteLibraryFile struct {
	Path    string
	Size    int64
	ModTime time.Time
}

func (s *Server) scanModels(w http.ResponseWriter, r *http.Request) {
	active, err := s.loadActiveInstance(r.Context())
	if err != nil {
		errs.Write(w, err)
		return
	}
	jobID, err := s.insertLibraryJob(r.Context(), "models_scan", active.Target.ID, map[string]any{"root": active.ComfyRoot})
	if err != nil {
		errs.Write(w, err)
		return
	}
	go s.runModelScan(context.Background(), jobID, active)
	writeJSON(w, http.StatusAccepted, map[string]any{"jobId": jobID, "status": "pending"})
}

func (s *Server) listModels(w http.ResponseWriter, r *http.Request) {
	active, err := s.loadActiveInstance(r.Context())
	if err != nil {
		errs.Write(w, err)
		return
	}
	q := r.URL.Query()
	where := []string{"instance_id=?"}
	args := []any{active.Target.ID}
	if typ := strings.TrimSpace(q.Get("type")); typ != "" {
		where = append(where, "model_type=?")
		args = append(args, typ)
	}
	if search := strings.TrimSpace(q.Get("q")); search != "" {
		like := "%" + search + "%"
		where = append(where, "(filename LIKE ? OR rel_path LIKE ? OR COALESCE(metadata_json,'') LIKE ?)")
		args = append(args, like, like, like)
	}
	if tag := strings.TrimSpace(q.Get("tag")); tag != "" {
		where = append(where, "COALESCE(tags,'') LIKE ?")
		args = append(args, "%"+tag+"%")
	}
	rows, err := s.DB.QueryContext(r.Context(), modelSelectSQL+" WHERE "+strings.Join(where, " AND ")+" ORDER BY model_type, filename", args...)
	if err != nil {
		errs.Write(w, err)
		return
	}
	defer rows.Close()
	models, err := scanAPIModels(rows)
	if err != nil {
		errs.Write(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"models": models})
}

func (s *Server) getModel(w http.ResponseWriter, r *http.Request) {
	model, err := s.loadAPIModel(r.Context(), r)
	if err != nil {
		errs.Write(w, err)
		return
	}
	writeJSON(w, http.StatusOK, model)
}

func (s *Server) syncModelCivitai(w http.ResponseWriter, r *http.Request) {
	model, err := s.loadAPIModel(r.Context(), r)
	if err != nil {
		errs.Write(w, err)
		return
	}
	if model.SHA256 == "" {
		errs.Write(w, errs.New(errs.CodeBadRequest, http.StatusBadRequest, "model has no sha256"))
		return
	}
	updated, err := s.syncCivitai(r.Context(), model.ID, model.SHA256)
	if err != nil {
		errs.Write(w, err)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) deleteModel(w http.ResponseWriter, r *http.Request) {
	active, err := s.loadActiveInstance(r.Context())
	if err != nil {
		errs.Write(w, err)
		return
	}
	model, err := s.loadAPIModelForActive(r.Context(), r, active.Target.ID)
	if err != nil {
		errs.Write(w, err)
		return
	}
	c, err := s.sftpForActive(r)
	if err != nil {
		errs.Write(w, err)
		return
	}
	defer c.Close()
	if err := c.Remove(comfyRemotePath(active.ComfyRoot, model.RelPath)); err != nil {
		errs.Write(w, errs.New(errs.CodeBadRequest, http.StatusBadRequest, err.Error()))
		return
	}
	if _, err := s.DB.ExecContext(r.Context(), `DELETE FROM models WHERE id=? AND instance_id=?`, model.ID, active.Target.ID); err != nil {
		errs.Write(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) modelDiskUsage(w http.ResponseWriter, r *http.Request) {
	active, err := s.loadActiveInstance(r.Context())
	if err != nil {
		errs.Write(w, err)
		return
	}
	rows, err := s.DB.QueryContext(r.Context(), `SELECT model_type, COALESCE(SUM(size_bytes),0), COUNT(*) FROM models WHERE instance_id=? GROUP BY model_type ORDER BY model_type`, active.Target.ID)
	if err != nil {
		errs.Write(w, err)
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var typ string
		var totalSize, count int64
		if err := rows.Scan(&typ, &totalSize, &count); err != nil {
			errs.Write(w, err)
			return
		}
		items = append(items, map[string]any{"type": typ, "totalSize": totalSize, "count": count})
	}
	if err := rows.Err(); err != nil {
		errs.Write(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) scanImages(w http.ResponseWriter, r *http.Request) {
	active, err := s.loadActiveInstance(r.Context())
	if err != nil {
		errs.Write(w, err)
		return
	}
	jobID, err := s.insertLibraryJob(r.Context(), "images_scan", active.Target.ID, map[string]any{"root": active.ComfyRoot})
	if err != nil {
		errs.Write(w, err)
		return
	}
	go s.runImageScan(context.Background(), jobID, active)
	writeJSON(w, http.StatusAccepted, map[string]any{"jobId": jobID, "status": "pending"})
}

func (s *Server) listImages(w http.ResponseWriter, r *http.Request) {
	active, err := s.loadActiveInstance(r.Context())
	if err != nil {
		errs.Write(w, err)
		return
	}
	q := r.URL.Query()
	where := []string{"instance_id=?"}
	args := []any{active.Target.ID}
	if from := strings.TrimSpace(q.Get("from")); from != "" {
		where = append(where, "mtime>=?")
		args = append(args, from)
	}
	if to := strings.TrimSpace(q.Get("to")); to != "" {
		where = append(where, "mtime<=?")
		args = append(args, to)
	}
	if search := strings.TrimSpace(q.Get("q")); search != "" {
		like := "%" + search + "%"
		where = append(where, "(filename LIKE ? OR rel_path LIKE ? OR COALESCE(prompt,'') LIKE ? OR COALESCE(workflow_json,'') LIKE ?)")
		args = append(args, like, like, like, like)
	}
	if fav := strings.TrimSpace(q.Get("favorited")); fav != "" {
		where = append(where, "favorited=?")
		args = append(args, truthy(fav))
	}
	rows, err := s.DB.QueryContext(r.Context(), imageSelectSQL+" WHERE "+strings.Join(where, " AND ")+" ORDER BY mtime DESC", args...)
	if err != nil {
		errs.Write(w, err)
		return
	}
	defer rows.Close()
	images, err := scanAPIImages(rows)
	if err != nil {
		errs.Write(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"images": images})
}

func (s *Server) getImage(w http.ResponseWriter, r *http.Request) {
	img, err := s.loadAPIImage(r.Context(), r)
	if err != nil {
		errs.Write(w, err)
		return
	}
	writeJSON(w, http.StatusOK, img)
}

func (s *Server) getImageWorkflow(w http.ResponseWriter, r *http.Request) {
	img, err := s.loadAPIImage(r.Context(), r)
	if err != nil {
		errs.Write(w, err)
		return
	}
	var workflow any
	if len(img.WorkflowJSON) > 0 {
		workflow = img.WorkflowJSON
	}
	writeJSON(w, http.StatusOK, map[string]any{"workflowJson": workflow})
}

type favoriteImageRequest struct {
	Favorited bool `json:"favorited"`
}

func (s *Server) favoriteImage(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDParam(r, "id")
	if err != nil {
		errs.Write(w, err)
		return
	}
	active, err := s.loadActiveInstance(r.Context())
	if err != nil {
		errs.Write(w, err)
		return
	}
	var req favoriteImageRequest
	if err := decodeJSON(r, &req); err != nil {
		errs.Write(w, err)
		return
	}
	res, err := s.DB.ExecContext(r.Context(), `UPDATE images SET favorited=? WHERE id=? AND instance_id=?`, boolInt(req.Favorited), id, active.Target.ID)
	if err != nil {
		errs.Write(w, err)
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		errs.Write(w, errs.New(errs.CodeNotFound, http.StatusNotFound, "image not found"))
		return
	}
	img, err := s.loadAPIImageByID(r.Context(), id, active.Target.ID)
	if err != nil {
		errs.Write(w, err)
		return
	}
	writeJSON(w, http.StatusOK, img)
}

func (s *Server) deleteImage(w http.ResponseWriter, r *http.Request) {
	active, err := s.loadActiveInstance(r.Context())
	if err != nil {
		errs.Write(w, err)
		return
	}
	img, err := s.loadAPIImageForActive(r.Context(), r, active.Target.ID)
	if err != nil {
		errs.Write(w, err)
		return
	}
	c, err := s.sftpForActive(r)
	if err != nil {
		errs.Write(w, err)
		return
	}
	defer c.Close()
	if err := c.Remove(comfyRemotePath(active.ComfyRoot, img.RelPath)); err != nil {
		errs.Write(w, errs.New(errs.CodeBadRequest, http.StatusBadRequest, err.Error()))
		return
	}
	if _, err := s.DB.ExecContext(r.Context(), `DELETE FROM images WHERE id=? AND instance_id=?`, img.ID, active.Target.ID); err != nil {
		errs.Write(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type batchZipRequest struct {
	IDs []int64 `json:"ids"`
}

func (s *Server) zipImages(w http.ResponseWriter, r *http.Request) {
	var req batchZipRequest
	if err := decodeJSON(r, &req); err != nil {
		errs.Write(w, err)
		return
	}
	if len(req.IDs) == 0 {
		errs.Write(w, errs.New(errs.CodeBadRequest, http.StatusBadRequest, "ids is required"))
		return
	}
	active, err := s.loadActiveInstance(r.Context())
	if err != nil {
		errs.Write(w, err)
		return
	}
	images := make([]apiImage, 0, len(req.IDs))
	for _, id := range req.IDs {
		if id <= 0 {
			errs.Write(w, errs.New(errs.CodeBadRequest, http.StatusBadRequest, "bad id"))
			return
		}
		img, err := s.loadAPIImageByID(r.Context(), id, active.Target.ID)
		if err != nil {
			errs.Write(w, err)
			return
		}
		images = append(images, img)
	}
	c, err := s.sftpForActive(r)
	if err != nil {
		errs.Write(w, err)
		return
	}
	defer c.Close()
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="comfynexus-images.zip"`)
	zw := zip.NewWriter(w)
	for _, img := range images {
		if err := s.addImageToZip(active, c.Client, zw, img); err != nil {
			_ = zw.Close()
			return
		}
	}
	_ = zw.Close()
}

func (s *Server) runModelScan(ctx context.Context, jobID int64, active activeInstance) {
	s.updateLibraryJob(ctx, jobID, "running", 0, 0, "connecting", "", false)
	sshClient, err := s.SSH.Get(ctx, active.Target)
	if err != nil {
		s.failLibraryJob(ctx, jobID, "cannot connect to active instance: "+err.Error())
		return
	}
	c, err := sftpx.NewClient(sshClient)
	if err != nil {
		s.failLibraryJob(ctx, jobID, "cannot open sftp: "+err.Error())
		return
	}
	defer c.Close()
	seen := map[string]bool{}
	progress, total := int64(0), int64(0)
	for _, dir := range comfyModelDirs {
		files := collectLibraryFiles(c, comfyRemotePath(active.ComfyRoot, dir.Rel), comfyModelExts)
		total += int64(len(files))
		s.updateLibraryJob(ctx, jobID, "running", progress, total, "scanning "+dir.Rel, "", false)
		for _, f := range files {
			rel := comfyRelPath(active.ComfyRoot, f.Path)
			seen[rel] = true
			sha := s.cachedOrRemoteSHA256(ctx, active.Target, rel, f)
			mtime := f.ModTime.UTC().Format(time.RFC3339)
			_, _ = s.DB.ExecContext(ctx, `INSERT INTO models(instance_id, model_type, filename, rel_path, size_bytes, sha256, remote_mtime, scanned_at)
				VALUES(?,?,?,?,?,?,?,CURRENT_TIMESTAMP)
				ON CONFLICT(instance_id, rel_path) DO UPDATE SET model_type=excluded.model_type, filename=excluded.filename, size_bytes=excluded.size_bytes, sha256=COALESCE(excluded.sha256, models.sha256), remote_mtime=excluded.remote_mtime, scanned_at=CURRENT_TIMESTAMP`, active.Target.ID, dir.Type, path.Base(rel), rel, f.Size, nullString(sha), mtime)
			progress++
			s.updateLibraryJob(ctx, jobID, "running", progress, total, "indexed "+rel, "", false)
		}
	}
	s.pruneMissingModels(ctx, active.Target.ID, seen)
	s.updateLibraryJob(ctx, jobID, "done", progress, total, "model scan complete", "", true)
}

func (s *Server) runImageScan(ctx context.Context, jobID int64, active activeInstance) {
	s.updateLibraryJob(ctx, jobID, "running", 0, 0, "connecting", "", false)
	sshClient, err := s.SSH.Get(ctx, active.Target)
	if err != nil {
		s.failLibraryJob(ctx, jobID, "cannot connect to active instance: "+err.Error())
		return
	}
	c, err := sftpx.NewClient(sshClient)
	if err != nil {
		s.failLibraryJob(ctx, jobID, "cannot open sftp: "+err.Error())
		return
	}
	defer c.Close()
	files := collectLibraryFiles(c, comfyRemotePath(active.ComfyRoot, "/output"), comfyImageExts)
	seen := map[string]bool{}
	total := int64(len(files))
	for i, f := range files {
		rel := comfyRelPath(active.ComfyRoot, f.Path)
		seen[rel] = true
		meta := readRemoteImageMetadata(c, f.Path)
		mtime := f.ModTime.UTC().Format(time.RFC3339)
		thumb := "/api/files/download?path=" + url.QueryEscape(f.Path)
		_, _ = s.DB.ExecContext(ctx, `INSERT INTO images(instance_id, filename, rel_path, mtime, size_bytes, width, height, prompt, negative_prompt, workflow_json, params_json, used_models, thumbnail_path)
			VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)
			ON CONFLICT(instance_id, rel_path) DO UPDATE SET filename=excluded.filename, mtime=excluded.mtime, size_bytes=excluded.size_bytes, width=excluded.width, height=excluded.height, prompt=excluded.prompt, negative_prompt=excluded.negative_prompt, workflow_json=excluded.workflow_json, params_json=excluded.params_json, used_models=excluded.used_models, thumbnail_path=excluded.thumbnail_path`, active.Target.ID, path.Base(rel), rel, mtime, f.Size, nullableInt(meta.Width), nullableInt(meta.Height), meta.Prompt, meta.NegativePrompt, nullString(meta.WorkflowJSON), nullString(meta.ParamsJSON), strings.Join(meta.UsedModels, ","), thumb)
		s.updateLibraryJob(ctx, jobID, "running", int64(i+1), total, "indexed "+rel, "", false)
	}
	s.pruneMissingImages(ctx, active.Target.ID, seen)
	s.updateLibraryJob(ctx, jobID, "done", total, total, "image scan complete", "", true)
}

func (s *Server) insertLibraryJob(ctx context.Context, typ string, instanceID int64, payload any) (int64, error) {
	body, _ := json.Marshal(payload)
	res, err := s.DB.ExecContext(ctx, `INSERT INTO jobs(type, status, payload_json, progress, total, message, instance_id) VALUES(?, 'pending', ?, 0, 0, 'queued', ?)`, typ, string(body), instanceID)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Server) updateLibraryJob(ctx context.Context, id int64, status string, progress, total int64, message, errMsg string, finish bool) {
	if status == "running" {
		_, _ = s.DB.ExecContext(ctx, `UPDATE jobs SET status=?, progress=?, total=?, message=?, error=NULL, started_at=COALESCE(started_at,CURRENT_TIMESTAMP) WHERE id=?`, status, progress, total, message, id)
		return
	}
	if finish {
		_, _ = s.DB.ExecContext(ctx, `UPDATE jobs SET status=?, progress=?, total=?, message=?, error=?, finished_at=CURRENT_TIMESTAMP WHERE id=?`, status, progress, total, message, nullString(errMsg), id)
		return
	}
	_, _ = s.DB.ExecContext(ctx, `UPDATE jobs SET status=?, progress=?, total=?, message=?, error=? WHERE id=?`, status, progress, total, message, nullString(errMsg), id)
}

func (s *Server) failLibraryJob(ctx context.Context, id int64, message string) {
	s.updateLibraryJob(ctx, id, "failed", 0, 0, "scan failed", message, true)
}

func collectLibraryFiles(c *sftp.Client, root string, exts map[string]bool) []remoteLibraryFile {
	files := []remoteLibraryFile{}
	var walk func(string)
	walk = func(dir string) {
		infos, err := c.ReadDir(dir)
		if err != nil {
			return
		}
		for _, info := range infos {
			name := info.Name()
			if strings.HasPrefix(name, ".") {
				continue
			}
			p := path.Join(dir, name)
			if info.IsDir() {
				walk(p)
				continue
			}
			if exts[strings.ToLower(path.Ext(name))] {
				files = append(files, remoteLibraryFile{Path: p, Size: info.Size(), ModTime: info.ModTime()})
			}
		}
	}
	walk(root)
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files
}

func (s *Server) cachedOrRemoteSHA256(ctx context.Context, target sshmgr.Target, rel string, f remoteLibraryFile) string {
	mtime := f.ModTime.UTC().Format(time.RFC3339)
	var oldSize int64
	var oldMTime, oldSHA string
	_ = s.DB.QueryRowContext(ctx, `SELECT size_bytes, COALESCE(remote_mtime,''), COALESCE(sha256,'') FROM models WHERE instance_id=? AND rel_path=?`, target.ID, rel).Scan(&oldSize, &oldMTime, &oldSHA)
	if oldSHA != "" && oldSize == f.Size && oldMTime == mtime {
		return oldSHA
	}
	sshClient, err := s.SSH.Get(ctx, target)
	if err != nil {
		return ""
	}
	sess, err := sshClient.NewSession()
	if err != nil {
		return ""
	}
	defer sess.Close()
	out, err := sess.Output("sha256sum -- " + shellQuote(f.Path))
	if err != nil {
		return ""
	}
	parts := strings.Fields(string(out))
	if len(parts) == 0 || len(parts[0]) != 64 {
		return ""
	}
	return strings.ToLower(parts[0])
}

func (s *Server) pruneMissingModels(ctx context.Context, instanceID int64, seen map[string]bool) {
	if len(seen) == 0 {
		return
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT id, rel_path FROM models WHERE instance_id=?`, instanceID)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var rel string
		if rows.Scan(&id, &rel) == nil && !seen[rel] {
			_, _ = s.DB.ExecContext(ctx, `DELETE FROM models WHERE id=?`, id)
		}
	}
}

func (s *Server) pruneMissingImages(ctx context.Context, instanceID int64, seen map[string]bool) {
	if len(seen) == 0 {
		return
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT id, rel_path FROM images WHERE instance_id=?`, instanceID)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var rel string
		if rows.Scan(&id, &rel) == nil && !seen[rel] {
			_, _ = s.DB.ExecContext(ctx, `DELETE FROM images WHERE id=?`, id)
		}
	}
}

const modelSelectSQL = `SELECT id, instance_id, model_type, filename, rel_path, size_bytes, COALESCE(sha256,''), civitai_model_id, civitai_version_id, COALESCE(hf_repo,''), COALESCE(hf_file,''), COALESCE(trigger_words,''), COALESCE(base_model,''), COALESCE(metadata_json,''), COALESCE(thumbnail_path,''), COALESCE(tags,''), COALESCE(remote_mtime,''), COALESCE(scanned_at,''), COALESCE(civitai_synced_at,'') FROM models`

func (s *Server) loadAPIModel(ctx context.Context, r *http.Request) (apiModel, error) {
	active, err := s.loadActiveInstance(ctx)
	if err != nil {
		return apiModel{}, err
	}
	return s.loadAPIModelForActive(ctx, r, active.Target.ID)
}

func (s *Server) loadAPIModelForActive(ctx context.Context, r *http.Request, instanceID int64) (apiModel, error) {
	id, err := parseIDParam(r, "id")
	if err != nil {
		return apiModel{}, err
	}
	return s.loadAPIModelByID(ctx, id, instanceID)
}

func (s *Server) loadAPIModelByID(ctx context.Context, id, instanceID int64) (apiModel, error) {
	row := s.DB.QueryRowContext(ctx, modelSelectSQL+` WHERE id=? AND instance_id=?`, id, instanceID)
	models, err := scanAPIModels(&singleSQLRow{row: row})
	if err != nil {
		return apiModel{}, err
	}
	if len(models) == 0 {
		return apiModel{}, errs.New(errs.CodeNotFound, http.StatusNotFound, "model not found")
	}
	return models[0], nil
}

func scanAPIModels(rows sqlRows) ([]apiModel, error) {
	models := []apiModel{}
	for rows.Next() {
		var m apiModel
		var civitaiModelID, civitaiVersionID sql.NullInt64
		var triggerWords, metadataJSON, tags string
		if err := rows.Scan(&m.ID, &m.InstanceID, &m.ModelType, &m.Filename, &m.RelPath, &m.SizeBytes, &m.SHA256, &civitaiModelID, &civitaiVersionID, &m.HFRepo, &m.HFFile, &triggerWords, &m.BaseModel, &metadataJSON, &m.ThumbnailPath, &tags, &m.MTime, &m.ScannedAt, &m.CivitaiSyncedAt); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return models, nil
			}
			return nil, err
		}
		m.Type = m.ModelType
		m.Path = m.RelPath
		if civitaiModelID.Valid {
			v := civitaiModelID.Int64
			m.CivitaiModelID = &v
		}
		if civitaiVersionID.Valid {
			v := civitaiVersionID.Int64
			m.CivitaiVersionID = &v
		}
		m.TriggerWords = splitCSV(triggerWords)
		m.Tags = splitCSV(tags)
		if metadataJSON != "" && json.Valid([]byte(metadataJSON)) {
			m.Metadata = json.RawMessage(metadataJSON)
		}
		models = append(models, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return models, nil
}

const imageSelectSQL = `SELECT id, instance_id, filename, rel_path, size_bytes, mtime, width, height, COALESCE(prompt,''), COALESCE(negative_prompt,''), COALESCE(workflow_json,''), COALESCE(params_json,''), COALESCE(used_models,''), COALESCE(thumbnail_path,''), favorited, COALESCE(tags,''), COALESCE(created_at,'') FROM images`

func (s *Server) loadAPIImage(ctx context.Context, r *http.Request) (apiImage, error) {
	active, err := s.loadActiveInstance(ctx)
	if err != nil {
		return apiImage{}, err
	}
	return s.loadAPIImageForActive(ctx, r, active.Target.ID)
}

func (s *Server) loadAPIImageForActive(ctx context.Context, r *http.Request, instanceID int64) (apiImage, error) {
	id, err := parseIDParam(r, "id")
	if err != nil {
		return apiImage{}, err
	}
	return s.loadAPIImageByID(ctx, id, instanceID)
}

func (s *Server) loadAPIImageByID(ctx context.Context, id, instanceID int64) (apiImage, error) {
	row := s.DB.QueryRowContext(ctx, imageSelectSQL+` WHERE id=? AND instance_id=?`, id, instanceID)
	images, err := scanAPIImages(&singleSQLRow{row: row})
	if err != nil {
		return apiImage{}, err
	}
	if len(images) == 0 {
		return apiImage{}, errs.New(errs.CodeNotFound, http.StatusNotFound, "image not found")
	}
	return images[0], nil
}

func scanAPIImages(rows sqlRows) ([]apiImage, error) {
	images := []apiImage{}
	for rows.Next() {
		var img apiImage
		var width, height sql.NullInt64
		var workflowJSON, paramsJSON, usedModels, tags string
		var favorited int
		if err := rows.Scan(&img.ID, &img.InstanceID, &img.Filename, &img.RelPath, &img.SizeBytes, &img.MTime, &width, &height, &img.Prompt, &img.NegativePrompt, &workflowJSON, &paramsJSON, &usedModels, &img.ThumbnailPath, &favorited, &tags, &img.CreatedAt); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return images, nil
			}
			return nil, err
		}
		img.Path = img.RelPath
		if width.Valid {
			v := width.Int64
			img.Width = &v
		}
		if height.Valid {
			v := height.Int64
			img.Height = &v
		}
		if workflowJSON != "" && json.Valid([]byte(workflowJSON)) {
			img.WorkflowJSON = json.RawMessage(workflowJSON)
		}
		if paramsJSON != "" && json.Valid([]byte(paramsJSON)) {
			img.ParamsJSON = json.RawMessage(paramsJSON)
		}
		img.UsedModels = splitCSV(usedModels)
		img.Tags = splitCSV(tags)
		img.Favorited = favorited != 0
		images = append(images, img)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return images, nil
}

type sqlRows interface {
	Next() bool
	Scan(...any) error
	Err() error
}

type singleSQLRow struct {
	row  *sql.Row
	done bool
}

func (r *singleSQLRow) Next() bool { return !r.done }
func (r *singleSQLRow) Err() error { return nil }
func (r *singleSQLRow) Scan(dest ...any) error {
	err := r.row.Scan(dest...)
	r.done = true
	return err
}

func (s *Server) syncCivitai(ctx context.Context, id int64, sha string) (apiModel, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://civitai.com/api/v1/model-versions/by-hash/"+url.PathEscape(sha), nil)
	if err != nil {
		return apiModel{}, err
	}
	if s.Cfg.CivitaiAPIKey != "" {
		req.Header.Set("Authorization", "Bearer "+s.Cfg.CivitaiAPIKey)
	}
	client := &http.Client{Timeout: 25 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return apiModel{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return apiModel{}, errs.New(errs.CodeNotFound, http.StatusNotFound, "civitai model version not found")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return apiModel{}, errs.New(errs.CodeBadRequest, http.StatusBadRequest, "civitai returned "+resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return apiModel{}, err
	}
	var meta map[string]any
	_ = json.Unmarshal(body, &meta)
	var civitaiVersionID, civitaiModelID any
	if v := jsonInt64(meta["id"]); v != 0 {
		civitaiVersionID = v
	}
	if modelObj, _ := meta["model"].(map[string]any); modelObj != nil {
		if v := jsonInt64(modelObj["id"]); v != 0 {
			civitaiModelID = v
		}
	}
	triggerWords := strings.Join(jsonStringSlice(meta["trainedWords"]), ",")
	baseModel, _ := meta["baseModel"].(string)
	thumbnail := civitaiThumbnailURL(meta)
	if _, err := s.DB.ExecContext(ctx, `UPDATE models SET civitai_model_id=?, civitai_version_id=?, trigger_words=?, base_model=?, metadata_json=?, thumbnail_path=?, civitai_synced_at=CURRENT_TIMESTAMP WHERE id=?`, civitaiModelID, civitaiVersionID, triggerWords, baseModel, string(body), thumbnail, id); err != nil {
		return apiModel{}, err
	}
	active, err := s.loadActiveInstance(ctx)
	if err != nil {
		return apiModel{}, err
	}
	return s.loadAPIModelByID(ctx, id, active.Target.ID)
}

type remoteImageMetadata struct {
	Width          int
	Height         int
	Prompt         string
	NegativePrompt string
	WorkflowJSON   string
	ParamsJSON     string
	UsedModels     []string
}

func readRemoteImageMetadata(c *sftp.Client, p string) remoteImageMetadata {
	f, err := c.Open(p)
	if err != nil {
		return remoteImageMetadata{}
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, 8<<20))
	if err != nil {
		return remoteImageMetadata{}
	}
	meta := remoteImageMetadata{}
	if cfg, _, err := image.DecodeConfig(bytes.NewReader(data)); err == nil {
		meta.Width = cfg.Width
		meta.Height = cfg.Height
	}
	if strings.EqualFold(path.Ext(p), ".png") {
		chunks := pngTextChunks(data)
		meta.Prompt = chunks["prompt"]
		meta.WorkflowJSON = chunks["workflow"]
		if json.Valid([]byte(meta.Prompt)) {
			meta.ParamsJSON = meta.Prompt
		}
		meta.UsedModels = extractUsedModels(meta.WorkflowJSON + "\n" + meta.Prompt)
	}
	return meta
}

func pngTextChunks(data []byte) map[string]string {
	out := map[string]string{}
	if len(data) < 8 || string(data[:8]) != "\x89PNG\r\n\x1a\n" {
		return out
	}
	for off := 8; off+12 <= len(data); {
		ln := int(data[off])<<24 | int(data[off+1])<<16 | int(data[off+2])<<8 | int(data[off+3])
		typ := string(data[off+4 : off+8])
		start, end := off+8, off+8+ln
		if ln < 0 || end+4 > len(data) {
			break
		}
		chunk := data[start:end]
		switch typ {
		case "tEXt":
			if i := bytes.IndexByte(chunk, 0); i > 0 {
				out[string(chunk[:i])] = string(chunk[i+1:])
			}
		case "iTXt":
			if key, text, ok := parseITXt(chunk); ok {
				out[key] = text
			}
		}
		if typ == "IEND" {
			break
		}
		off = end + 4
	}
	return out
}

func parseITXt(chunk []byte) (string, string, bool) {
	keyEnd := bytes.IndexByte(chunk, 0)
	if keyEnd <= 0 || keyEnd+3 >= len(chunk) {
		return "", "", false
	}
	key := string(chunk[:keyEnd])
	compressionFlag := chunk[keyEnd+1]
	if compressionFlag != 0 {
		return "", "", false
	}
	rest := chunk[keyEnd+3:]
	langEnd := bytes.IndexByte(rest, 0)
	if langEnd < 0 || langEnd+1 >= len(rest) {
		return "", "", false
	}
	rest = rest[langEnd+1:]
	translatedEnd := bytes.IndexByte(rest, 0)
	if translatedEnd < 0 || translatedEnd+1 > len(rest) {
		return "", "", false
	}
	return key, string(rest[translatedEnd+1:]), true
}

func (s *Server) addImageToZip(active activeInstance, c *sftp.Client, zw *zip.Writer, img apiImage) error {
	f, err := c.Open(comfyRemotePath(active.ComfyRoot, img.RelPath))
	if err != nil {
		return err
	}
	defer f.Close()
	name := strings.TrimPrefix(path.Clean("/"+strings.TrimPrefix(img.RelPath, "/")), "/")
	h := &zip.FileHeader{Name: name, Method: zip.Deflate}
	h.SetModTime(time.Now())
	w, err := zw.CreateHeader(h)
	if err != nil {
		return err
	}
	_, err = io.Copy(w, f)
	return err
}

func comfyRemotePath(root, rel string) string {
	rel = path.Clean("/" + strings.TrimPrefix(rel, "/"))
	if root == "" {
		return rel
	}
	return path.Join(root, rel)
}

func comfyRelPath(root, full string) string {
	full = path.Clean(full)
	if root != "" {
		root = path.Clean(root)
		if full == root {
			return "/"
		}
		if strings.HasPrefix(full, root+"/") {
			return path.Clean("/" + strings.TrimPrefix(full, root+"/"))
		}
	}
	return path.Clean("/" + strings.TrimPrefix(full, "/"))
}

func nullableInt(i int) any {
	if i == 0 {
		return nil
	}
	return i
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func truthy(s string) int {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "1" || s == "true" || s == "yes" || s == "on" {
		return 1
	}
	return 0
}

func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func jsonInt64(v any) int64 {
	switch n := v.(type) {
	case float64:
		return int64(n)
	case int64:
		return n
	case int:
		return int64(n)
	case json.Number:
		i, _ := n.Int64()
		return i
	default:
		return 0
	}
}

func jsonStringSlice(v any) []string {
	arr, _ := v.([]any)
	out := []string{}
	for _, item := range arr {
		if s, ok := item.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	return out
}

func civitaiThumbnailURL(meta map[string]any) string {
	images, _ := meta["images"].([]any)
	for _, item := range images {
		m, _ := item.(map[string]any)
		if u, _ := m["url"].(string); u != "" {
			return u
		}
	}
	return ""
}

func extractUsedModels(s string) []string {
	if s == "" {
		return nil
	}
	seen := map[string]bool{}
	out := []string{}
	for _, ext := range []string{".safetensors", ".ckpt", ".pt", ".pth"} {
		start := 0
		lower := strings.ToLower(s)
		for {
			i := strings.Index(lower[start:], ext)
			if i < 0 {
				break
			}
			end := start + i + len(ext)
			begin := end - len(ext)
			for begin > 0 {
				r := s[begin-1]
				if r == '"' || r == '\'' || r == '/' || r == '\\' || r == ':' || r == ',' || r == '[' || r == '{' || r == ' ' || r == '\n' || r == '\t' {
					break
				}
				begin--
			}
			name := strings.Trim(s[begin:end], `"' ,[]{}()`)
			if name != "" && !seen[name] {
				seen[name] = true
				out = append(out, name)
			}
			start = end
		}
	}
	return out
}
