package server

import (
	"io"
	"net/http"
	"path"
	"strconv"

	"github.com/Youzini-afk/ComfyNexus/internal/errs"
	"github.com/Youzini-afk/ComfyNexus/internal/sftpx"
	"github.com/pkg/sftp"
)

type pathRequest struct {
	Path string `json:"path"`
}

type renameRequest struct {
	OldPath string `json:"oldPath"`
	NewPath string `json:"newPath"`
}

func (s *Server) sftpForActive(r *http.Request) (*sftpxClient, error) {
	active, err := s.loadActiveInstance(r.Context())
	if err != nil {
		return nil, err
	}
	sshClient, err := s.SSH.Get(r.Context(), active.Target)
	if err != nil {
		return nil, errs.New(errs.CodeInstanceUnreach, http.StatusBadGateway, "cannot connect to active instance: "+err.Error())
	}
	c, err := sftpx.NewClient(sshClient)
	if err != nil {
		return nil, errs.New(errs.CodeInstanceUnreach, http.StatusBadGateway, "cannot open sftp: "+err.Error())
	}
	return &sftpxClient{Client: c}, nil
}

type sftpxClient struct{ *sftp.Client }

func (s *Server) listFiles(w http.ResponseWriter, r *http.Request) {
	p, err := cleanRequestPath(r.URL.Query().Get("path"))
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
	entries, err := sftpx.List(c.Client, p)
	if err != nil {
		errs.Write(w, errs.New(errs.CodeBadRequest, http.StatusBadRequest, err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, entries)
}

func (s *Server) mkdirFile(w http.ResponseWriter, r *http.Request) {
	var req pathRequest
	if err := decodeJSON(r, &req); err != nil {
		errs.Write(w, err)
		return
	}
	p, err := cleanRequestPath(req.Path)
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
	if err := c.MkdirAll(p); err != nil {
		errs.Write(w, errs.New(errs.CodeBadRequest, http.StatusBadRequest, err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) renameFile(w http.ResponseWriter, r *http.Request) {
	var req renameRequest
	if err := decodeJSON(r, &req); err != nil {
		errs.Write(w, err)
		return
	}
	oldPath, err := cleanRequestPath(req.OldPath)
	if err != nil {
		errs.Write(w, err)
		return
	}
	newPath, err := cleanRequestPath(req.NewPath)
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
	if err := sftpx.EnsureParent(c.Client, newPath); err != nil {
		errs.Write(w, errs.New(errs.CodeBadRequest, http.StatusBadRequest, err.Error()))
		return
	}
	if err := c.Rename(oldPath, newPath); err != nil {
		errs.Write(w, errs.New(errs.CodeBadRequest, http.StatusBadRequest, err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) deleteFile(w http.ResponseWriter, r *http.Request) {
	p, err := cleanRequestPath(r.URL.Query().Get("path"))
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
	if err := sftpx.Remove(c.Client, p); err != nil {
		errs.Write(w, errs.New(errs.CodeBadRequest, http.StatusBadRequest, err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) downloadFile(w http.ResponseWriter, r *http.Request) {
	p, err := cleanRequestPath(r.URL.Query().Get("path"))
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
	info, err := c.Stat(p)
	if err != nil || info.IsDir() {
		if err == nil {
			err = path.ErrBadPattern
		}
		errs.Write(w, errs.New(errs.CodeBadRequest, http.StatusBadRequest, err.Error()))
		return
	}
	f, err := c.Open(p)
	if err != nil {
		errs.Write(w, errs.New(errs.CodeBadRequest, http.StatusBadRequest, err.Error()))
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="`+escapeHeaderFilename(path.Base(p))+`"`)
	w.Header().Set("Content-Length", strconv.FormatInt(info.Size(), 10))
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, f)
}

func cleanRequestPath(p string) (string, error) {
	cleaned, err := sftpx.CleanPath(p)
	if err != nil {
		return "", errs.New(errs.CodeBadRequest, http.StatusBadRequest, err.Error())
	}
	return cleaned, nil
}

func escapeHeaderFilename(name string) string {
	out := make([]rune, 0, len(name))
	for _, r := range name {
		if r == '"' || r == '\\' || r < 0x20 || r == 0x7f {
			out = append(out, '_')
			continue
		}
		out = append(out, r)
	}
	return string(out)
}
