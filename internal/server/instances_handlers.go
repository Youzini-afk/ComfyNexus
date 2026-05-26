package server

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"time"

	cnxcrypto "github.com/Youzini-afk/ComfyNexus/internal/crypto"
	"github.com/Youzini-afk/ComfyNexus/internal/errs"
	"github.com/Youzini-afk/ComfyNexus/internal/sshmgr"
	"github.com/go-chi/chi/v5"
)

// instanceWire is the wire-format we expose to the frontend. We never return
// the raw key blob/passphrase.
type instanceWire struct {
	ID              int64  `json:"id"`
	Name            string `json:"name"`
	SSHHost         string `json:"sshHost"`
	SSHPort         int    `json:"sshPort"`
	SSHUser         string `json:"sshUser"`
	SSHKeySource    string `json:"sshKeySource"`         // "inline"/"inline_encrypted"/"inline_plain" | "mounted"/"mounted_file"
	SSHKeyPath      string `json:"sshKeyPath,omitempty"` // when source = mounted
	HasInlineKey    bool   `json:"hasInlineKey"`
	HasPassphrase   bool   `json:"hasPassphrase"`
	HostFingerprint string `json:"hostFingerprint,omitempty"`
	ComfyHost       string `json:"comfyHost"`
	ComfyPort       int    `json:"comfyPort"`
	ComfyRoot       string `json:"comfyRoot,omitempty"`
	ComfyStartCmd   string `json:"comfyStartCmd,omitempty"`
	Notes           string `json:"notes,omitempty"`
	IsActive        bool   `json:"isActive"`
	CreatedAt       string `json:"createdAt"`
	UpdatedAt       string `json:"updatedAt"`
}

type instanceUpsert struct {
	Name            string `json:"name"`
	SSHHost         string `json:"sshHost"`
	SSHPort         int    `json:"sshPort"`
	SSHUser         string `json:"sshUser"`
	SSHKeySource    string `json:"sshKeySource"` // "inline"/"inline_encrypted"/"inline_plain" | "mounted"/"mounted_file"
	SSHKeyPEM       string `json:"sshKeyPEM,omitempty"`
	SSHKeyPath      string `json:"sshKeyPath,omitempty"`
	SSHPassphrase   string `json:"sshPassphrase,omitempty"`
	HostFingerprint string `json:"hostFingerprint,omitempty"`
	ComfyHost       string `json:"comfyHost"`
	ComfyPort       int    `json:"comfyPort"`
	ComfyRoot       string `json:"comfyRoot,omitempty"`
	ComfyStartCmd   string `json:"comfyStartCmd,omitempty"`
	Notes           string `json:"notes,omitempty"`
}

