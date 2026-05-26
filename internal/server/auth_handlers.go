package server

import (
	"database/sql"
	"errors"
	"net/http"

	"github.com/Youzini-afk/ComfyNexus/internal/auth"
	"github.com/Youzini-afk/ComfyNexus/internal/errs"
)

// ----- /api/auth/setup-required -----

func (s *Server) handleSetupRequired(w http.ResponseWriter, r *http.Request) {
	var n int
	if err := s.DB.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM users`).Scan(&n); err != nil {
		errs.Write(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"setupRequired": n == 0})
}

// ----- /api/auth/setup -----

type setupReq struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type setupResp struct {
	TOTPSecret string `json:"totpSecret"`
	TOTPURI    string `json:"totpUri"`
}

func (s *Server) handleSetup(w http.ResponseWriter, r *http.Request) {
	var n int
	if err := s.DB.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM users`).Scan(&n); err != nil {
		errs.Write(w, err)
		return
	}
	if n > 0 {
		errs.Write(w, errs.New(errs.CodeAuthSetupComplete, http.StatusConflict, "setup already complete"))
		return
	}
	var req setupReq
	if err := decodeJSON(r, &req); err != nil {
		errs.Write(w, err)
		return
	}
	if req.Username == "" {
		req.Username = "admin"
	}
	if req.Username == "" {
		errs.Write(w, errs.New(errs.CodeBadRequest, http.StatusBadRequest, "username required"))
		return
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		errs.Write(w, err)
		return
	}
	secret, uri, err := auth.NewTOTPSecret(req.Username)
	if err != nil {
		errs.Write(w, err)
		return
	}
	if _, err := s.DB.ExecContext(r.Context(),
		`INSERT INTO users(username, password_hash, totp_secret, role) VALUES(?, ?, ?, 'admin')`,
		req.Username, hash, secret); err != nil {
		errs.Write(w, err)
		return
	}
	writeJSON(w, http.StatusOK, setupResp{TOTPSecret: secret, TOTPURI: uri})
}

// ----- /api/auth/login -----

type loginReq struct {
	Username string `json:"username"`
	Password string `json:"password"`
	TOTP     string `json:"totp"`
}

type loginResp struct {
	Username string `json:"username"`
	Role     string `json:"role"`
	Locale   string `json:"locale"`
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	ip := s.Auth.ClientIP(r)
	if locked, _ := s.Auth.IsLocked(r.Context(), ip); locked {
		errs.Write(w, errs.New(errs.CodeAuthLocked, http.StatusTooManyRequests, "too many failed attempts; try again later"))
		return
	}
	var req loginReq
	if err := decodeJSON(r, &req); err != nil {
		errs.Write(w, err)
		return
	}
	row := s.DB.QueryRowContext(r.Context(),
		`SELECT id, password_hash, totp_secret, role, locale FROM users WHERE username = ?`, req.Username)
	var (
		id     int64
		hash   string
		secret string
		role   string
		locale string
	)
	err := row.Scan(&id, &hash, &secret, &role, &locale)
	if errors.Is(err, sql.ErrNoRows) {
		s.Auth.RecordLogin(r.Context(), ip, req.Username, false)
		errs.Write(w, errs.New(errs.CodeAuthInvalid, http.StatusUnauthorized, "invalid credentials"))
		return
	}
	if err != nil {
		errs.Write(w, err)
		return
	}
	if !auth.VerifyPassword(hash, req.Password) {
		s.Auth.RecordLogin(r.Context(), ip, req.Username, false)
		errs.Write(w, errs.New(errs.CodeAuthInvalid, http.StatusUnauthorized, "invalid credentials"))
		return
	}
	if !auth.VerifyTOTP(secret, req.TOTP) {
		s.Auth.RecordLogin(r.Context(), ip, req.Username, false)
		errs.Write(w, errs.New(errs.CodeAuthTOTPInvalid, http.StatusUnauthorized, "invalid TOTP code"))
		return
	}
	tok, exp, err := s.Auth.CreateSession(r.Context(), id, ip, r.UserAgent())
	if err != nil {
		errs.Write(w, err)
		return
	}
	s.Auth.RecordLogin(r.Context(), ip, req.Username, true)
	_, _ = s.DB.ExecContext(r.Context(), `UPDATE users SET last_login_at = CURRENT_TIMESTAMP WHERE id = ?`, id)
	s.Auth.IssueCookie(w, tok, exp)
	writeJSON(w, http.StatusOK, loginResp{Username: req.Username, Role: role, Locale: locale})
}

// ----- /api/auth/logout -----

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(auth.CookieName); err == nil {
		_ = s.Auth.DeleteSession(r.Context(), c.Value)
	}
	s.Auth.ClearCookie(w)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ----- /api/auth/me -----

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	sess := sessionFrom(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{
		"username": sess.Username,
		"role":     sess.Role,
		"locale":   sess.Locale,
	})
}

// ----- /api/auth/locale -----

type localeReq struct {
	Locale string `json:"locale"`
}

func (s *Server) handleSetLocale(w http.ResponseWriter, r *http.Request) {
	var req localeReq
	if err := decodeJSON(r, &req); err != nil {
		errs.Write(w, err)
		return
	}
	switch req.Locale {
	case "zh-CN", "en-US":
	default:
		errs.Write(w, errs.New(errs.CodeBadRequest, http.StatusBadRequest, "unsupported locale"))
		return
	}
	sess := sessionFrom(r.Context())
	if _, err := s.DB.ExecContext(r.Context(),
		`UPDATE users SET locale = ? WHERE id = ?`, req.Locale, sess.UserID); err != nil {
		errs.Write(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"locale": req.Locale})
}
