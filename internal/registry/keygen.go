package registry

import (
	"crypto/rand"
	"encoding/hex"
)

// generateKey creates a 24-char hex group key. Used for auth on add/remove operations.
func generateKey() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