func (s *Server) listInstances(w http.ResponseWriter, r *http.Request) {
	activeID, _ := s.activeInstanceID(r.Context())
	rows, err := s.DB.QueryContext(r.Context(), `
		SELECT id, name, ssh_host, ssh_port, ssh_user, ssh_key_source,
		       ssh_key_path, ssh_key_blob, ssh_passphrase_blob, ssh_host_fingerprint,
		       comfy_host, comfy_port, comfy_root, comfy_start_cmd, notes,
		       created_at, updated_at
		FROM gpu_instances ORDER BY id`)
	if err != nil {
		errs.Write(w, err)
		return
	}
	defer rows.Close()
	out := []instanceWire{}
	for rows.Next() {
		var iw instanceWire
		var keyPath, fingerprint, comfyRoot, comfyStartCmd, notes sql.NullString
		var keyBlob, passBlob []byte
		if err := rows.Scan(&iw.ID, &iw.Name, &iw.SSHHost, &iw.SSHPort, &iw.SSHUser, &iw.SSHKeySource,
			&keyPath, &keyBlob, &passBlob, &fingerprint,
			&iw.ComfyHost, &iw.ComfyPort, &comfyRoot, &comfyStartCmd, &notes,
			&iw.CreatedAt, &iw.UpdatedAt); err != nil {
			errs.Write(w, err)
			return
		}
		iw.SSHKeyPath = keyPath.String
		iw.HasInlineKey = len(keyBlob) > 0
		iw.HasPassphrase = len(passBlob) > 0
		iw.HostFingerprint = fingerprint.String
		iw.ComfyRoot = comfyRoot.String
		iw.ComfyStartCmd = comfyStartCmd.String
		iw.Notes = notes.String
		iw.IsActive = iw.ID == activeID
		out = append(out, iw)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) createInstance(w http.ResponseWriter, r *http.Request) {
	var req instanceUpsert
	if err := decodeJSON(r, &req); err != nil {
		errs.Write(w, err)
		return
	}
	if err := validateInstance(&req); err != nil {
		errs.Write(w, err)
		return
	}
	if isInlineKeySource(req.SSHKeySource) && req.SSHKeyPEM == "" {
		errs.Write(w, errs.New(errs.CodeBadRequest, http.StatusBadRequest, "sshKeyPEM required for inline key source"))
		return
	}
	keyBlob, passBlob, err := s.encryptInstanceSecrets(req)
	if err != nil {
		errs.Write(w, err)
		return
	}
	res, err := s.DB.ExecContext(r.Context(), `
		INSERT INTO gpu_instances(name, ssh_host, ssh_port, ssh_user, ssh_key_source,
		    ssh_key_blob, ssh_key_path, ssh_passphrase_blob, ssh_host_fingerprint,
		    comfy_host, comfy_port, comfy_root, comfy_start_cmd, notes)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		req.Name, req.SSHHost, req.SSHPort, req.SSHUser, req.SSHKeySource,
		keyBlob, nullString(req.SSHKeyPath), passBlob, nullString(req.HostFingerprint),
		req.ComfyHost, req.ComfyPort, nullString(req.ComfyRoot), nullString(req.ComfyStartCmd), nullString(req.Notes))
	if err != nil {
		errs.Write(w, err)
		return
	}
	id, _ := res.LastInsertId()
	// Auto-activate the first instance.
	if active, _ := s.activeInstanceID(r.Context()); active == 0 {
		_ = s.setActiveInstance(r.Context(), id)
	}
	writeJSON(w, http.StatusCreated, map[string]int64{"id": id})
}

func (s *Server) updateInstance(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		errs.Write(w, errs.New(errs.CodeBadRequest, http.StatusBadRequest, "bad id"))
		return
	}
	var req instanceUpsert
	if err := decodeJSON(r, &req); err != nil {
		errs.Write(w, err)
		return
	}
	if err := validateInstance(&req); err != nil {
		errs.Write(w, err)
		return
	}
	// Only update key blobs when the user supplied a fresh PEM/passphrase.
	keyBlob, passBlob, err := s.encryptInstanceSecrets(req)
	if err != nil {
		errs.Write(w, err)
		return
	}
	tx, err := s.DB.BeginTx(r.Context(), nil)
	if err != nil {
		errs.Write(w, err)
		return
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(r.Context(), `
		UPDATE gpu_instances SET name=?, ssh_host=?, ssh_port=?, ssh_user=?, ssh_key_source=?,
		       ssh_key_path=?, ssh_host_fingerprint=?, comfy_host=?, comfy_port=?,
		       comfy_root=?, comfy_start_cmd=?, notes=?, updated_at=CURRENT_TIMESTAMP
		WHERE id=?`,
		req.Name, req.SSHHost, req.SSHPort, req.SSHUser, req.SSHKeySource,
		nullString(req.SSHKeyPath), nullString(req.HostFingerprint),
		req.ComfyHost, req.ComfyPort, nullString(req.ComfyRoot), nullString(req.ComfyStartCmd), nullString(req.Notes),
		id); err != nil {
		errs.Write(w, err)
		return
	}
	if isInlineKeySource(req.SSHKeySource) {
		var existing int
		if err := tx.QueryRowContext(r.Context(), `SELECT CASE WHEN ssh_key_blob IS NULL THEN 0 ELSE 1 END FROM gpu_instances WHERE id=?`, id).Scan(&existing); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				errs.Write(w, errs.New(errs.CodeNotFound, http.StatusNotFound, "instance not found"))
				return
			}
			errs.Write(w, err)
			return
		}
		if existing == 0 && keyBlob == nil {
			errs.Write(w, errs.New(errs.CodeBadRequest, http.StatusBadRequest, "sshKeyPEM required for inline key source"))
			return
		}
	}
	if keyBlob != nil {
		if _, err := tx.ExecContext(r.Context(), `UPDATE gpu_instances SET ssh_key_blob=? WHERE id=?`, keyBlob, id); err != nil {
			errs.Write(w, err)
			return
		}
	}
	if isMountedKeySource(req.SSHKeySource) {
		// Clear inline key blob when switching to mounted.
		if _, err := tx.ExecContext(r.Context(), `UPDATE gpu_instances SET ssh_key_blob=NULL WHERE id=?`, id); err != nil {
			errs.Write(w, err)
			return
		}
	}
	if passBlob != nil {
		if _, err := tx.ExecContext(r.Context(), `UPDATE gpu_instances SET ssh_passphrase_blob=? WHERE id=?`, passBlob, id); err != nil {
			errs.Write(w, err)
			return
		}
	}
	if err := tx.Commit(); err != nil {
		errs.Write(w, err)
		return
	}
	s.SSH.CloseInstance(id) // force reconnect with updated config.
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) deleteInstance(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		errs.Write(w, errs.New(errs.CodeBadRequest, http.StatusBadRequest, "bad id"))
		return
	}
	if _, err := s.DB.ExecContext(r.Context(), `DELETE FROM gpu_instances WHERE id=?`, id); err != nil {
		errs.Write(w, err)
		return
	}
	// Clear active pointer if it referenced this instance.
	if active, _ := s.activeInstanceID(r.Context()); active == id {
		_, _ = s.DB.ExecContext(r.Context(), `DELETE FROM settings WHERE key='active_instance_id'`)
	}
	s.SSH.CloseInstance(id)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) activateInstance(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		errs.Write(w, errs.New(errs.CodeBadRequest, http.StatusBadRequest, "bad id"))
		return
	}
	var n int
	if err := s.DB.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM gpu_instances WHERE id=?`, id).Scan(&n); err != nil {
		errs.Write(w, err)
		return
	}
	if n == 0 {
		errs.Write(w, errs.New(errs.CodeNotFound, http.StatusNotFound, "instance not found"))
		return
	}
	if err := s.setActiveInstance(r.Context(), id); err != nil {
		errs.Write(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"activeId": id})
}

func (s *Server) getActiveInstance(w http.ResponseWriter, r *http.Request) {
	id, err := s.activeInstanceID(r.Context())
	if err != nil {
		errs.Write(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"activeId": id})
}

func (s *Server) testInstance(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		errs.Write(w, errs.New(errs.CodeBadRequest, http.StatusBadRequest, "bad id"))
		return
	}
	tgt, err := s.loadTarget(r.Context(), id)
	if err != nil {
		errs.Write(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	cli, err := s.SSH.Get(ctx, tgt)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	// Quick smoke: open a session, run `uname -a`.
	sess, err := cli.NewSession()
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	defer sess.Close()
	out, err := sess.CombinedOutput("uname -a; (command -v nvidia-smi >/dev/null && nvidia-smi -L) || true")
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "output": string(out)})
}

