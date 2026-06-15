package env

import (
	"os"
	"strconv"
)

// Get returns the value of key or fallback when unset or empty.
func Get(key string, fallback string) string {

	if value := os.Getenv(key); value != "" {
		return value
	}

	return fallback
}

// GetInt returns the integer value of key, or fallback when unset, empty, or
// not parseable as a base-10 integer.
func GetInt(key string, fallback int) int {

	if value := os.Getenv(key); value != "" {
		if n, err := strconv.Atoi(value); err == nil {
			return n
		}
	}

	return fallback
}

// MustGet returns the value of key and panics when unset or empty. Reserved
// for values the service cannot run without, such as credentials.
func MustGet(key string) string {

	value := os.Getenv(key)
	if value == "" {
		panic("missing required environment variable: " + key)
	}

	return value
}
