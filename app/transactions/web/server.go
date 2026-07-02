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

	"github.com/gofiber/fiber/v2"
	"go.uber.org/zap"
)

type Store interface {
	Record(ctx context.Context, id string, userID uint64) error
	SaveWebhookEvent(ctx context.Context, event repository.WebhookEvent) error
}

type Config struct {
	WebhookSecret string
	Ready         func() bool
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
	TransactionID string                     `json:"transaction_id"`
	Custom        map[string]json.RawMessage `json:"custom"`
	Customer      struct {
		Username usernameRef `json:"username"`
	} `json:"customer"`
	Products []struct {
		Custom   map[string]json.RawMessage `json:"custom"`
		Username usernameRef                `json:"username"`
	} `json:"products"`
}

type recurringSubject struct {
	Reference      string          `json:"reference"`
	InitialPayment *paymentSubject `json:"initial_payment"`
	LastPayment    *paymentSubject `json:"last_payment"`
}

type usernameRef struct {
	ID json.RawMessage `json:"id"`
}

type recordablePayment struct {
	TransactionID string
	UserID        uint64
}

var errNoRecordablePayment = errors.New("no recordable Tebex payment for event")

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
	app.Post("/tebex", s.tebexWebhook)
	app.Post("/webhooks/tebex", s.tebexWebhook)

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
		if err := s.store.Record(ctx, payment.TransactionID, payment.UserID); err != nil {
			return s.failEvent(c, event, payment, fiber.StatusInternalServerError, err)
		}
		if err := s.saveEvent(ctx, event, repository.WebhookProcessed, payment, ""); err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to save webhook state"})
		}
		return c.SendStatus(fiber.StatusNoContent)
	}

	if unsupportedActionable(event.Type) {
		return s.failEvent(c, event, recordablePayment{}, fiber.StatusNotImplemented, fmt.Errorf("handler not implemented for %s", event.Type))
	}

	if err := s.saveEvent(ctx, event, repository.WebhookIgnored, recordablePayment{}, ""); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to save webhook state"})
	}
	return c.SendStatus(fiber.StatusNoContent)
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

	switch event.Type {
	case "payment.completed":
		var payment paymentSubject
		if err := json.Unmarshal(event.Subject, &payment); err != nil {
			return recordablePayment{}, err
		}
		return recordableFromPayment(payment)

	case "recurring-payment.started", "recurring-payment.renewed":
		var recurring recurringSubject
		if err := json.Unmarshal(event.Subject, &recurring); err != nil {
			return recordablePayment{}, err
		}
		if event.Type == "recurring-payment.renewed" && recurring.LastPayment != nil {
			return recordableFromPayment(*recurring.LastPayment)
		}
		if recurring.InitialPayment != nil {
			return recordableFromPayment(*recurring.InitialPayment)
		}
		if recurring.LastPayment != nil {
			return recordableFromPayment(*recurring.LastPayment)
		}
		return recordablePayment{}, errNoRecordablePayment

	default:
		return recordablePayment{}, errNoRecordablePayment
	}
}

func recordableFromPayment(payment paymentSubject) (recordablePayment, error) {

	if payment.TransactionID == "" {
		return recordablePayment{}, errors.New("payment transaction_id missing")
	}

	if userID, ok := userIDFromPayment(payment); ok {
		return recordablePayment{TransactionID: payment.TransactionID, UserID: userID}, nil
	}

	return recordablePayment{TransactionID: payment.TransactionID}, errors.New("payment user id missing")
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

func unsupportedActionable(eventType string) bool {

	switch eventType {
	case "payment.refunded",
		"payment.dispute.opened",
		"payment.dispute.won",
		"payment.dispute.lost",
		"payment.dispute.closed",
		"recurring-payment.ended",
		"recurring-payment.cancellation.requested",
		"recurring-payment.cancellation.aborted":
		return true
	default:
		return false
	}
}
