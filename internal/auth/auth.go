// Package auth implements password hashing, TOTP enrolment/verification, login
// rate limiting, and HTTP session management.
package auth

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Youzini-afk/ComfyNexus/internal/crypto"
	"github.com/Youzini-afk/ComfyNexus/internal/errs"
	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
	"golang.org/x/crypto/argon2"
)

const (
	// CookieName is the HTTP cookie that carries the session token.
	CookieName = "comfynexus_session"
	// Issuer used in TOTP provisioning URIs.
	Issuer = "ComfyNexus"
	// MaxFailedAttempts5m caps the failed login count per IP within 5 minutes.
	MaxFailedAttempts5m = 5
	sessionHashPrefix   = "sha256:"
)

// Service bundles auth logic; held as a singleton in the API layer.
type Service struct {
	DB           *sql.DB
	SessionTTL   time.Duration
	CookieSecure bool
	TrustProxy   bool
	now          func() time.Time
}

func NewService(db *sql.DB, sessionTTL time.Duration, cookieSecure, trustProxy bool) *Service {
	return &Service{DB: db, SessionTTL: sessionTTL, CookieSecure: cookieSecure, TrustProxy: trustProxy, now: time.Now}
}

// ----- argon2id password hashing -----

type argonParams struct {
	memory, iterations uint32
	parallelism        uint8
	saltLen, keyLen    uint32
}

var defaultArgon = argonParams{memory: 64 * 1024, iterations: 3, parallelism: 2, saltLen: 16, keyLen: 32}

// HashPassword returns an encoded argon2id hash in PHC format.
func HashPassword(pw string) (string, error) {
	if len(pw) < 12 {
		return "", errs.New(errs.CodeBadRequest, http.StatusBadRequest, "password must be at least 12 characters")
	}
	salt := make([]byte, defaultArgon.saltLen)
	if _, err := crypto.ReadRandom(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(pw), salt, defaultArgon.iterations, defaultArgon.memory, defaultArgon.parallelism, defaultArgon.keyLen)
	enc := fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version,
		defaultArgon.memory, defaultArgon.iterations, defaultArgon.parallelism,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key))
	return enc, nil
}

// VerifyPassword does a constant-time comparison against a PHC-encoded hash.
func VerifyPassword(encoded, pw string) bool {
	parts := strings.Split(encoded, "$")
	// Format: ["", "argon2id", "v=19", "m=...,t=...,p=...", "<salt>", "<hash>"]
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false
	}
	var memory, iterations uint32
	var parallelism uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &iterations, &parallelism); err != nil {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false
	}
	got := argon2.IDKey([]byte(pw), salt, iterations, memory, parallelism, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}

// ----- TOTP -----

// NewTOTPSecret generates a fresh TOTP secret bound to the given username.
// Returns the base32 secret and a provisioning URI for QR codes.
func NewTOTPSecret(username string) (secret, uri string, err error) {
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      Issuer,
		AccountName: username,
		Algorithm:   otp.AlgorithmSHA1,
		Digits:      otp.DigitsSix,
	})
	if err != nil {
		return "", "", err
	}
	return key.Secret(), key.URL(), nil
}

// VerifyTOTP returns true if code matches the current secret.
func VerifyTOTP(secret, code string) bool { return totp.Validate(code, secret) }

// ----- session management -----

type Session struct {
	Token     string
	UserID    int64
	Username  string
	Role      string
	Locale    string
	ExpiresAt time.Time
}

func (s *Service) CreateSession(ctx context.Context, userID int64, ip, ua string) (string, time.Time, error) {
	tok, err := crypto.RandomToken(32)
	if err != nil {
		return "", time.Time{}, err
	}
	exp := s.now().Add(s.SessionTTL)
	if _, err := s.DB.ExecContext(ctx,
		`INSERT INTO sessions(token, user_id, expires_at, ip, user_agent) VALUES(?, ?, ?, ?, ?)`,
		hashSessionToken(tok), userID, exp, ip, ua); err != nil {
		return "", time.Time{}, err
	}
	return tok, exp, nil
}

