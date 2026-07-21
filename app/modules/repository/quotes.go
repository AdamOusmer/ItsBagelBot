package repository

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"strings"
	"time"

	"ItsBagelBot/app/modules/ent"
	"ItsBagelBot/app/modules/ent/quote"
	modulesrpc "ItsBagelBot/internal/domain/rpc/modules"

	"go.uber.org/zap"
)

// QuoteTextMaxLen bounds one quote's body. It matches the ent column and keeps
// the chat readout ("Quote #NNNN: text (2026-07-10)") safely under Twitch's
// 500-character message cap.
const QuoteTextMaxLen = 450

// ErrQuoteTooLong rejects an over-long quote at the validation boundary so the
// RPC returns a stable message the module can translate for chat.
var ErrQuoteTooLong = fmt.Errorf("quote text exceeds %d characters", QuoteTextMaxLen)

// ErrQuoteEmpty rejects a blank quote body.
var ErrQuoteEmpty = errors.New("quote text is empty")

// quoteAddAttempts bounds the add retry loop. Two adds racing for the same
// max+1 number collide on the (user_id, number) unique index; the loser
// re-reads the max and tries again. Channel-sized contention makes more than
// one retry vanishingly rare.
const quoteAddAttempts = 3

// Quotes is the channel-quotes store: plain reads and writes on the ent
// client. Unlike Modules there is no cache or write-behind here — a quote is
// read once per !quote invocation and written by a moderator a few times per
// stream, so the DB round-trip is the simple and correct shape.
type Quotes struct {
	client *ent.Client
	log    *zap.Logger
}

// NewQuotes returns the quotes store.
func NewQuotes(client *ent.Client, log *zap.Logger) *Quotes {
	return &Quotes{client: client, log: log.Named("quotes")}
}

// quoteView converts one row to the wire shape.
func quoteView(q *ent.Quote) *modulesrpc.Quote {
	return &modulesrpc.Quote{
		Number:    q.Number,
		Text:      q.Text,
		AddedBy:   q.AddedBy,
		CreatedAt: q.CreatedAt.UTC().Format(time.RFC3339),
	}
}

// QuoteDraft is the complete input for a new quote. CreatedAt is optional:
// chat saves leave it zero to use the current time, while the dashboard can
// preserve the day on which the quote was originally said.
type QuoteDraft struct {
	Text      string
	AddedBy   string
	CreatedAt time.Time
}

// Add saves a new quote under the channel's next number (max+1) and returns
// it. Removing a quote leaves a hole, so a number chat has already seen is
// never reassigned to different text — except the top number, which max+1
// reuses after a remove.
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
		lastErr = err // number raced; re-read the max and retry
	}
	return nil, fmt.Errorf("quote add: numbering contention: %w", lastErr)
}

// nextNumber reads the channel's highest quote number and returns it plus one
// (1 for the first quote).
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

// Get returns quote #number; found=false when it does not exist.
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

// Random returns a uniformly random quote of the channel; found=false when
// none are saved. Count-then-offset keeps it portable across dialects; a
// channel's quote list is small enough that the offset scan is negligible.
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
		// A remove raced between the count and the read; treat as empty.
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return quoteView(row), true, nil
}

// Search returns a random quote whose text contains term (case-insensitive);
// found=false when nothing matches. Random-among-matches mirrors Mix It Up's
// "!quote <word>" and keeps a popular word from always landing on the same
// quote. Count-then-offset like Random; the matching set is at most the
// channel's book, so the scan stays negligible.
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
		// A remove raced between the count and the read; treat as no match.
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return quoteView(row), true, nil
}

// QuoteUpdate is the changeable content of an existing quote. Text is
// required; CreatedAt is optional (zero keeps the saved date) so the
// dashboard can correct the day a quote was said. The number never changes,
// so chat references stay stable across an edit.
type QuoteUpdate struct {
	Text      string
	CreatedAt time.Time
}

// Update rewrites quote #number in place; found=false when the number does
// not exist.
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

// List returns the channel's whole quote book, lowest number first, for the
// dashboard management page. A channel's book is small (a handful to a few
// hundred), so returning it whole is fine.
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

// Remove deletes quote #number; found=false when it did not exist.
func (q *Quotes) Remove(ctx context.Context, userID, number uint64) (bool, error) {
	n, err := q.client.Quote.Delete().
		Where(quote.UserID(userID), quote.Number(number)).
		Exec(ctx)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// DeleteAllForUser removes every quote of a deleted account (the user_deleted
// event path, alongside the module-rows sweep).
func (q *Quotes) DeleteAllForUser(ctx context.Context, userID uint64) error {
	_, err := q.client.Quote.Delete().Where(quote.UserID(userID)).Exec(ctx)
	return err
}
