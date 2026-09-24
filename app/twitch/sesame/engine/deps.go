// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"time"

	"ItsBagelBot/app/twitch/sesame/automod"
	"ItsBagelBot/app/twitch/sesame/engine/scope"
	loyaltyrpc "ItsBagelBot/internal/domain/rpc/loyalty"
	modulesrpc "ItsBagelBot/internal/domain/rpc/modules"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/bus"
	"github.com/valkey-io/valkey-go"

	"go.uber.org/zap"
)

type CommandManager interface {
	Upsert(ctx context.Context, userID string, name, response string) error
	Delete(ctx context.Context, userID string, name string) error
}

type QuotesStore interface {
	QuoteAdd(ctx context.Context, broadcasterID uint64, text, addedBy string) (modulesrpc.Quote, error)
	QuoteGet(ctx context.Context, broadcasterID, number uint64) (modulesrpc.Quote, bool, error)
	QuoteRandom(ctx context.Context, broadcasterID uint64) (modulesrpc.Quote, bool, error)
	QuoteSearch(ctx context.Context, broadcasterID uint64, term string) (modulesrpc.Quote, bool, error)
	QuoteEdit(ctx context.Context, broadcasterID, number uint64, text string) (modulesrpc.Quote, bool, error)
	QuoteRemove(ctx context.Context, broadcasterID, number uint64) (bool, error)
}

type Deps struct {
	TrialStore    valkey.Client
	Proj          projection.Reader
	Live          LiveStore
	Greet         GreetStore
	Cooldown      CooldownStore
	Special       *SpecialSet
	Pub           bus.Publisher
	Commands      CommandManager
	Gossip        GossipCaller
	CustomFetch   UrlFetchCaller
	Followage     FollowageLookup
	AccountAge    AccountAgeLookup
	Uptime        UptimeLookup
	StreamInfo    StreamInfoLookup
	ChannelCounts ChannelCountsLookup
	Viewers       ViewerLookup
	Log           *zap.Logger
	Timers        TimersStore
	ChatLines     ChatLineCounter
	Automod       *automod.Gate
	Reputation    Reputation
	Campaign      Campaign
	Queue         QueueStore
	SongQueue     SongQueueStore
	Raffle        RaffleStore
	Duel          DuelStore
	Quotes        QuotesStore
	Loyalty       LoyaltyStore
	LoyaltyTick   LoyaltyTicker
	Stats         CounterBumper
	PublicBaseURL string
	Personality   PersonalityStore
	EmotePlay     EmotePlayStore
	Emotes        scope.EmoteSource
	Dedup         *EventDedup
	Seq           *Sequencer
	Nuke          *Nuke
}

type FeedCounts struct {
	Today int64
	Total int64
}

type FeedBoardEntry struct {
	BroadcasterID uint64
	Name          string
	Count         int64
}

type FeedBoard struct {
	Entries []FeedBoardEntry
	Ranked  uint64
	Channel int64
	Rank    uint64
}

type PersonalityStore interface {
	FactCursor(ctx context.Context, broadcasterID uint64) (int64, error)
	Feed(ctx context.Context, broadcasterID uint64, name, eventID string) (FeedCounts, error)
	FeedBoard(ctx context.Context, broadcasterID uint64, limit int) (FeedBoard, error)
	Mood(ctx context.Context, broadcasterID uint64, candidate string) (string, error)
}

type FeedTotals struct {
	Total   int64
	Channel int64
	Rank    uint64
}

type FeedTotalPersister interface {
	FeedBump(ctx context.Context, broadcasterID uint64, name, eventID string) (FeedTotals, error)
	FeedBoard(ctx context.Context, broadcasterID uint64, limit int) (FeedBoard, error)
}

type EmotePlayStore interface {
	Bump(ctx context.Context, u EmotePlayUpdate) (EmotePlayResult, error)
}

type IsLiveChecker interface {
	IsLive(ctx context.Context, broadcasterID uint64) (bool, error)
}

type LiveStore interface {
	IsLiveChecker
	SetLive(ctx context.Context, broadcasterID uint64, version int64) (applied bool, err error)
	ClearLive(ctx context.Context, broadcasterID uint64, version int64) (applied bool, err error)
}

type GreetStore interface {
	FirstGreet(ctx context.Context, broadcasterID uint64, chatterID string) (bool, error)
	ResetGreets(ctx context.Context, broadcasterID uint64) error
}

type CooldownStore interface {
	Allow(ctx context.Context, key string, ttl time.Duration) (bool, error)
}

type Viewer struct {
	ID    uint64
	Login string
	Name  string
}

type CounterBump struct {
	BroadcasterID uint64
	Name          string
	Viewer        Viewer
	Command       string
	Delta         int64
}

type LoyaltyStore interface {
	Earn(broadcasterID, viewerID uint64, login, name string, points int64, watchSeconds uint64)
	CounterBump(ctx context.Context, b CounterBump) (int64, error)
	CounterPeek(ctx context.Context, target CounterTarget) (loyaltyrpc.Counter, bool, error)
	BalanceGet(ctx context.Context, broadcasterID, viewerID uint64) (loyaltyrpc.Balance, error)
	BalanceAdjust(ctx context.Context, broadcasterID uint64, viewerLogin string, value int64, absolute bool) (loyaltyrpc.Balance, bool, error)
	BalanceSpend(ctx context.Context, broadcasterID uint64, viewerLogin string, amount int64) (bal loyaltyrpc.Balance, found, spent bool, err error)
	BalanceTransfer(ctx context.Context, broadcasterID, fromViewerID uint64, targetLogin string, amount int64) (bal loyaltyrpc.Balance, found, moved bool, err error)
	Top(ctx context.Context, broadcasterID uint64, limit int) ([]loyaltyrpc.Balance, error)
	CounterCreate(ctx context.Context, broadcasterID uint64, name, scope string) (loyaltyrpc.Counter, error)
	CounterSet(ctx context.Context, broadcasterID uint64, name string, viewerID uint64, command string, value int64) (bool, error)
	CounterDelete(ctx context.Context, broadcasterID uint64, name string) error
	CounterList(ctx context.Context, broadcasterID uint64) ([]loyaltyrpc.Counter, error)
}

type CounterBumper interface {
	BumpBot(name string, delta int64)
	BumpChannel(broadcasterID uint64, name string, delta int64)
}

type LoyaltyTicker interface {
	Arm(ctx context.Context, broadcasterID uint64)
	Disarm(ctx context.Context, broadcasterID uint64)
}

type TimersStore interface {
	ArmAll(ctx context.Context, broadcasterID uint64)
	DisarmAll(ctx context.Context, broadcasterID uint64)
}

type ChatLineCounter interface {
	CountChatLine(ctx context.Context, broadcasterID uint64)
}

type NoopCooldown struct{}

func (NoopCooldown) Allow(context.Context, string, time.Duration) (bool, error) { return true, nil }
