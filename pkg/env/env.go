// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package env

import (
	"os"
	"strconv"
	"time"
)

func Get(key string, fallback string) string {

	if value := os.Getenv(key); value != "" {
		return value
	}

	return fallback
}

func GetInt(key string, fallback int) int {

	if value := os.Getenv(key); value != "" {
		if n, err := strconv.Atoi(value); err == nil {
			return n
		}
	}

	return fallback
}

func GetBool(key string, fallback bool) bool {

	if value := os.Getenv(key); value != "" {
		if b, err := strconv.ParseBool(value); err == nil {
			return b
		}
	}

	return fallback
}

func GetDuration(key string, fallback time.Duration) time.Duration {

	if value := os.Getenv(key); value != "" {
		if d, err := time.ParseDuration(value); err == nil {
			return d
		}
	}

	return fallback
}

func MustGet(key string) string {

	value := os.Getenv(key)
	if value == "" {
		panic("missing required environment variable: " + key)
	}

	return value
}

func GetFloat(key string, fallback float64) float64 {

	if value := os.Getenv(key); value != "" {
		if f, err := strconv.ParseFloat(value, 64); err == nil {
			return f
		}
	}

	return fallback
}
