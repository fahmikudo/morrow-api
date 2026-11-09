// Package ids allocates the short prefixed identifiers the frontend expects.
package ids

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// New returns `<prefix>_<8 hex chars>`, e.g. `t_1a2b3c4d`.
func New(prefix string) string {
	var buffer [4]byte
	if _, err := rand.Read(buffer[:]); err != nil {
		panic(fmt.Sprintf("ids: read random bytes: %v", err))
	}

	return fmt.Sprintf("%s_%s", prefix, hex.EncodeToString(buffer[:]))
}
