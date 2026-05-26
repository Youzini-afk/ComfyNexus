package server

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"

	cnxcrypto "github.com/Youzini-afk/ComfyNexus/internal/crypto"
	"github.com/Youzini-afk/ComfyNexus/internal/errs"
	"github.com/Youzini-afk/ComfyNexus/internal/sshmgr"
)

type activeInstance struct {
	Target    sshmgr.Target
	ComfyHost string
	ComfyPort int
	ComfyRoot string
}

func (s *Server) loadActiveInstance(ctx context.Context) (activeInstance, error) {
	row := s.DB.QueryRowContext(ctx, `
		SELECT i.id, i.name, i.ssh_host, i.ssh_port, i.ssh_user,
		       i.ssh_key_source, i.ssh_key_blob, i.ssh_key_path, i.ssh_passphrase_blob,
		       i.ssh_host_fingerprint, i.comfy_host, i.comfy_port, i.comfy_root
		FROM gpu_instances i
		JOIN settings st ON st.key='active_instance_id' AND st.value = CAST(i.id AS TEXT)
		LIMIT 1`)
	var (
		id          int64
		name        string
		host, user  string
		port        int
		keySource   string
		keyBlob     []byte
		keyPath     sql.NullString
		passBlob    []byte
		fingerprint sql.NullString
		comfyHost   string
		comfyPort   int
		comfyRoot   sql.NullString
	)
	if err := row.Scan(&id, &name, &host, &port, &user, &keySource, &keyBlob, &keyPath, &passBlob, &fingerprint, &comfyHost, &comfyPort, &comfyRoot); err != nil {
		if err == sql.ErrNoRows {
			return activeInstance{}, errs.New(errs.CodeInstanceNoActive, http.StatusServiceUnavailable, "no active GPU instance")
		}
		return activeInstance{}, err
	}
	tgt := sshmgr.Target{ID: id, Name: name, Host: host, Port: port, User: user}
	if isInlineKeySource(keySource) {
		pem, err := cnxcrypto.Open(s.KEK, keyBlob)
		if err != nil {
			return activeInstance{}, fmt.Errorf("decrypt key: %w", err)
		}
		tgt.PrivateKeyPEM = pem
	} else if keyPath.Valid {
		tgt.KeyPath = keyPath.String
	}
	if len(passBlob) > 0 {
		pass, err := cnxcrypto.Open(s.KEK, passBlob)
		if err != nil {
			return activeInstance{}, fmt.Errorf("decrypt passphrase: %w", err)
		}
		tgt.Passphrase = pass
	}
	if fingerprint.Valid {
		tgt.HostFingerprint = fingerprint.String
	}
	return activeInstance{Target: tgt, ComfyHost: comfyHost, ComfyPort: comfyPort, ComfyRoot: comfyRoot.String}, nil
}