func (s *Service) LookupSession(ctx context.Context, token string) (*Session, error) {
	if token == "" {
		return nil, errs.New(errs.CodeUnauthorized, http.StatusUnauthorized, "no session")
	}
	lookupToken := hashSessionToken(token)
	row := s.DB.QueryRowContext(ctx, `
		SELECT s.token, s.user_id, s.expires_at, u.username, u.role, u.locale
		FROM sessions s JOIN users u ON u.id = s.user_id
		WHERE s.token = ?`, lookupToken)
	var sess Session
	if err := row.Scan(&sess.Token, &sess.UserID, &sess.ExpiresAt, &sess.Username, &sess.Role, &sess.Locale); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			row = s.DB.QueryRowContext(ctx, `
				SELECT s.token, s.user_id, s.expires_at, u.username, u.role, u.locale
				FROM sessions s JOIN users u ON u.id = s.user_id
				WHERE s.token = ?`, token)
			if err := row.Scan(&sess.Token, &sess.UserID, &sess.ExpiresAt, &sess.Username, &sess.Role, &sess.Locale); err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return nil, errs.New(errs.CodeUnauthorized, http.StatusUnauthorized, "invalid session")
				}
				return nil, err
			}
			_, _ = s.DB.ExecContext(ctx, `UPDATE sessions SET token = ? WHERE token = ?`, lookupToken, token)
		} else {
			return nil, err
		}
	}
	if s.now().After(sess.ExpiresAt) {
		_, _ = s.DB.ExecContext(ctx, `DELETE FROM sessions WHERE token IN (?, ?)`, lookupToken, token)
		return nil, errs.New(errs.CodeUnauthorized, http.StatusUnauthorized, "session expired")
	}
	sess.Token = token
	_, _ = s.DB.ExecContext(ctx, `UPDATE sessions SET last_seen = CURRENT_TIMESTAMP WHERE token = ?`, lookupToken)
	return &sess, nil
}

func (s *Service) DeleteSession(ctx context.Context, token string) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM sessions WHERE token IN (?, ?)`, hashSessionToken(token), token)
	return err
}

func hashSessionToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return sessionHashPrefix + base64.RawStdEncoding.EncodeToString(sum[:])
}

// ----- login attempt rate limiting -----

func (s *Service) RecordLogin(ctx context.Context, ip, username string, success bool) {
	v := 0
	if success {
		v = 1
	}
	_, _ = s.DB.ExecContext(ctx,
		`INSERT INTO login_attempts(ip, username, success) VALUES(?, ?, ?)`, ip, username, v)
}

func (s *Service) IsLocked(ctx context.Context, ip string) (bool, error) {
	since := s.now().Add(-5 * time.Minute)
	var n int
	err := s.DB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM login_attempts WHERE ip = ? AND success = 0 AND created_at >= ?`,
		ip, since).Scan(&n)
	if err != nil {
		return false, err
	}
	return n >= MaxFailedAttempts5m, nil
}

// ----- helpers -----

func (s *Service) ClientIP(r *http.Request) string {
	if s.TrustProxy {
		if v := r.Header.Get("X-Forwarded-For"); v != "" {
			if i := strings.Index(v, ","); i >= 0 {
				return strings.TrimSpace(v[:i])
			}
			return strings.TrimSpace(v)
		}
		if v := r.Header.Get("X-Real-IP"); v != "" {
			return v
		}
	}
	host := r.RemoteAddr
	if i := strings.LastIndex(host, ":"); i >= 0 {
		return host[:i]
	}
	return host
}

func (s *Service) IssueCookie(w http.ResponseWriter, token string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    token,
		Path:     "/",
		Expires:  expires,
		HttpOnly: true,
		Secure:   s.CookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
}

func (s *Service) ClearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   s.CookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
}
