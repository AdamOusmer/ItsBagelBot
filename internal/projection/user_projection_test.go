// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package projection

import (
	"reflect"
	"testing"

	"ItsBagelBot/internal/domain/event/data"
	contract "ItsBagelBot/internal/domain/rpc/projection"

	"github.com/stretchr/testify/require"
)

var fullUser = UserProjection{
	StateRevision:      7,
	AccountCreatedAt:   101,
	Status:             "paid",
	IsActive:           true,
	Banned:             true,
	Locale:             "fr",
	CommandsPageHidden: true,
}

func TestFullUserSetsEveryField(t *testing.T) {
	v := reflect.ValueOf(fullUser)
	for i := range v.NumField() {
		require.False(t, v.Field(i).IsZero(), "fullUser leaves %s zero", v.Type().Field(i).Name)
	}
}

func TestUserFromReplyCopiesEveryField(t *testing.T) {
	got := UserFromReply(contract.UserReply{
		StateRevision:      7,
		AccountCreatedAt:   101,
		UserID:             "42",
		Status:             "paid",
		IsActive:           true,
		Banned:             true,
		Locale:             "fr",
		CommandsPageHidden: true,
	})
	require.Equal(t, fullUser, got)
}

func TestUserFromChangedCopiesEveryField(t *testing.T) {
	got := UserFromChanged(data.UserChangedDTO{
		StateRevision:      7,
		UserID:             42,
		AccountCreatedAt:   101,
		Username:           "bagel",
		IsActive:           true,
		Status:             "paid",
		Banned:             true,
		Locale:             "fr",
		CommandsPageHidden: true,
	})
	require.Equal(t, fullUser, got)
}
