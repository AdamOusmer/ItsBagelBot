package web

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"ItsBagelBot/app/transactions/repository"
	billingrpc "ItsBagelBot/internal/domain/rpc/billing"

	"github.com/gofiber/fiber/v2"
	"go.uber.org/zap"
)

type Store interface {
	SaveWebhookEvent(ctx context.Context, event repository.WebhookEvent) error
}

// GiftNotice describes a gifted entitlement that just landed: who received it,
// who paid, and the webhook id (used as the idempotency key so Tebex's webhook
// retries never duplicate the notification).
type GiftNotice struct {
	WebhookID     string
	RecipientID   uint64
	GiftedByID    uint64
	GiftedByLogin string
}

type Config struct {
	WebhookSecret string
	Ready         func() bool
	// NotifyGift is called after a gifted payment is recorded (initial payment
	// only, not renewals). Best-effort: failures are logged, never surfaced to
	// Tebex — the entitlement is already durable at that point.
	NotifyGift func(ctx context.Context, notice GiftNotice) error
	// ApplyBilling synchronously updates the users service after signature
	// verification. Returning an error makes Tebex retry the webhook, so a
	// transient NATS/users outage cannot lose a paid entitlement.
	ApplyBilling func(ctx context.Context, req billingrpc.ApplyRequest) error
}

type Server struct {
	store Store
	cfg   Config
	log   *zap.Logger
}

type tebexEvent struct {
	ID      string          `json:"id"`
	Type    string          `json:"type"`
	Date    string          `json:"date"`
	Subject json.RawMessage `json:"subject"`
}

type paymentSubject struct {
	TransactionID             string                     `json:"transaction_id"`
	RecurringPaymentReference string                     `json:"recurring_payment_reference"`
	Custom                    map[string]json.RawMessage `json:"custom"`
	Customer                  struct {
		Username usernameRef `json:"username"`
	} `json:"customer"`
	Products []struct {
		Custom    map[string]json.RawMessage `json:"custom"`
		Username  usernameRef                `json:"username"`
		ExpiresAt string                     `json:"expires_at"`
	} `json:"products"`
}

type recurringSubject struct {
	Reference      string          `json:"reference"`
	NextPaymentAt  string          `json:"next_payment_at"`
	InitialPayment *paymentSubject `json:"initial_payment"`
	LastPayment    *paymentSubject `json:"last_payment"`
}

type usernameRef struct {
	ID json.RawMessage `json:"id"`
}

type recordablePayment struct {
	TransactionID string
	UserID        uint64
	// Gift attribution from the basket custom payload; zero for self-purchases.
	GiftedByID         uint64
	GiftedByLogin      string
	RecurringReference string
	ExpiresAt          *time.Time
}

var (
	errNoRecordablePayment = errors.New("no recordable Tebex payment for event")
	errPaymentUserMissing  = errors.New("payment user id missing")
)

func New(store Store, cfg Config, log *zap.Logger) *fiber.App {

	if log == nil {
		log = zap.NewNop()
	}

	s := &Server{
		store: store,
		cfg:   cfg,
		log:   log,
	}

	app := fiber.New(fiber.Config{
		BodyLimit:             256 * 1024,
		DisableStartupMessage: true,
		ReadTimeout:           5 * time.Second,
		WriteTimeout:          10 * time.Second,
	})

	app.Get("/healthz", s.health)
	app.Get("/readyz", s.ready)
	app.Get("/drain", s.drain)
	app.Get("/tebex", s.tebexReachable)
	app.Get("/tebex/", s.tebexReachable)
	app.Get("/webhooks/tebex", s.tebexReachable)
	app.Get("/webhooks/tebex/", s.tebexReachable)
	app.Post("/tebex", s.tebexWebhook)
	app.Post("/tebex/", s.tebexWebhook)
	app.Post("/webhooks/tebex", s.tebexWebhook)
	app.Post("/webhooks/tebex/", s.tebexWebhook)

	return app
}

func (s *Server) health(c *fiber.Ctx) error {
	return c.SendString("ok\n")
}

func (s *Server) ready(c *fiber.Ctx) error {
	if s.cfg.Ready != nil && !s.cfg.Ready() {
		return c.Status(fiber.StatusServiceUnavailable).SendString("not ready\n")
	}
	return c.SendString("ok\n")
}

func (s *Server) drain(c *fiber.Ctx) error {
	time.Sleep(10 * time.Second)
	return c.SendString("ok\n")
}

func (s *Server) tebexReachable(c *fiber.Ctx) error {
	return c.SendString("ok\n")
}

