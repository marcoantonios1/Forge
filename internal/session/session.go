package session

import (
	"crypto/rand"
	"encoding/hex"
)

func NewID() string {
	b := make([]byte, 5)
	if _, err := rand.Read(b); err != nil {
		panic("session: failed to read random bytes: " + err.Error())
	}
	return hex.EncodeToString(b)
}
