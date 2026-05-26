package auth

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestHashPasswordAndVerify(t *testing.T) {
	const password = "correct horse battery staple"

	hash, err := HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}

	if !strings.HasPrefix(hash, "$argon2id$v=19$m=") {
		t.Fatalf("HashPassword() returned non-PHC argon2id hash: %q", hash)
	}
	if hash == password {
		t.Fatal("HashPassword() returned the plain password")
	}
	if !VerifyPassword(hash, password) {
		t.Fatal("VerifyPassword() rejected the original password")
	}
	if VerifyPassword(hash, "correct horse battery staples") {
		t.Fatal("VerifyPassword() accepted an incorrect password")
	}
}

func TestHashPasswordRejectsShortPasswords(t *testing.T) {
	if hash, err := HashPassword("too short"); err == nil {
		t.Fatalf("HashPassword() hash = %q, want error", hash)
	}
}

func TestVerifyPasswordRejectsMalformedHashes(t *testing.T) {
	for _, encoded := range []string{
		"",
		"not-a-hash",
		"$argon2i$v=19$m=65536,t=3,p=2$c2FsdA$aGFzaA",
		"$argon2id$v=19$m=65536,t=3,p=2$not base64$aGFzaA",
		"$argon2id$v=19$m=65536,t=3,p=2$c2FsdA$not base64",
		"$argon2id$v=19$bad-params$c2FsdA$aGFzaA",
	} {
		if VerifyPassword(encoded, "correct horse battery staple") {
			t.Fatalf("VerifyPassword(%q) = true, want false", encoded)
		}
	}
}

func TestSessionTokensStoredHashedAndPlaintextMigrates(t *testing.T) {
	db := newAuthTestDB(t)
	svc := NewService(db, time.Hour, false, false)

	token, _, err := svc.CreateSession(context.Background(), 1, "127.0.0.1", "test")
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	var stored string
	if err := db.QueryRow(`SELECT token FROM sessions`).Scan(&stored); err != nil {
		t.Fatalf("scan stored token: %v", err)
	}
	if stored == token || !strings.HasPrefix(stored, sessionHashPrefix) {
		t.Fatalf("stored token = %q, want hashed and not plaintext %q", stored, token)
	}
	if _, err := svc.LookupSession(context.Background(), token); err != nil {
		t.Fatalf("LookupSession(hashed) error = %v", err)
	}

	plain := "legacy-plaintext-token"
	if _, err := db.Exec(`INSERT INTO sessions(token, user_id, expires_at) VALUES(?, 1, ?)`, plain, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("insert plaintext session: %v", err)
	}
	if _, err := svc.LookupSession(context.Background(), plain); err != nil {
		t.Fatalf("LookupSession(plaintext) error = %v", err)
	}
	if err := db.QueryRow(`SELECT token FROM sessions WHERE user_id=1 AND token=?`, hashSessionToken(plain)).Scan(&stored); err != nil {
		t.Fatalf("plaintext session was not migrated to hash: %v", err)
	}
}

func newAuthTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`
		CREATE TABLE users (id INTEGER PRIMARY KEY, username TEXT NOT NULL, role TEXT NOT NULL, locale TEXT NOT NULL);
		INSERT INTO users(id, username, role, locale) VALUES(1, 'admin', 'admin', 'en-US');
		CREATE TABLE sessions (token TEXT PRIMARY KEY, user_id INTEGER NOT NULL, created_at DATETIME DEFAULT CURRENT_TIMESTAMP, expires_at DATETIME NOT NULL, last_seen DATETIME DEFAULT CURRENT_TIMESTAMP, ip TEXT, user_agent TEXT);
	`); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	return db
}
