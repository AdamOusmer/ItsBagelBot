// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package giveaway

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	stdmail "net/mail"
	"time"

	"ItsBagelBot/app/db/transactions/ent"
	"ItsBagelBot/app/db/transactions/ent/awardemail"
	"ItsBagelBot/app/db/transactions/ent/giveawayalert"
	"ItsBagelBot/app/db/transactions/mail"
	"ItsBagelBot/pkg/codec"
)

// GiveawayMailer separates durable delivery decisions from template rendering
// and Resend transport. A prepared body contains no recipient address.
type GiveawayMailer interface {
	PrepareGiveaway(mail.GiveawayMessage) (mail.PreparedContent, error)
	DeliverPrepared(context.Context, mail.Delivery) (mail.Receipt, error)
}

var ErrMissingContact = errors.New("winner has no usable contact email")

const emailProviderDedupWindow = 24 * time.Hour

func (e *Engine) awardEmail(ctx context.Context, awardID, kind string) error {
	if e.users == nil || e.mailer == nil {
		return e.emailDependencyFailure(ctx, awardID)
	}
	award, err := e.store.DB.GiveawayAward.Get(ctx, awardID)
	if err != nil {
		return err
	}
	row, err := e.emailRecord(ctx, award, kind)
	if err != nil {
		return err
	}
	if row.State == "accepted" {
		return e.afterEmailAccepted(ctx, row)
	}
	if row.State == "needs_review" {
		return e.updateAwardEmailState(ctx, row, "needs_review")
	}
	return e.sendAwardEmail(ctx, row, award.UserID)
}

func (e *Engine) emailDependencyFailure(ctx context.Context, awardID string) error {
	if err := e.alert(ctx, awardID, "email", "giveaway mail dependencies unavailable"); err != nil {
		return err
	}
	return errors.New("giveaway mail dependencies unavailable")
}

func (e *Engine) emailRecord(ctx context.Context, award *ent.GiveawayAward, kind string) (*ent.AwardEmail, error) {
	row, err := e.store.DB.AwardEmail.Query().Where(awardemail.AwardIDEQ(award.ID), awardemail.KindEQ(kind)).Only(ctx)
	if !ent.IsNotFound(err) {
		return row, err
	}
	coverage, err := e.users.Coverage(ctx, award.UserID)
	if err != nil {
		return nil, errors.New("winner subscription lookup unavailable")
	}
	message := awardMessage(award, recurringReference(coverage) != "")
	message.Locale = coverage.Locale
	message.Confirmation = kind == "confirmation"
	if kind == "confirmation" && message.BillingPending {
		return nil, errors.New("prize confirmation is not ready")
	}
	content, err := e.mailer.PrepareGiveaway(message)
	if err != nil {
		return nil, mail.ErrInvalidMessage
	}
	encoded, err := codec.Marshal(content)
	if err != nil {
		return nil, mail.ErrInvalidMessage
	}
	builder := e.store.DB.AwardEmail.Create().
		SetID(award.ID + ":" + kind).SetAwardID(award.ID).SetKind(kind).
		SetTemplateVersion(mail.GiveawayTemplateVersion).
		SetDeliveryKey("giveaway:" + award.ID + ":" + kind + ":v1").
		SetMonths(message.Months).SetSubscriber(message.Subscriber).
		SetBillingPending(message.BillingPending).
		SetContentJSON(string(encoded)).SetCreatedAt(e.now()).SetUpdatedAt(e.now())
	if !message.Start.IsZero() {
		builder.SetPeriodStart(message.Start).SetPeriodEnd(message.End)
	}
	return builder.Save(ctx)
}

func awardMessage(award *ent.GiveawayAward, subscriber bool) mail.GiveawayMessage {
	confirmed := confirmedAward(award)
	message := mail.GiveawayMessage{Months: award.PrizeMonths, Subscriber: subscriber, BillingPending: !confirmed}
	if confirmed {
		message.Start, message.End = award.ConfirmedStart, award.ConfirmedEnd
	}
	return message
}

func confirmedAward(award *ent.GiveawayAward) bool {
	if award.ConfirmedStart.IsZero() || !award.ConfirmedEnd.After(award.ConfirmedStart) {
		return false
	}
	return award.BillingState == "protected" || award.BillingState == "not_required"
}

func (e *Engine) sendAwardEmail(ctx context.Context, row *ent.AwardEmail, userID uint64) error {
	if row.FirstAttemptAt != nil && !e.now().Before(row.FirstAttemptAt.Add(emailProviderDedupWindow)) {
		return e.reviewEmail(ctx, row, "provider deduplication window expired with acceptance unconfirmed")
	}
	address, err := e.users.Email(ctx, userID)
	if err != nil {
		return errors.New("winner email lookup unavailable")
	}
	if !usableEmail(address) {
		return e.missingEmail(ctx, row)
	}
	if row.RecipientHash != "" && row.RecipientHash != recipientHash(address) {
		return e.reviewEmail(ctx, row, "winner contact changed after the first delivery attempt")
	}
	return e.deliverEmail(ctx, row, address)
}

func usableEmail(value string) bool {
	address, err := stdmail.ParseAddress(value)
	return err == nil && address.Address == value
}

func recipientHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func (e *Engine) missingEmail(ctx context.Context, row *ent.AwardEmail) error {
	if err := e.emailState(ctx, row, "missing_contact", "missing-contact"); err != nil {
		return err
	}
	if err := e.alert(ctx, row.AwardID, "missing-contact", ErrMissingContact.Error()); err != nil {
		return err
	}
	return ErrMissingContact
}

