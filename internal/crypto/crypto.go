// Package crypto provides at-rest encryption for sensitive values such as SSH
// private keys and passphrases.
//
// A 32-byte key encryption key (KEK) is derived from the master passphrase
// using SHA-256. Each value is sealed with AES-256-GCM using a random 12-byte
// nonce; the nonce is prepended to the ciphertext so decryption is
// self-describing.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"io"
)

// KEK is a 32-byte key encryption key.
type KEK [32]byte

// DeriveKEK turns a passphrase into a 32-byte AES-256 key via SHA-256.
// The master passphrase should already be high-entropy (>=16 chars).
func DeriveKEK(passphrase string) KEK {
	return sha256.Sum256([]byte("comfynexus/v1/" + passphrase))
}

// Seal encrypts plaintext with AES-256-GCM. The 12-byte nonce is prepended.
func Seal(kek KEK, plaintext []byte) ([]byte, error) {
	if plaintext == nil {
		return nil, nil
	}
	block, err := aes.NewCipher(kek[:])
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	out := gcm.Seal(nil, nonce, plaintext, nil)
	return append(nonce, out...), nil
}

// Open reverses Seal. Returns ErrCorruptOrWrongKey if authentication fails,
// which usually means the master key changed.
var ErrCorruptOrWrongKey = errors.New("ciphertext is corrupt or master key changed")

func Open(kek KEK, sealed []byte) ([]byte, error) {
	if len(sealed) == 0 {
		return nil, nil
	}
	block, err := aes.NewCipher(kek[:])
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	ns := gcm.NonceSize()
	if len(sealed) < ns {
		return nil, ErrCorruptOrWrongKey
	}
	nonce, ct := sealed[:ns], sealed[ns:]
	pt, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return nil, ErrCorruptOrWrongKey
	}
	return pt, nil
}

// RandomToken returns n random bytes hex-encoded as 2n characters.
func RandomToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		return "", err
	}
	const hex = "0123456789abcdef"
	out := make([]byte, n*2)
	for i, v := range b {
		out[i*2] = hex[v>>4]
		out[i*2+1] = hex[v&0x0f]
	}
	return string(out), nil
}