// ----- helpers -----

func validateInstance(r *instanceUpsert) error {
	if r.Name == "" || r.SSHHost == "" || r.SSHUser == "" {
		return errs.New(errs.CodeBadRequest, http.StatusBadRequest, "name, sshHost, sshUser are required")
	}
	if r.SSHPort == 0 {
		r.SSHPort = 22
	}
	if r.ComfyHost == "" {
		r.ComfyHost = "127.0.0.1"
	}
	if r.ComfyPort == 0 {
		r.ComfyPort = 8188
	}
	if r.SSHKeySource == "" {
		r.SSHKeySource = "inline"
	}
	switch {
	case isInlineKeySource(r.SSHKeySource):
	case isMountedKeySource(r.SSHKeySource):
	default:
		return errs.New(errs.CodeBadRequest, http.StatusBadRequest, "sshKeySource must be inline, inline_encrypted, inline_plain, mounted, or mounted_file")
	}
	if isMountedKeySource(r.SSHKeySource) && r.SSHKeyPath == "" {
		return errs.New(errs.CodeBadRequest, http.StatusBadRequest, "sshKeyPath required for mounted source")
	}
	if isInlineKeySource(r.SSHKeySource) {
		r.SSHKeyPath = ""
	}
	return nil
}

func (s *Server) encryptInstanceSecrets(req instanceUpsert) (keyBlob, passBlob []byte, err error) {
	if req.SSHKeyPEM != "" {
		keyBlob, err = cnxcrypto.Seal(s.KEK, []byte(req.SSHKeyPEM))
		if err != nil {
			return nil, nil, err
		}
	}
	if req.SSHPassphrase != "" {
		passBlob, err = cnxcrypto.Seal(s.KEK, []byte(req.SSHPassphrase))
		if err != nil {
			return nil, nil, err
		}
	}
	return
}

