// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
	"time"

	"ItsBagelBot/app/db/modules/ent"
	"ItsBagelBot/app/db/modules/ent/quote"
	"ItsBagelBot/internal/domain/event/data"
	modulesrpc "ItsBagelBot/internal/domain/rpc/modules"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/bus/consumers"
	"ItsBagelBot/pkg/cache"

	"go.uber.org/zap"
)

const QuoteTextMaxLen = 450

var ErrQuoteTooLong = fmt.Errorf("quote text exceeds %d characters", QuoteTextMaxLen)

var ErrQuoteEmpty = errors.New("quote text is empty")

const quoteAddAttempts = 3

const (
	quotesKeyPrefix = "quotes:"

	quotesCacheTTL = 5 * time.Minute

	quotesCacheCapacity int64 = 1024
)

type Quotes struct {
	client *ent.Client
	views  *cache.Cache[[]modulesrpc.Quote]
	pub    bus.Publisher
	log    *zap.Logger
}

func NewQuotes(client *ent.Client, pub bus.Publisher, log *zap.Logger) *Quotes {
	return &Quotes{
		client: client,
		views:  cache.New[[]modulesrpc.Quote](quotesCacheCapacity, quotesCacheTTL, cache.StaleOnError(quotesCacheTTL)),
		pub:    pub,
		log:    log.Named("quotes"),
	}
}

func (q *Quotes) OnChanged() func(*bus.Message) error {
	return consumers.OnChangeInvalidate(quoteChangedUserID, func(userID uint64) {
		q.views.Invalidate(cache.UserKey(quotesKeyPrefix, userID))
	})
}

func quoteChangedUserID(dto data.QuoteChangedDTO) uint64 { return dto.UserID }

// Announce must follow every quote write: the other replicas only drop their cached list on this event.
func (q *Quotes) Announce(ctx context.Context, userID uint64) {
	q.views.Invalidate(cache.UserKey(quotesKeyPrefix, userID))
	if err := bus.PublishJSON(ctx, q.pub, data.SubjectQuoteChanged, data.QuoteChangedDTO{UserID: userID}); err != nil {
		q.log.Error("failed to publish quote change", zap.Uint64("user_id", userID), zap.Error(err))
	}
}

func (q *Quotes) Close() {
	q.views.Close()
}

func (q *Quotes) cached(ctx context.Context, userID uint64) ([]modulesrpc.Quote, error) {
	return q.views.GetOrLoad(ctx, cache.UserKey(quotesKeyPrefix, userID), func(ctx context.Context) ([]modulesrpc.Quote, error) {
		rows, err := q.client.Quote.Query().
			Where(quote.UserID(userID)).
			Order(ent.Asc(quote.FieldNumber)).
			All(ctx)
		if err != nil {
			return nil, err
		}
		out := make([]modulesrpc.Quote, len(rows))
		for i, row := range rows {
			out[i] = *quoteView(row)
		}
		return out, nil
	})
}

func quoteView(q *ent.Quote) *modulesrpc.Quote {
	return &modulesrpc.Quote{
		Number:    q.Number,
		Text:      q.Text,
		AddedBy:   q.AddedBy,
		CreatedAt: q.CreatedAt.UTC().Format(time.RFC3339),
	}
}

type QuoteDraft struct {
	Text      string
	AddedBy   string
	CreatedAt time.Time
}

func (q *Quotes) Add(ctx context.Context, userID uint64, draft QuoteDraft) (*modulesrpc.Quote, error) {
	draft.Text = strings.TrimSpace(draft.Text)
	if draft.Text == "" {
		return nil, ErrQuoteEmpty
	}
	if len(draft.Text) > QuoteTextMaxLen {
		return nil, ErrQuoteTooLong
	}
	if draft.CreatedAt.IsZero() {
		draft.CreatedAt = time.Now()
	}

	var lastErr error
	for range quoteAddAttempts {
		next, err := q.nextNumber(ctx, userID)
		if err != nil {
			return nil, err
		}
		row, err := q.client.Quote.Create().
			SetUserID(userID).
			SetNumber(next).
			SetText(draft.Text).
			SetAddedBy(draft.AddedBy).
			SetCreatedAt(draft.CreatedAt.UTC()).
			Save(ctx)
		if err == nil {
			q.Announce(ctx, userID)
			return quoteView(row), nil
		}
		if !ent.IsConstraintError(err) {
			return nil, err
		}
		lastErr = err
	}
	return nil, fmt.Errorf("quote add: numbering contention: %w", lastErr)
}

