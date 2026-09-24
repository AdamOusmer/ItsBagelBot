// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package utils

import "github.com/google/uuid"

func NewID() (uuid.UUID, error) {
	return uuid.NewV7()
}