func (s *Server) loadTarget(ctx context.Context, id int64) (sshmgr.Target, error) {
	row := s.DB.QueryRowContext(ctx, `
		SELECT name, ssh_host, ssh_port, ssh_user, ssh_key_source,
		       ssh_key_blob, ssh_key_path, ssh_passphrase_blob, ssh_host_fingerprint
		FROM gpu_instances WHERE id=?`, id)
	var (
		name, host, user, keySource string
		port                        int
		keyBlob, passBlob           []byte
		keyPath, fingerprint        sql.NullString
	)
	if err := row.Scan(&name, &host, &port, &user, &keySource, &keyBlob, &keyPath, &passBlob, &fingerprint); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return sshmgr.Target{}, errs.New(errs.CodeNotFound, http.StatusNotFound, "instance not found")
		}
		return sshmgr.Target{}, err
	}
	tgt := sshmgr.Target{ID: id, Name: name, Host: host, Port: port, User: user}
	if isInlineKeySource(keySource) {
		pem, err := cnxcrypto.Open(s.KEK, keyBlob)
		if err != nil {
			return sshmgr.Target{}, errs.New(errs.CodeInstanceBadKey, http.StatusBadRequest, "cannot decrypt key (master key changed?)")
		}
		tgt.PrivateKeyPEM = pem
	} else if keyPath.Valid {
		tgt.KeyPath = keyPath.String
	}
	if len(passBlob) > 0 {
		pass, err := cnxcrypto.Open(s.KEK, passBlob)
		if err != nil {
			return sshmgr.Target{}, errs.New(errs.CodeInstanceBadKey, http.StatusBadRequest, "cannot decrypt passphrase")
		}
		tgt.Passphrase = pass
	}
	if fingerprint.Valid {
		tgt.HostFingerprint = fingerprint.String
	}
	return tgt, nil
}

func (s *Server) activeInstanceID(ctx context.Context) (int64, error) {
	row := s.DB.QueryRowContext(ctx, `SELECT value FROM settings WHERE key='active_instance_id'`)
	var v string
	if err := row.Scan(&v); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, nil
		}
		return 0, err
	}
	id, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return 0, nil
	}
	return id, nil
}

func (s *Server) setActiveInstance(ctx context.Context, id int64) error {
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO settings(key, value) VALUES('active_instance_id', ?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=CURRENT_TIMESTAMP`,
		strconv.FormatInt(id, 10))
	return err
}

func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func isInlineKeySource(source string) bool {
	switch source {
	case "inline", "inline_encrypted", "inline_plain":
		return true
	default:
		return false
	}
}

func isMountedKeySource(source string) bool {
	switch source {
	case "mounted", "mounted_file":
		return true
	default:
		return false
	}
}
