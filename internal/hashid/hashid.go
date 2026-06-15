package hashid

import (
	"crypto/sha256"
	"encoding/hex"
)

func Short(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])[:16]
}
