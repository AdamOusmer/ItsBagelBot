// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import "fmt"

func (st DuelState) openerStake() DuelStake {
	return DuelStake{Login: st.Opener, ViewerID: st.OpenerID, Stake: st.OpenerStake}
}

func (st DuelState) challengedParty() DuelParty {
	return DuelParty{ID: st.ChallengeID, Login: st.Challenged}
}

func (st DuelState) openerParty() DuelParty {
	return DuelParty{ID: st.OpenerID, Login: st.Opener}
}

func (p DuelParty) is(other DuelParty) bool {
	if p.ID != 0 && other.ID != 0 {
		return p.ID == other.ID
	}
	return p.Login == other.Login
}

func prepareDuelOpen(spec DuelOpenSpec) (int64, error) {
	if err := validateOpenSpec(spec); err != nil {
		return 0, err
	}
	return openClock(spec), nil
}

func validateOpenSpec(spec DuelOpenSpec) error {
	if spec.Stake <= 0 {
		return fmt.Errorf("duel: stake below the floor (%d)", spec.Stake)
	}
	if spec.Stake > DuelMaxStake {
		return fmt.Errorf("duel: stake above the ceiling (%d)", spec.Stake)
	}
	if spec.Opener.Login == "" || spec.Opener.ID == 0 {
		return fmt.Errorf("duel: open without an opener")
	}
	if spec.Kind == DuelChallenge {
		return validateChallenged(spec)
	}
	return validateKind(spec)
}

func validateChallenged(spec DuelOpenSpec) error {
	if spec.Challenged.Login == "" || spec.Challenged.ID == 0 {
		return fmt.Errorf("duel: challenge without a challenged party")
	}
	if spec.Opener.ID == spec.Challenged.ID {
		return fmt.Errorf("duel: self-challenge (%q)", spec.Opener.Login)
	}
	return nil
}

func validateKind(spec DuelOpenSpec) error {
	switch spec.Kind {
	case DuelPot:
		return nil
	case DuelChallenge:
		return nil
	default:
		return fmt.Errorf("duel: unknown kind %q", spec.Kind)
	}
}

func openClock(spec DuelOpenSpec) int64 {
	if spec.Kind == DuelChallenge {
		return ClampDuelSeconds(spec.ChallengeSeconds, DuelDefaultChallengeSeconds)
	}
	return ClampDuelSeconds(spec.PotSeconds, DuelDefaultPotSeconds)
}
