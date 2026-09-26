// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package watchtime

import (
	"errors"

	"ItsBagelBot/internal/domain/event/data"
)

// ValidateAward is shared by the outbox reader and repository entry point.
// Identity, batch bounds and individual numeric mutations are validated before
// any delivery is accepted or credited.
func ValidateAward(award data.WatchAwardDTO) error {
	if err := validateAwardIdentity(award); err != nil {
		return err
	}
	if len(award.Entries) == 0 || len(award.Entries) > 1000 {
		return errors.New("invalid watch award identity or batch size")
	}
	for _, entry := range award.Entries {
		if err := validateAwardEntry(entry); err != nil {
			return err
		}
	}
	return nil
}

func validateAwardIdentity(award data.WatchAwardDTO) error {
	if award.UserID == 0 {
		return errors.New("invalid watch award identity or batch size")
	}
	if !(awardNumberRange{1, data.MaxCounter}).contains(award.AccountCreatedAt) {
		return errors.New("invalid watch award identity or batch size")
	}
	if !(awardNumberRange{0, data.MaxCounter}).contains(award.WindowStartedAtUnixMilli) {
		return errors.New("invalid watch award identity or batch size")
	}
	for _, field := range []awardIdentityField{{award.Generation, 64}, {award.LiveSession, 128}, {award.WindowID, 128}} {
		if !field.valid() {
			return errors.New("invalid watch award identity or batch size")
		}
	}
	return nil
}

type awardIdentityField struct {
	value   string
	maximum int
}

func (field awardIdentityField) valid() bool {
	return (awardNumberRange{1, int64(field.maximum)}).contains(int64(len(field.value)))
}

type awardNumberRange struct{ minimum, maximum int64 }

func (limit awardNumberRange) contains(value int64) bool {
	return value >= limit.minimum && value <= limit.maximum
}

func validateAwardEntry(entry data.LoyaltyEarnEntry) error {
	if entry.ViewerID == 0 {
		return errors.New("invalid watch award entry")
	}
	if !(awardNumberRange{0, 1_000_000_000}).contains(entry.Points) {
		return errors.New("invalid watch award entry")
	}
	if entry.WatchSeconds == 0 || entry.WatchSeconds > 300 {
		return errors.New("invalid watch award entry")
	}
	if len(entry.ViewerLogin) > 64 || len(entry.ViewerName) > 64 {
		return errors.New("invalid watch award entry")
	}
	return nil
}
