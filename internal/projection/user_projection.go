// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package projection

import (
	"ItsBagelBot/internal/domain/event/data"
	contract "ItsBagelBot/internal/domain/rpc/projection"
)

func UserFromReply(r contract.UserReply) UserProjection {
	return UserProjection{
		StateRevision:      r.StateRevision,
		AccountCreatedAt:   r.AccountCreatedAt,
		Status:             r.Status,
		IsActive:           r.IsActive,
		Banned:             r.Banned,
		Locale:             r.Locale,
		CommandsPageHidden: r.CommandsPageHidden,
	}
}

func UserFromChanged(dto data.UserChangedDTO) UserProjection {
	return UserProjection{
		StateRevision:      dto.StateRevision,
		AccountCreatedAt:   dto.AccountCreatedAt,
		Status:             dto.Status,
		IsActive:           dto.IsActive,
		Banned:             dto.Banned,
		Locale:             dto.Locale,
		CommandsPageHidden: dto.CommandsPageHidden,
	}
}
