// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"strconv"
	"strings"
	"time"

	pkg_valkey "ItsBagelBot/pkg/valkey"

	"github.com/valkey-io/valkey-go"
)

const (
	durableKeyPrefix   = "automation:durable:"
	durableReplicasKey = durableKeyPrefix + "replicas"
	durableWakeChannel = durableKeyPrefix + "wake"
	durableKeyTTL      = time.Hour
	durableLapsedLimit = 32
)

var durableReplicas = pkg_valkey.DueIndex{Key: durableReplicasKey}

// ValkeyDurableRuns keeps per-channel records under one hash tag; only the replica lease index and each replica's
// owned set are shared, so a sweep touches dead replicas and never walks channels.
type ValkeyDurableRuns struct {
	client valkey.Client
	pubsub valkey.Client
}

func NewValkeyDurableRuns(client valkey.Client) *ValkeyDurableRuns {
	return &ValkeyDurableRuns{client: pkg_valkey.Primary(client), pubsub: client}
}

func durableChannelBase(broadcasterID uint64) string {
	return durableKeyPrefix + "{" + strconv.FormatUint(broadcasterID, 10) + "}:"
}

func durableSeatsKey(broadcasterID uint64) string {
	return durableChannelBase(broadcasterID) + "seats"
}

func durableRecordKeys(ref DurableRef) []string {
	base := durableChannelBase(ref.BroadcasterID)
	return []string{base + "records", base + "owners", base + "seats", base + "taken:" + ref.ID}
}

func ownedDurableRecord(ref DurableRef) pkg_valkey.OwnedRecord {
	keys := durableRecordKeys(ref)
	return pkg_valkey.OwnedRecord{Records: keys[0], Owners: keys[1], PinCounts: keys[2], Taken: keys[3], ID: ref.ID}
}

type durableOwner string

type durableMember string

type durableHandover struct {
	from durableOwner
	to   durableOwner
}

func durableOwnedKey(owner durableOwner) string {
	return durableKeyPrefix + "owned:{" + string(owner) + "}"
}

func durableOwned(owner durableOwner) pkg_valkey.ExpiringSet {
	return pkg_valkey.ExpiringSet{Key: durableOwnedKey(owner), TTL: durableKeyTTL}
}

func memberOf(ref DurableRef) durableMember {
	return durableMember(strconv.FormatUint(ref.BroadcasterID, 10) + ":" + ref.ID)
}

func memberStrings(members []durableMember) []string {
	out := make([]string, len(members))
	for i, member := range members {
		out[i] = string(member)
	}
	return out
}

func parseDurableMember(member durableMember) (DurableRef, bool) {
	raw, id, ok := strings.Cut(string(member), ":")
	broadcasterID, err := strconv.ParseUint(raw, 10, 64)
	if !ok || err != nil {
		return DurableRef{}, false
	}
	return DurableRef{BroadcasterID: broadcasterID, ID: id}, true
}

func (s *ValkeyDurableRuns) own(ctx context.Context, owner durableOwner, members ...durableMember) error {
	return durableOwned(owner).Add(ctx, s.client, memberStrings(members))
}

func (s *ValkeyDurableRuns) disown(ctx context.Context, owner durableOwner, members ...durableMember) error {
	return durableOwned(owner).Remove(ctx, s.client, memberStrings(members))
}

// The owned entry goes first: a crash between the writes leaves a stale entry a sweep drops, never an unreachable record.
func (s *ValkeyDurableRuns) Save(ctx context.Context, owner string, save DurableSave) (DurableStanding, error) {
	if err := s.own(ctx, durableOwner(owner), memberOf(save.Ref)); err != nil {
		return DurableMissing, err
	}
	standing, err := ownedDurableRecord(save.Ref).Save(ctx, s.client, pkg_valkey.OwnedSave{
		Owner:     owner,
		Body:      string(save.Body),
		MustBeNew: save.Fresh,
		Pin:       save.Seat.Ticket,
		PinKeys:   runQueueKeys(save.Seat),
		TTL:       durableKeyTTL,
	})
	return DurableStanding(standing), err
}

func (s *ValkeyDurableRuns) Standing(ctx context.Context, owner string, ref DurableRef) (DurableStanding, error) {
	standing, err := ownedDurableRecord(ref).Standing(ctx, s.client, owner)
	return DurableStanding(standing), err
}

func (s *ValkeyDurableRuns) Finish(ctx context.Context, owner string, ref DurableRef, ticket string) (DurableStanding, error) {
	standing, err := ownedDurableRecord(ref).Finish(ctx, s.client, pkg_valkey.OwnedFinish{Owner: owner, Pin: ticket})
	if err != nil {
		return DurableMissing, err
	}
	return DurableStanding(standing), s.disown(ctx, durableOwner(owner), memberOf(ref))
}

func (s *ValkeyDurableRuns) Renew(ctx context.Context, owner string, lease time.Duration) error {
	return durableReplicas.Renew(ctx, s.client, pkg_valkey.DueEntry{Member: owner, After: lease})
}

func (s *ValkeyDurableRuns) Release(ctx context.Context, owner string) error {
	return durableReplicas.LapseNow(ctx, s.client, pkg_valkey.DueNotice{Member: owner, Channel: durableWakeChannel})
}

func (s *ValkeyDurableRuns) Lapsed(ctx context.Context) ([]string, time.Duration, error) {
	return durableReplicas.Lapsed(ctx, s.client, durableLapsedLimit)
}

func (s *ValkeyDurableRuns) Claim(ctx context.Context, from, to string, limit int) ([]DurableClaim, bool, error) {
	handover := durableHandover{from: durableOwner(from), to: durableOwner(to)}
	raw, err := durableOwned(handover.from).Sample(ctx, s.client, limit)
	if err != nil {
		return nil, false, err
	}
	if len(raw) == 0 {
		return nil, true, durableReplicas.Remove(ctx, s.client, from)
	}
	members := make([]durableMember, len(raw))
	for i, member := range raw {
		members[i] = durableMember(member)
	}
	if err := s.own(ctx, handover.to, members...); err != nil {
		return nil, false, err
	}
	claims := make([]DurableClaim, 0, len(members))
	for _, member := range members {
		claim, claimed, err := s.claimOne(ctx, handover, member)
		if err != nil {
			return claims, false, err
		}
		if claimed {
			claims = append(claims, claim)
		}
	}
	return claims, false, nil
}

func (s *ValkeyDurableRuns) claimOne(ctx context.Context, handover durableHandover, member durableMember) (DurableClaim, bool, error) {
	ref, ok := parseDurableMember(member)
	if !ok {
		return DurableClaim{}, false, s.disown(ctx, handover.from, member)
	}
	result, err := ownedDurableRecord(ref).Claim(ctx, s.client, pkg_valkey.OwnedHandover{From: string(handover.from), To: string(handover.to), TakenTTL: durableKeyTTL})
	if err != nil {
		return DurableClaim{}, false, err
	}
	if result.Status == pkg_valkey.OwnedClaimGone {
		_ = s.disown(ctx, handover.to, member)
	}
	if err := s.disown(ctx, handover.from, member); err != nil {
		return DurableClaim{}, false, err
	}
	return DurableClaim{Ref: ref, Body: []byte(result.Body)}, result.Status == pkg_valkey.OwnedClaimWon, result.BodyErr
}

func (s *ValkeyDurableRuns) Wakes(ctx context.Context, wake func()) error {
	return pkg_valkey.OnPublish(ctx, s.pubsub, durableWakeChannel, wake)
}