func (s *Server) tebexWebhook(c *fiber.Ctx) error {

	if s.cfg.WebhookSecret == "" {
		s.log.Warn("tebex webhook secret not configured")
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "webhook not configured"})
	}

	body := c.BodyRaw()
	if !verifyTebexSignature(body, c.Get("X-Signature"), s.cfg.WebhookSecret) {
		s.log.Warn("tebex webhook signature rejected", zap.String("remote", c.IP()))
		return c.SendStatus(fiber.StatusUnauthorized)
	}

	var event tebexEvent
	if err := json.Unmarshal(body, &event); err != nil {
		s.log.Warn("tebex webhook json rejected", zap.Error(err))
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "bad json"})
	}
	if event.ID == "" || event.Type == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "webhook id and type are required"})
	}

	ctx := c.UserContext()

	switch event.Type {
	case "validation.webhook":
		if err := s.saveEvent(ctx, event, repository.WebhookValidation, recordablePayment{}, ""); err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to save webhook state"})
		}
		return c.Status(fiber.StatusOK).JSON(fiber.Map{"id": event.ID})

	case "payment.completed", "recurring-payment.started", "recurring-payment.renewed":
		payment, err := recordableFromEvent(event)
		if err != nil {
			return s.failEvent(c, event, payment, fiber.StatusUnprocessableEntity, err)
		}
		if err := s.applyBilling(ctx, event, payment, billingrpc.ActionActivate); err != nil {
			return s.failEvent(c, event, payment, fiber.StatusInternalServerError, err)
		}
		if err := s.saveEvent(ctx, event, repository.WebhookProcessed, payment, ""); err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to save webhook state"})
		}
		s.notifyGift(ctx, event, payment)
		return c.SendStatus(fiber.StatusNoContent)

	case "recurring-payment.cancellation.requested":
		return s.applyLifecycle(c, event, billingrpc.ActionCancelRequested)

	case "recurring-payment.cancellation.aborted", "payment.dispute.won":
		return s.applyLifecycle(c, event, billingrpc.ActionCancelAborted)

	case "recurring-payment.ended", "payment.refunded", "payment.dispute.opened", "payment.dispute.lost":
		return s.applyLifecycle(c, event, billingrpc.ActionRevoke)
	}

	// The trial webhooks exist in the Tebex panel but not (yet) in their docs,
	// so match by prefix rather than exact strings.
	if strings.HasPrefix(event.Type, "recurring-payment.trial") {
		return s.trialLifecycle(c, event)
	}

	// Everything else changes no entitlement and is audited as ignored:
	// payment.declined (nothing was granted), payment.dispute.closed (won/lost
	// carry the outcome), recurring-payment status changes (renewed/ended carry
	// the transitions we act on), and any future types.
	if err := s.saveEvent(ctx, event, repository.WebhookIgnored, recordablePayment{}, ""); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to save webhook state"})
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// trialLifecycle maps trial webhooks onto the billing actions: a started trial
// activates premium until the trial's next payment date, a cancelled trial
// keeps the entitlement but marks the cancellation pending (the expiry safety
// net or a recurring-payment.ended does the actual revoke at trial end), and
// the rest (ending-soon reminder, trial ended) change nothing — a conversion
// to paid arrives as payment.completed / recurring-payment.renewed.
func (s *Server) trialLifecycle(c *fiber.Ctx, event tebexEvent) error {

	var action billingrpc.Action
	switch {
	case strings.Contains(event.Type, "cancel"):
		action = billingrpc.ActionCancelRequested
	case strings.Contains(event.Type, "start"):
		action = billingrpc.ActionActivate
	default:
		if err := s.saveEvent(c.UserContext(), event, repository.WebhookIgnored, recordablePayment{}, ""); err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to save webhook state"})
		}
		return c.SendStatus(fiber.StatusNoContent)
	}

	payment, err := recordableFromEvent(event)
	if err != nil {
		// A trial subject may carry no payment (nothing has been charged), so
		// attribution can be impossible. Retries cannot fix that — audit the
		// event and acknowledge instead of making Tebex redeliver forever.
		if errors.Is(err, errNoRecordablePayment) || errors.Is(err, errPaymentUserMissing) {
			s.log.Warn("tebex trial webhook without attributable payment",
				zap.String("webhook_id", event.ID),
				zap.String("webhook_type", event.Type),
				zap.Error(err),
			)
			if err := s.saveEvent(c.UserContext(), event, repository.WebhookIgnored, payment, err.Error()); err != nil {
				return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to save webhook state"})
			}
			return c.SendStatus(fiber.StatusNoContent)
		}
		return s.failEvent(c, event, payment, fiber.StatusUnprocessableEntity, err)
	}

	if err := s.applyBilling(c.UserContext(), event, payment, action); err != nil {
		return s.failEvent(c, event, payment, fiber.StatusInternalServerError, err)
	}
	if err := s.saveEvent(c.UserContext(), event, repository.WebhookProcessed, payment, ""); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to save webhook state"})
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (s *Server) applyLifecycle(c *fiber.Ctx, event tebexEvent, action billingrpc.Action) error {
	payment, err := recordableFromEvent(event)
	if err != nil {
		return s.failEvent(c, event, payment, fiber.StatusUnprocessableEntity, err)
	}
	if err := s.applyBilling(c.UserContext(), event, payment, action); err != nil {
		return s.failEvent(c, event, payment, fiber.StatusInternalServerError, err)
	}
	if err := s.saveEvent(c.UserContext(), event, repository.WebhookProcessed, payment, ""); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to save webhook state"})
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (s *Server) applyBilling(ctx context.Context, event tebexEvent, payment recordablePayment, action billingrpc.Action) error {
	if s.cfg.ApplyBilling == nil {
		return errors.New("billing entitlement handler not configured")
	}
	occurredAt, err := time.Parse(time.RFC3339, event.Date)
	if err != nil {
		return fmt.Errorf("invalid webhook date: %w", err)
	}
	expiresAt := payment.ExpiresAt
	if action == billingrpc.ActionActivate && expiresAt == nil {
		// One-time purchases (single-month buys, gifts) can arrive without any
		// expiry on the payment subject, but a paid month must still run out —
		// an activation without expiry would never be revoked by the safety
		// net. Default to one month from the payment event.
		fallback := occurredAt.AddDate(0, 1, 0)
		expiresAt = &fallback
	}
	return s.cfg.ApplyBilling(ctx, billingrpc.ApplyRequest{
		UserID:             payment.UserID,
		EventID:            event.ID,
		Action:             action,
		OccurredAt:         occurredAt,
		ExpiresAt:          expiresAt,
		RecurringReference: payment.RecurringReference,
	})
}

func (s *Server) failEvent(c *fiber.Ctx, event tebexEvent, payment recordablePayment, status int, cause error) error {

	if err := s.saveEvent(c.UserContext(), event, repository.WebhookFailed, payment, cause.Error()); err != nil {
		s.log.Error("failed to persist tebex webhook failure",
			zap.String("webhook_id", event.ID),
			zap.String("webhook_type", event.Type),
			zap.Error(err),
		)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to save webhook state"})
	}

	s.log.Warn("tebex webhook failed",
		zap.String("webhook_id", event.ID),
		zap.String("webhook_type", event.Type),
		zap.String("transaction_id", payment.TransactionID),
		zap.Uint64("user_id", payment.UserID),
		zap.Error(cause),
	)
	return c.Status(status).JSON(fiber.Map{"error": cause.Error()})
}

// notifyGift tells the recipient their gifted premium landed. Initial payments
// only — a renewal keeps the plan running and does not warrant a ping. Never
// fails the webhook: the entitlement is already recorded.
func (s *Server) notifyGift(ctx context.Context, event tebexEvent, payment recordablePayment) {

	if s.cfg.NotifyGift == nil || payment.GiftedByID == 0 {
		return
	}
	if event.Type == "recurring-payment.renewed" {
		return
	}

	err := s.cfg.NotifyGift(ctx, GiftNotice{
		WebhookID:     event.ID,
		RecipientID:   payment.UserID,
		GiftedByID:    payment.GiftedByID,
		GiftedByLogin: payment.GiftedByLogin,
	})
	if err != nil {
		s.log.Warn("gift notification failed",
			zap.String("webhook_id", event.ID),
			zap.Uint64("recipient", payment.UserID),
			zap.Uint64("gifted_by", payment.GiftedByID),
			zap.Error(err),
		)
	}
}

func (s *Server) saveEvent(ctx context.Context, event tebexEvent, status repository.WebhookStatus, payment recordablePayment, message string) error {

	return s.store.SaveWebhookEvent(ctx, repository.WebhookEvent{
		ID:            event.ID,
		Type:          event.Type,
		Status:        status,
		TransactionID: payment.TransactionID,
		UserID:        payment.UserID,
		Error:         message,
	})
}

func verifyTebexSignature(body []byte, signature string, secret string) bool {

	provided, err := hex.DecodeString(strings.TrimSpace(signature))
	if err != nil {
		return false
	}
	expected := tebexSignature(body, secret)
	return hmac.Equal(provided, expected)
}

func tebexSignature(body []byte, secret string) []byte {

	bodyHash := sha256.Sum256(body)
	bodyHashHex := make([]byte, hex.EncodedLen(len(bodyHash)))
	hex.Encode(bodyHashHex, bodyHash[:])

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(bodyHashHex)
	return mac.Sum(nil)
}

func recordableFromEvent(event tebexEvent) (recordablePayment, error) {

	switch {
	case strings.HasPrefix(event.Type, "payment."):
		var payment paymentSubject
		if err := json.Unmarshal(event.Subject, &payment); err != nil {
			return recordablePayment{}, err
		}
		return recordableFromPayment(payment)

	case strings.HasPrefix(event.Type, "recurring-payment."):
		var recurring recurringSubject
		if err := json.Unmarshal(event.Subject, &recurring); err != nil {
			return recordablePayment{}, err
		}
		var payment recordablePayment
		var err error
		if event.Type == "recurring-payment.renewed" && recurring.LastPayment != nil {
			payment, err = recordableFromPayment(*recurring.LastPayment)
		} else if recurring.InitialPayment != nil {
			payment, err = recordableFromPayment(*recurring.InitialPayment)
		} else if recurring.LastPayment != nil {
			payment, err = recordableFromPayment(*recurring.LastPayment)
		} else {
			return recordablePayment{}, errNoRecordablePayment
		}
		if err != nil {
			return recordablePayment{}, err
		}
		payment.RecurringReference = recurring.Reference
		if expiresAt, ok := parseTebexTime(recurring.NextPaymentAt); ok {
			payment.ExpiresAt = &expiresAt
		}
		return payment, nil

	default:
		return recordablePayment{}, errNoRecordablePayment
	}
}

func recordableFromPayment(payment paymentSubject) (recordablePayment, error) {

	if payment.TransactionID == "" {
		return recordablePayment{}, errors.New("payment transaction_id missing")
	}

	if userID, ok := userIDFromPayment(payment); ok {
		giftedBy, giftedByLogin := giftFromPayment(payment)
		// A basket gifted to yourself is a plain purchase; drop the marker.
		if giftedBy == userID {
			giftedBy, giftedByLogin = 0, ""
		}
		result := recordablePayment{
			TransactionID:      payment.TransactionID,
			UserID:             userID,
			GiftedByID:         giftedBy,
			GiftedByLogin:      giftedByLogin,
			RecurringReference: payment.RecurringPaymentReference,
		}
		for _, product := range payment.Products {
			if expiresAt, ok := parseTebexTime(product.ExpiresAt); ok && (result.ExpiresAt == nil || expiresAt.After(*result.ExpiresAt)) {
				result.ExpiresAt = &expiresAt
			}
		}
		return result, nil
	}

	return recordablePayment{TransactionID: payment.TransactionID}, errPaymentUserMissing
}

func parseTebexTime(raw string) (time.Time, bool) {
	if raw == "" {
		return time.Time{}, false
	}
	parsed, err := time.Parse(time.RFC3339, raw)
	return parsed, err == nil
}

// giftFromPayment reads the gifted_by attribution the basket carried. Checked
// on the payment-level custom payload first, then per-product (mirrors
// userIDFromPayment's search order).
func giftFromPayment(payment paymentSubject) (uint64, string) {

	if id, ok := rawUint(payment.Custom["gifted_by"]); ok {
		return id, rawString(payment.Custom["gifted_by_login"])
	}
	for _, product := range payment.Products {
		if id, ok := rawUint(product.Custom["gifted_by"]); ok {
			return id, rawString(product.Custom["gifted_by_login"])
		}
	}
	return 0, ""
}

func rawString(raw json.RawMessage) string {
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return ""
	}
	return value
}

