// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"testing"

	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/internal/domain/validate"

	"github.com/stretchr/testify/assert"
)

func TestValidateCommandChangedBoundsBothCooldowns(t *testing.T) {
	dto := data.CommandChangedDTO{UserID: 1, Name: "lurk", Response: "hi", Perm: "everyone", Cooldown: 5, UserCooldown: 86400}
	assert.NoError(t, validateCommandChanged(dto))

	dto.UserCooldown = 86401
	assert.ErrorIs(t, validateCommandChanged(dto), validate.ErrCooldownInvalid)
}
