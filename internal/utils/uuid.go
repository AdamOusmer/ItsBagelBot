package utils

import "github.com/google/uuid"

// NewID returns a fresh UUIDv7. The caller receives the raw [16]byte value so
// that it can extract the embedded millisecond timestamp without re-parsing the
// string representation. Call .String() when a text form is needed.
func NewID() (uuid.UUID, error) {
	return uuid.NewV7()
}
