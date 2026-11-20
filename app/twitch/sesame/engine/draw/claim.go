// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package draw

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"ItsBagelBot/pkg/valkey"
)

type ClaimOutcome struct {
	Claimed int
	Late    int
}

type ClaimLookup struct {
	Found   bool
	spec    valkey.SlotClaim
	channel ChannelID
}

func (s *Store) CanClaim(ctx context.Context, ch ChannelID, viewer ViewerID) (bool, error) {
	lookup, err := s.LookupClaims(ctx, ch, viewer)
	return lookup.Found, err
}

func (s *Store) LookupClaims(ctx context.Context, ch ChannelID, viewer ViewerID) (ClaimLookup, error) {
	key := claimableKey(ch)
	raw, err := valkey.ReadClaimRefs(ctx, s.client, key, string(viewer))
	if err != nil || len(raw) == 0 {
		return ClaimLookup{}, err
	}
	refs, err := decodeRefs(raw)
	return ClaimLookup{Found: true, channel: ch, spec: valkey.SlotClaim{
		Claimable: key, Viewer: string(viewer), Refs: refs, Due: dueKey, Wake: wakeChannel,
	}}, err
}

func (s *Store) Claim(ctx context.Context, ch ChannelID, viewer ViewerID) (ClaimOutcome, error) {
	lookup, err := s.LookupClaims(ctx, ch, viewer)
	if err != nil {
		return ClaimOutcome{}, err
	}
	return s.ClaimResolved(ctx, lookup)
}

func (s *Store) ClaimResolved(ctx context.Context, lookup ClaimLookup) (ClaimOutcome, error) {
	if len(lookup.spec.Refs) == 0 {
		return ClaimOutcome{}, nil
	}
	if err := s.syncClaimLookup(ctx, lookup); err != nil {
		return ClaimOutcome{}, err
	}
	codes, err := valkey.ClaimSlots(ctx, s.client, lookup.spec)
	var out ClaimOutcome
	for _, code := range codes {
		switch code {
		case valkey.SlotClaimed:
			out.Claimed++
		case valkey.SlotClaimLate:
			out.Late++
		}
	}
	return out, err
}

func (s *Store) syncClaimLookup(ctx context.Context, lookup ClaimLookup) error {
	if s.cfg.Live == nil {
		return nil
	}
	now, err := valkey.ServerNow(ctx, s.client)
	if err != nil {
		return err
	}
	for _, ref := range lookup.spec.Refs {
		if err := s.syncClaimReference(ctx, lookup.channel, RoundID(ref.RoundID), now); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) syncClaimReference(ctx context.Context, channel ChannelID, id RoundID, now time.Time) error {
	for ctx.Err() == nil {
		result, err := s.syncClaimRound(ctx, channel, id, now)
		if err != nil || result != clockConflict {
			return err
		}
	}
	return ctx.Err()
}

func (s *Store) syncClaimRound(ctx context.Context, channel ChannelID, id RoundID, now time.Time) (claimClockResult, error) {
	raw, err := keysFor(channel, id).View(ctx, s.client)
	data, err := decodeSnapshot(id, raw, err)
	if err != nil || data.round.Phase != PhaseSettling {
		return clockReady, err
	}
	return s.syncClaimClock(ctx, data, now)
}

func decodeRefs(raw []string) ([]valkey.ClaimRef, error) {
	refs := make([]valkey.ClaimRef, 0, len(raw))
	for _, ref := range raw {
		parts := strings.Split(ref, ":")
		if len(parts) != 3 {
			return nil, fmt.Errorf("draw: invalid claim reference %q", ref)
		}
		id := RoundID(parts[0])
		refs = append(refs, valkey.ClaimRef{Slots: slotsKey(id), Round: roundKey(id), RoundID: string(id),
			Ref: ref, Slot: parts[1], Generation: parseInt(parts[2])})
	}
	return refs, nil
}

func claimRef(id RoundID, slot Slot) string {
	return string(id) + ":" + strconv.Itoa(slot.N) + ":" + strconv.FormatInt(slot.Generation, 10)
}

func claimDrop(slot Slot) valkey.ClaimIndex {
	if slot.State == SlotWon && !slot.Deadline.IsZero() {
		return valkey.ClaimDrop
	}
	return valkey.ClaimKeep
}
