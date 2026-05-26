package crypto

import "crypto/rand"

// ReadRandom is a thin wrapper over crypto/rand for testability.
func ReadRandom(b []byte) (int, error) { return rand.Read(b) }