func (e *Engine) reviewEmail(ctx context.Context, row *ent.AwardEmail, reason string) error {
	if err := e.emailState(ctx, row, "needs_review", "ambiguous-delivery"); err != nil {
		return err
	}
	return e.alert(ctx, row.AwardID, "email-ambiguous", reason)
}

func (e *Engine) emailState(ctx context.Context, row *ent.AwardEmail, state, category string) error {
	_, err := row.Update().SetState(state).SetErrorCategory(category).SetUpdatedAt(e.now()).Save(ctx)
	if err != nil {
		return err
	}
	return e.updateAwardEmailState(ctx, row, state)
}

func (e *Engine) updateAwardEmailState(ctx context.Context, row *ent.AwardEmail, state string) error {
	_, err := e.store.DB.GiveawayAward.UpdateOneID(row.AwardID).SetEmailState(state).SetUpdatedAt(e.now()).Save(ctx)
	return err
}

func (e *Engine) deliverEmail(ctx context.Context, row *ent.AwardEmail, address string) error {
	var content mail.PreparedContent
	if codec.Unmarshal([]byte(row.ContentJSON), &content) != nil {
		return e.reviewEmail(ctx, row, "saved email content cannot be recovered")
	}
	// Persist ambiguity before the request: a process exit after sending but
	// before saving the receipt must still observe the original 24-hour window.
	update := row.Update().SetState("uncertain").SetRecipientHash(recipientHash(address)).AddAttempts(1).SetUpdatedAt(e.now())
	if row.FirstAttemptAt == nil {
		update.SetFirstAttemptAt(e.now())
	}
	if _, err := update.Save(ctx); err != nil {
		return err
	}
	receipt, err := e.mailer.DeliverPrepared(ctx, mail.Delivery{To: address, Key: row.DeliveryKey, Content: content})
	if err != nil {
		return e.failedEmailDelivery(ctx, row, err)
	}
	if receipt.ProviderID == "" {
		return e.failedEmailDelivery(ctx, row, mail.ErrUnconfirmed)
	}
	if _, err := row.Update().SetState("accepted").SetProviderMessageID(receipt.ProviderID).SetAcceptedAt(e.now()).SetErrorCategory("").SetLastError("").SetUpdatedAt(e.now()).Save(ctx); err != nil {
		return err
	}
	return e.afterEmailAccepted(ctx, row)
}

func (e *Engine) failedEmailDelivery(ctx context.Context, row *ent.AwardEmail, cause error) error {
	category := "ambiguous-delivery"
	if errors.Is(cause, mail.ErrRateLimited) {
		category = "rate-limited"
	}
	if errors.Is(cause, mail.ErrInvalidMessage) {
		return e.reviewEmail(ctx, row, "saved giveaway message was rejected before delivery")
	}
	if err := e.emailState(ctx, row, "uncertain", category); err != nil {
		return err
	}
	return mail.ErrUnconfirmed
}

func (e *Engine) afterEmailAccepted(ctx context.Context, row *ent.AwardEmail) error {
	if err := e.updateAwardEmailState(ctx, row, "accepted"); err != nil {
		return err
	}
	if _, err := e.store.DB.GiveawayAlert.Update().Where(giveawayalert.AwardIDEQ(row.AwardID), giveawayalert.CategoryIn("email", "missing-contact", "email-ambiguous")).SetState("resolved").Save(ctx); err != nil {
		return err
	}
	if row.Kind == "selection" {
		return e.queueConfirmation(ctx, row.AwardID)
	}
	return nil
}

// queueConfirmation is replayable, including after a crash between the Users
// commit and enqueue. Selection must have been accepted first to preserve order.
func (e *Engine) queueConfirmation(ctx context.Context, awardID string) error {
	award, err := e.store.DB.GiveawayAward.Get(ctx, awardID)
	if err != nil || !confirmedAward(award) {
		return err
	}
	selection, err := e.store.DB.AwardEmail.Query().Where(awardemail.AwardIDEQ(awardID), awardemail.KindEQ("selection")).Only(ctx)
	if ent.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !confirmationNeeded(selection) {
		return nil
	}
	payload, err := codec.Marshal(map[string]string{"award_id": awardID})
	if err != nil {
		return err
	}
	return e.saveConfirmationQueue(ctx, selection, string(payload))
}

func confirmationNeeded(selection *ent.AwardEmail) bool {
	return selection.State == "accepted" && selection.BillingPending && !selection.ConfirmationQueued
}

func (e *Engine) saveConfirmationQueue(ctx context.Context, selection *ent.AwardEmail, payload string) error {
	awardID := selection.AwardID
	tx, err := e.store.DB.Tx(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := tx.GiveawayOutbox.Create().SetID(awardID + ":email:confirmation").SetAggregateID(awardID).
		SetEventType("award.email.confirmation").SetPayloadJSON(payload).
		OnConflict().DoNothing().Exec(ctx); err != nil {
		return err
	}
	if _, err := tx.AwardEmail.UpdateOneID(selection.ID).SetConfirmationQueued(true).Save(ctx); err != nil {
		return err
	}
	return tx.Commit()
}

func (e *Engine) reconcileEmails(ctx context.Context) error {
	rows, err := e.store.DB.AwardEmail.Query().Where(awardemail.KindEQ("selection"), awardemail.StateEQ("accepted"), awardemail.BillingPendingEQ(true), awardemail.ConfirmationQueuedEQ(false)).All(ctx)
	if err != nil {
		return err
	}
	for _, row := range rows {
		if err := e.queueConfirmation(ctx, row.AwardID); err != nil {
			return err
		}
	}
	return nil
}
