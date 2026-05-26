// Package server wires the HTTP router for ComfyNexus.
package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Youzini-afk/ComfyNexus/internal/auth"
	"github.com/Youzini-afk/ComfyNexus/internal/config"
	cnxcrypto "github.com/Youzini-afk/ComfyNexus/internal/crypto"
	"github.com/Youzini-afk/ComfyNexus/internal/errs"
	"github.com/Youzini-afk/ComfyNexus/internal/proxy"
	"github.com/Youzini-afk/ComfyNexus/internal/sshmgr"
	"github.com/Youzini-afk/ComfyNexus/internal/web"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// Server holds shared dependencies for HTTP handlers.
type Server struct {
	Cfg    *config.Config
	DB     *sql.DB
	Log    *slog.Logger
	Auth   *auth.Service
	SSH    *sshmgr.Manager
	KEK    cnxcrypto.KEK
	Router http.Handler
}

// New constructs a fully wired *Server.
func New(cfg *config.Config, db *sql.DB, log *slog.Logger) *Server {
	authSvc := auth.NewService(db,
		time.Duration(cfg.SessionTTLDays)*24*time.Hour,
		cfg.CookieSecure, cfg.TrustProxy)

	s := &Server{
		Cfg:  cfg,
		DB:   db,
		Log:  log,
		Auth: authSvc,
		SSH:  sshmgr.New(),
		KEK:  cnxcrypto.DeriveKEK(cfg.MasterKey),
	}
	s.Router = s.routes()
	return s
}

func (s *Server) routes() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(s.requestLogger)
	r.Use(secureHeaders)

	// Public health.
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	// Auth endpoints (public except /me).
	r.Route("/api/auth", func(r chi.Router) {
		r.Get("/setup-status", s.handleSetupRequired)
		r.Get("/setup-required", s.handleSetupRequired)
		r.Post("/setup", s.handleSetup)
		r.Post("/login", s.handleLogin)
		r.Post("/logout", s.handleLogout)
		r.With(s.requireSession).Get("/me", s.handleMe)
		r.With(s.requireSession).Post("/locale", s.handleSetLocale)
	})

	// Authenticated APIs.
	r.Route("/api", func(r chi.Router) {
		r.Use(s.requireSession)

		r.Get("/instances", s.listInstances)
		r.Post("/instances", s.createInstance)
		r.Put("/instances/{id}", s.updateInstance)
		r.Delete("/instances/{id}", s.deleteInstance)
		r.Post("/instances/{id}/test", s.testInstance)
		r.Post("/instances/{id}/activate", s.activateInstance)
		r.Get("/instances/active", s.getActiveInstance)
	})

	// ComfyUI reverse proxy. The session cookie carries auth so an iframe
	// rendered from the same origin "just works". WS connections use the
	// same cookie.
	r.With(s.requireSession).Mount("/comfy", proxy.New(s.SSH, s.activeInstanceProvider, "/comfy"))

	// SPA fallback (must be last).
	r.Handle("/*", web.Handler())

	return r
}

// ----- middlewares -----

func (s *Server) requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)
		// Don't log noisy proxy/static traffic at info level.
		level := slog.LevelDebug
		if strings.HasPrefix(r.URL.Path, "/api/") {
			level = slog.LevelInfo
		}
		s.Log.Log(r.Context(), level, "http",
			"method", r.Method, "path", r.URL.Path,
			"status", ww.Status(), "bytes", ww.BytesWritten(),
			"dur_ms", time.Since(start).Milliseconds())
	})
}

func secureHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "same-origin")
		// Allow same-origin frames so the SPA can iframe /comfy/.
		w.Header().Set("X-Frame-Options", "SAMEORIGIN")
		next.ServeHTTP(w, r)
	})
}

// ctxKey is a private type for storing the session in request context.
type ctxKey int

const ctxSession ctxKey = 1

func sessionFrom(ctx context.Context) *auth.Session {
	v, _ := ctx.Value(ctxSession).(*auth.Session)
	return v
}

func (s *Server) requireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(auth.CookieName)
		if err != nil {
			errs.Write(w, errs.New(errs.CodeUnauthorized, http.StatusUnauthorized, "no session"))
			return
		}
		sess, err := s.Auth.LookupSession(r.Context(), c.Value)
		if err != nil {
			errs.Write(w, err)
			return
		}
		// CSRF guard for state-changing methods on /api/*: require either a
		// custom header (XHR) or same-origin POST/PUT/DELETE.
		if r.Method != http.MethodGet && r.Method != http.MethodHead && strings.HasPrefix(r.URL.Path, "/api/") {
			if r.Header.Get("X-Requested-With") == "" && !sameOrigin(r) {
				errs.Write(w, errs.New(errs.CodeForbidden, http.StatusForbidden, "missing CSRF guard"))
				return
			}
		}
		ctx := context.WithValue(r.Context(), ctxSession, sess)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true // legacy clients omitting Origin
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	return strings.EqualFold(u.Host, r.Host)
}

// ----- helpers -----

func decodeJSON(r *http.Request, v any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return errs.New(errs.CodeBadRequest, http.StatusBadRequest, "invalid JSON: "+err.Error())
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if v == nil {
		return
	}
	_ = json.NewEncoder(w).Encode(v)
}

// activeInstanceProvider satisfies proxy.InstanceProvider.
func (s *Server) activeInstanceProvider(ctx context.Context) (sshmgr.Target, string, int, error) {
	row := s.DB.QueryRowContext(ctx, `
		SELECT i.id, i.name, i.ssh_host, i.ssh_port, i.ssh_user,
		       i.ssh_key_source, i.ssh_key_blob, i.ssh_key_path, i.ssh_passphrase_blob,
		       i.ssh_host_fingerprint, i.comfy_host, i.comfy_port
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
	)
	if err := row.Scan(&id, &name, &host, &port, &user, &keySource, &keyBlob, &keyPath, &passBlob, &fingerprint, &comfyHost, &comfyPort); err != nil {
		if err == sql.ErrNoRows {
			return sshmgr.Target{}, "", 0, errs.New(errs.CodeInstanceNoActive, http.StatusBadGateway, "no active GPU instance")
		}
		return sshmgr.Target{}, "", 0, err
	}
	tgt := sshmgr.Target{ID: id, Name: name, Host: host, Port: port, User: user}
	if isInlineKeySource(keySource) {
		pem, err := cnxcrypto.Open(s.KEK, keyBlob)
		if err != nil {
			return sshmgr.Target{}, "", 0, fmt.Errorf("decrypt key: %w", err)
		}
		tgt.PrivateKeyPEM = pem
	} else {
		if keyPath.Valid {
			tgt.KeyPath = keyPath.String
		}
	}
	if len(passBlob) > 0 {
		pass, err := cnxcrypto.Open(s.KEK, passBlob)
		if err != nil {
			return sshmgr.Target{}, "", 0, fmt.Errorf("decrypt passphrase: %w", err)
		}
		tgt.Passphrase = pass
	}
	if fingerprint.Valid {
		tgt.HostFingerprint = fingerprint.String
	}
	return tgt, comfyHost, comfyPort, nil
}
