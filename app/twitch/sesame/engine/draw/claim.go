// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package draw

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"ItsBagelBot/pkg/valkey"
)

type ClaimOutcome struct {
	Claimed int
	Late    int
}

func (s *Store) CanClaim(ctx context.Context, ch ChannelID, viewer ViewerID) (bool, error) {
	refs, err := valkey.ReadClaimRefs(ctx, s.client, claimableKey(ch), string(viewer))
	return len(refs) > 0, err
}

func (s *Store) Claim(ctx context.Context, ch ChannelID, viewer ViewerID) (ClaimOutcome, error) {
	key := claimableKey(ch)
	raw, err := valkey.ReadClaimRefs(ctx, s.client, key, string(viewer))
	if err != nil || len(raw) == 0 {
		return ClaimOutcome{}, err
	}
	refs, err := decodeRefs(raw)
	if err != nil {
		return ClaimOutcome{}, err
	}
	codes, err := valkey.ClaimSlots(ctx, s.client, valkey.SlotClaim{
		Claimable: key, Viewer: string(viewer), Refs: refs, Due: dueKey, Wake: wakeChannel,
	})
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