func (q *Quotes) nextNumber(ctx context.Context, userID uint64) (uint64, error) {
	last, err := q.client.Quote.Query().
		Where(quote.UserID(userID)).
		Order(ent.Desc(quote.FieldNumber)).
		Select(quote.FieldNumber).
		First(ctx)
	switch {
	case ent.IsNotFound(err):
		return 1, nil
	case err != nil:
		return 0, err
	}
	return last.Number + 1, nil
}

func (q *Quotes) Get(ctx context.Context, userID, number uint64) (*modulesrpc.Quote, bool, error) {
	quotes, err := q.cached(ctx, userID)
	if err != nil {
		return nil, false, err
	}
	i := slices.IndexFunc(quotes, func(view modulesrpc.Quote) bool { return view.Number == number })
	if i < 0 {
		return nil, false, nil
	}
	view := quotes[i]
	return &view, true, nil
}

func (q *Quotes) Random(ctx context.Context, userID uint64) (*modulesrpc.Quote, bool, error) {
	quotes, err := q.cached(ctx, userID)
	if err != nil || len(quotes) == 0 {
		return nil, false, err
	}
	view := quotes[rand.IntN(len(quotes))]
	return &view, true, nil
}

func (q *Quotes) Search(ctx context.Context, userID uint64, term string) (*modulesrpc.Quote, bool, error) {
	term = strings.TrimSpace(term)
	if term == "" {
		return nil, false, nil
	}
	match := q.client.Quote.Query().
		Where(quote.UserID(userID), quote.TextContainsFold(term))
	n, err := match.Count(ctx)
	if err != nil {
		return nil, false, err
	}
	if n == 0 {
		return nil, false, nil
	}
	row, err := match.
		Order(ent.Asc(quote.FieldNumber)).
		Offset(rand.IntN(n)).
		First(ctx)
	if ent.IsNotFound(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return quoteView(row), true, nil
}

type QuoteUpdate struct {
	Text      string
	CreatedAt time.Time
}

func (q *Quotes) Update(ctx context.Context, userID, number uint64, upd QuoteUpdate) (*modulesrpc.Quote, bool, error) {
	upd.Text = strings.TrimSpace(upd.Text)
	if upd.Text == "" {
		return nil, false, ErrQuoteEmpty
	}
	if len(upd.Text) > QuoteTextMaxLen {
		return nil, false, ErrQuoteTooLong
	}
	row, err := q.client.Quote.Query().
		Where(quote.UserID(userID), quote.Number(number)).
		Only(ctx)
	switch {
	case ent.IsNotFound(err):
		return nil, false, nil
	case err != nil:
		return nil, false, err
	}
	write := row.Update().SetText(upd.Text)
	if !upd.CreatedAt.IsZero() {
		write.SetCreatedAt(upd.CreatedAt.UTC())
	}
	row, err = write.Save(ctx)
	if err != nil {
		return nil, false, err
	}
	q.Announce(ctx, userID)
	return quoteView(row), true, nil
}

func (q *Quotes) List(ctx context.Context, userID uint64) ([]modulesrpc.Quote, error) {
	quotes, err := q.cached(ctx, userID)
	return slices.Clone(quotes), err
}

func (q *Quotes) Remove(ctx context.Context, userID, number uint64) (bool, error) {
	n, err := q.client.Quote.Delete().
		Where(quote.UserID(userID), quote.Number(number)).
		Exec(ctx)
	if err != nil {
		return false, err
	}
	if n > 0 {
		q.Announce(ctx, userID)
	}
	return n > 0, nil
}

func (q *Quotes) DeleteAllForUser(ctx context.Context, userID uint64) error {
	_, err := q.client.Quote.Delete().Where(quote.UserID(userID)).Exec(ctx)
	if err == nil {
		q.Announce(ctx, userID)
	}
	return err
}
