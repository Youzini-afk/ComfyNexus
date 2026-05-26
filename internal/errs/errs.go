// Package errs defines stable, machine-readable error codes returned by the
// HTTP API. The frontend uses these codes for i18n lookups so that error
// messages can be translated client-side without depending on the server's
// locale.
package errs

import (
	"encoding/json"
	"errors"
	"net/http"
)

type Code string

const (
	CodeInternal           Code = "INTERNAL"
	CodeBadRequest         Code = "BAD_REQUEST"
	CodeUnauthorized       Code = "UNAUTHORIZED"
	CodeForbidden          Code = "FORBIDDEN"
	CodeNotFound           Code = "NOT_FOUND"
	CodeConflict           Code = "CONFLICT"
	CodeRateLimited        Code = "RATE_LIMITED"
	CodeAuthInvalid        Code = "AUTH_INVALID"
	CodeAuthLocked         Code = "AUTH_LOCKED"
	CodeAuthTOTPInvalid    Code = "AUTH_TOTP_INVALID"
	CodeAuthSetupComplete  Code = "AUTH_SETUP_COMPLETE"
	CodeAuthSetupRequired  Code = "AUTH_SETUP_REQUIRED"
	CodeInstanceNoActive   Code = "INSTANCE_NO_ACTIVE"
	CodeInstanceUnreach    Code = "INSTANCE_UNREACHABLE"
	CodeInstanceBadKey     Code = "INSTANCE_BAD_KEY"
)

// Error is the wire shape returned to clients on failure.
type Error struct {
	Code    Code   `json:"code"`
	Message string `json:"message,omitempty"`
	Status  int    `json:"-"`
}

func (e *Error) Error() string { return string(e.Code) + ": " + e.Message }

func New(code Code, status int, msg string) *Error {
	return &Error{Code: code, Status: status, Message: msg}
}

// Write renders the error as JSON. Falls back to internal for unknown errors.
func Write(w http.ResponseWriter, err error) {
	var e *Error
	if !errors.As(err, &e) {
		e = New(CodeInternal, http.StatusInternalServerError, err.Error())
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(e.Status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": e})
}
