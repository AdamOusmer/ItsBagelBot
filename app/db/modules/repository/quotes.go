// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"strings"
	"time"

	"ItsBagelBot/app/db/modules/ent"
	"ItsBagelBot/app/db/modules/ent/quote"
	modulesrpc "ItsBagelBot/internal/domain/rpc/modules"

	"go.uber.org/zap"
)

const QuoteTextMaxLen = 450

var ErrQuoteTooLong = fmt.Errorf("quote text exceeds %d characters", QuoteTextMaxLen)

var ErrQuoteEmpty = errors.New("quote text is empty")

const quoteAddAttempts = 3

type Quotes struct {
	client *ent.Client
	log    *zap.Logger
}

func NewQuotes(client *ent.Client, log *zap.Logger) *Quotes {
	return &Quotes{client: client, log: log.Named("quotes")}
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
	row, err := q.client.Quote.Query().
		Where(quote.UserID(userID), quote.Number(number)).
		Only(ctx)
	switch {
	case ent.IsNotFound(err):
		return nil, false, nil
	case err != nil:
		return nil, false, err
	}
	return quoteView(row), true, nil
}

func (q *Quotes) Random(ctx context.Context, userID uint64) (*modulesrpc.Quote, bool, error) {
	n, err := q.client.Quote.Query().Where(quote.UserID(userID)).Count(ctx)
	if err != nil {
		return nil, false, err
	}
	if n == 0 {
		return nil, false, nil
	}
	row, err := q.client.Quote.Query().
		Where(quote.UserID(userID)).
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
	return quoteView(row), true, nil
}

func (q *Quotes) List(ctx context.Context, userID uint64) ([]modulesrpc.Quote, error) {
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
}

func (q *Quotes) Remove(ctx context.Context, userID, number uint64) (bool, error) {
	n, err := q.client.Quote.Delete().
		Where(quote.UserID(userID), quote.Number(number)).
		Exec(ctx)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func (q *Quotes) DeleteAllForUser(ctx context.Context, userID uint64) error {
	_, err := q.client.Quote.Delete().Where(quote.UserID(userID)).Exec(ctx)
	return err
}
