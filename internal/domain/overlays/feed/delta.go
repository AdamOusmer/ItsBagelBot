// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package feed

import "ItsBagelBot/internal/domain/channelevents"

const (
	FieldFollows     = "follows"
	FieldSubs        = "subs"
	FieldResubs      = "resubs"
	FieldGifts       = "gifts"
	FieldBits        = "bits"
	FieldRaids       = "raids"
	FieldRedemptions = "redemptions"
	FieldSubPoints   = "sub_points"
	FieldTierPrefix  = "tier:"
)

type Board string

const (
	BoardCheer Board = "cheer"
	BoardGift  Board = "gift"
)

type CountDelta struct {
	Field  string
	Amount int64
}

type LeaderDelta struct {
	Board  Board
	User   User
	Amount int64
}

// CreditDelta adds Amount to User on the credits list named by the overlay kind.
type CreditDelta struct {
	List   Kind
	User   User
	Amount int64
}

// Delta is every additive effect of one channel event on the feed tallies, so replays and reorderings sum to the same stats.
type Delta struct {
	Counts  []CountDelta
	Leader  *LeaderDelta
	Credits []CreditDelta
}

// SubPoints is the Twitch sub point value of a tier.
func SubPoints(tier string) int64 {
	switch tier {
	case "2000":
		return 2
	case "3000":
		return 6
	}
	return 1
}

func DeltaOf(e channelevents.Event) Delta {
	switch e.Kind {
	case channelevents.Follow:
		return Delta{Counts: []CountDelta{{FieldFollows, 1}}, Credits: creditsOf(Follow, e, 1)}
	case channelevents.Sub, channelevents.Resub:
		return subscriptionDelta(e)
	case channelevents.CommunityGift:
		return giftDelta(e)
	case channelevents.Cheer:
		return Delta{Counts: []CountDelta{{FieldBits, e.Amount}}, Leader: leaderOf(BoardCheer, e), Credits: creditsOf(Cheer, e, e.Amount)}
	case channelevents.Raid:
		return Delta{Counts: []CountDelta{{FieldRaids, 1}}, Credits: creditsOf(Raid, e, max(1, e.Amount))}
	case channelevents.Redemption:
		return Delta{Counts: []CountDelta{{FieldRedemptions, 1}}, Credits: creditsOf(Redemption, e, 1)}
	}
	return Delta{}
}

func subscriptionDelta(e channelevents.Event) Delta {
	tier := tierOf(e)
	counts := []CountDelta{{FieldSubs, 1}, {FieldTierPrefix + tier, 1}, {FieldSubPoints, SubPoints(tier)}}
	if e.Kind == channelevents.Resub {
		counts = append(counts, CountDelta{FieldResubs, 1})
	}
	return Delta{Counts: counts, Credits: creditsOf(Sub, e, 1)}
}

func giftDelta(e channelevents.Event) Delta {
	tier := tierOf(e)
	counts := []CountDelta{
		{FieldGifts, e.Amount}, {FieldSubs, e.Amount}, {FieldTierPrefix + tier, e.Amount}, {FieldSubPoints, e.Amount * SubPoints(tier)},
	}
	credits := creditsOf(GiftSub, e, e.Amount)
	for _, recipient := range e.Recipients {
		credits = append(credits, CreditDelta{List: Sub, User: User{ID: recipient.ID, Login: recipient.Login, Name: recipient.Name}, Amount: 1})
	}
	return Delta{Counts: counts, Leader: leaderOf(BoardGift, e), Credits: credits}
}

func identified(e channelevents.Event) (User, bool) {
	if e.Anonymous || !hasID(e.User) {
		return User{}, false
	}
	return User{ID: e.User.ID, Login: e.User.Login, Name: e.User.Name}, true
}

func hasID(u *channelevents.User) bool {
	return u != nil && u.ID != ""
}

func leaderOf(board Board, e channelevents.Event) *LeaderDelta {
	user, ok := identified(e)
	if !ok {
		return nil
	}
	return &LeaderDelta{Board: board, User: user, Amount: e.Amount}
}

func creditsOf(list Kind, e channelevents.Event, amount int64) []CreditDelta {
	user, ok := identified(e)
	if !ok {
		return nil
	}
	return []CreditDelta{{List: list, User: user, Amount: amount}}
}