func userIDFromPayment(payment paymentSubject) (uint64, bool) {

	if userID, ok := userIDFromCustom(payment.Custom); ok {
		return userID, true
	}
	if userID, ok := rawUint(payment.Customer.Username.ID); ok {
		return userID, true
	}

	for _, product := range payment.Products {
		if userID, ok := userIDFromCustom(product.Custom); ok {
			return userID, true
		}
		if userID, ok := rawUint(product.Username.ID); ok {
			return userID, true
		}
	}

	return 0, false
}

func userIDFromCustom(custom map[string]json.RawMessage) (uint64, bool) {

	for _, key := range []string{"user_id", "twitch_user_id", "broadcaster_user_id"} {
		if userID, ok := rawUint(custom[key]); ok {
			return userID, true
		}
	}
	return 0, false
}

func rawUint(raw json.RawMessage) (uint64, bool) {

	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return 0, false
	}

	if raw[0] == '"' {
		var value string
		if err := json.Unmarshal(raw, &value); err != nil {
			return 0, false
		}
		parsed, err := strconv.ParseUint(value, 10, 64)
		return parsed, err == nil && parsed != 0
	}

	var number json.Number
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&number); err != nil {
		return 0, false
	}
	parsed, err := strconv.ParseUint(number.String(), 10, 64)
	return parsed, err == nil && parsed != 0
}
