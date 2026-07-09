package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"ItsBagelBot/app/transactions/repository"
	billingrpc "ItsBagelBot/internal/domain/rpc/billing"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

// maxBodyBytes caps the webhook request body; Tebex payloads are far smaller.
const maxBodyBytes = 256 * 1024

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
	// GiftMessage is the buyer's optional personal note from the basket custom
	// payload; empty falls back to the default gift email copy.
	GiftMessage string
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

func New(store Store, cfg Config, log *zap.Logger) http.Handler {

	if log == nil {
		log = zap.NewNop()
	}

	s := &Server{
		store: store,
		cfg:   cfg,
		log:   log,
	}

	r := chi.NewRouter()

	r.Get("/healthz", s.health)
	r.Get("/readyz", s.ready)
	r.Get("/drain", s.drain)
	r.Get("/tebex", s.tebexReachable)
	r.Get("/tebex/", s.tebexReachable)
	r.Get("/webhooks/tebex", s.tebexReachable)
	r.Get("/webhooks/tebex/", s.tebexReachable)
	r.Post("/tebex", s.tebexWebhook)
	r.Post("/tebex/", s.tebexWebhook)
	r.Post("/webhooks/tebex", s.tebexWebhook)
	r.Post("/webhooks/tebex/", s.tebexWebhook)

	return r
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	sendOK(w)
}

func (s *Server) ready(w http.ResponseWriter, _ *http.Request) {
	if s.cfg.Ready != nil && !s.cfg.Ready() {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = io.WriteString(w, "not ready\n")
		return
	}
	sendOK(w)
}

func (s *Server) drain(w http.ResponseWriter, _ *http.Request) {
	time.Sleep(10 * time.Second)
	sendOK(w)
}

func (s *Server) tebexReachable(w http.ResponseWriter, _ *http.Request) {
	sendOK(w)
}

// tebexWebhook owns only the HTTP-and-verification concern: authenticate the
// request, then route the event to its handler. Event classification lives in
// tebexevent.go; the per-outcome work lives in the dispatch helpers below.
func (s *Server) tebexWebhook(w http.ResponseWriter, r *http.Request) {

	body, ok := s.verifiedBody(w, r)
	if !ok {
		return
	}

	var event tebexEvent
	if err := json.Unmarshal(body, &event); err != nil {
		s.log.Warn("tebex webhook json rejected", zap.Error(err))
		sendJSON(w, http.StatusBadRequest, errorBody{Error: "bad json"})
		return
	}
	if event.ID == "" || event.Type == "" {
		sendJSON(w, http.StatusBadRequest, errorBody{Error: "webhook id and type are required"})
		return
	}

	ctx := r.Context()
	if event.Type == "validation.webhook" {
		s.handleValidation(ctx, w, event)
		return
	}
	if spec, ok := billingEventActions[event.Type]; ok {
		s.processBillingEvent(ctx, w, event, spec.action, spec.notify)
		return
	}
	// Trial webhooks exist in the Tebex panel but not (yet) in their docs, so
	// match by prefix rather than exact strings.
	if strings.HasPrefix(event.Type, "recurring-payment.trial") {
		s.trialLifecycle(ctx, w, event)
		return
	}
	// Everything else changes no entitlement and is audited as ignored:
	// payment.declined (nothing was granted), payment.dispute.closed (won/lost
	// carry the outcome), status changes, and any future types.
	s.auditIgnored(ctx, w, event)
}

// verifiedBody reads the capped request body and checks the Tebex signature.
// On failure it writes the rejection response and returns ok=false.
func (s *Server) verifiedBody(w http.ResponseWriter, r *http.Request) ([]byte, bool) {

	if s.cfg.WebhookSecret == "" {
		s.log.Warn("tebex webhook secret not configured")
		sendJSON(w, http.StatusServiceUnavailable, errorBody{Error: "webhook not configured"})
		return nil, false
	}

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err != nil {
		s.log.Warn("tebex webhook body rejected", zap.Error(err))
		sendJSON(w, http.StatusRequestEntityTooLarge, errorBody{Error: "body too large"})
		return nil, false
	}

	if !verifyTebexSignature(body, r.Header.Get("X-Signature"), s.cfg.WebhookSecret) {
		s.log.Warn("tebex webhook signature rejected", zap.String("remote", r.RemoteAddr))
		w.WriteHeader(http.StatusUnauthorized)
		return nil, false
	}
	return body, true
}

// processBillingEvent is the single path every entitlement-changing event
// follows: parse the payment, apply the billing action, persist the processed
// audit row, and (for activations) notify the gift recipient. A parse or apply
// failure is recorded and surfaced so Tebex retries; a persist failure alone
// returns 500.
func (s *Server) processBillingEvent(ctx context.Context, w http.ResponseWriter, event tebexEvent, action billingrpc.Action, notify bool) {

	payment, err := recordableFromEvent(event)
	if err != nil {
		s.failEvent(ctx, w, event, payment, http.StatusUnprocessableEntity, err)
		return
	}
	if err := s.applyBilling(ctx, event, payment, action); err != nil {
		s.failEvent(ctx, w, event, payment, http.StatusInternalServerError, err)
		return
	}
	if err := s.saveEvent(ctx, event, repository.WebhookProcessed, payment, ""); err != nil {
		s.saveError(w)
		return
	}
	if notify {
		s.notifyGift(ctx, event, payment)
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleValidation acknowledges Tebex's endpoint-validation ping: record it and
// echo the id back.
func (s *Server) handleValidation(ctx context.Context, w http.ResponseWriter, event tebexEvent) {
	if err := s.saveEvent(ctx, event, repository.WebhookValidation, recordablePayment{}, ""); err != nil {
		s.saveError(w)
		return
	}
	sendJSON(w, http.StatusOK, map[string]string{"id": event.ID})
}

// auditIgnored records an event that changes no entitlement and acknowledges it.
func (s *Server) auditIgnored(ctx context.Context, w http.ResponseWriter, event tebexEvent) {
	if err := s.saveEvent(ctx, event, repository.WebhookIgnored, recordablePayment{}, ""); err != nil {
		s.saveError(w)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// saveError is the one response for a failed audit-log write, which makes Tebex
// retry the delivery.
func (s *Server) saveError(w http.ResponseWriter) {
	sendJSON(w, http.StatusInternalServerError, errorBody{Error: "failed to save webhook state"})
}

// trialLifecycle maps trial webhooks onto the billing actions: a started trial
// activates premium until the trial's next payment date, a cancelled trial
// keeps the entitlement but marks the cancellation pending (the expiry safety
// net or a recurring-payment.ended does the actual revoke at trial end), and
// the rest (ending-soon reminder, trial ended) change nothing — a conversion
// to paid arrives as payment.completed / recurring-payment.renewed.
func (s *Server) trialLifecycle(ctx context.Context, w http.ResponseWriter, event tebexEvent) {

	action, ok := trialAction(event.Type)
	if !ok {
		s.auditIgnored(ctx, w, event)
		return
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
			if err := s.saveEvent(ctx, event, repository.WebhookIgnored, payment, err.Error()); err != nil {
				s.saveError(w)
				return
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		s.failEvent(ctx, w, event, payment, http.StatusUnprocessableEntity, err)
		return
	}

	if err := s.applyBilling(ctx, event, payment, action); err != nil {
		s.failEvent(ctx, w, event, payment, http.StatusInternalServerError, err)
		return
	}
	if err := s.saveEvent(ctx, event, repository.WebhookProcessed, payment, ""); err != nil {
		s.saveError(w)
		return
	}
	w.WriteHeader(http.StatusNoContent)
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
		// Non-zero only on a gift payment (gifts are one-time "single" packages,
		// so this is set on the initial payment.completed and never on renewals);
		// lets the users service count the gift against the buyer.
		GifterID: payment.GiftedByID,
	})
}

func (s *Server) failEvent(ctx context.Context, w http.ResponseWriter, event tebexEvent, payment recordablePayment, status int, cause error) {

	if err := s.saveEvent(ctx, event, repository.WebhookFailed, payment, cause.Error()); err != nil {
		s.log.Error("failed to persist tebex webhook failure",
			zap.String("webhook_id", event.ID),
			zap.String("webhook_type", event.Type),
			zap.Error(err),
		)
		s.saveError(w)
		return
	}

	s.log.Warn("tebex webhook failed",
		zap.String("webhook_id", event.ID),
		zap.String("webhook_type", event.Type),
		zap.String("transaction_id", payment.TransactionID),
		zap.Uint64("user_id", payment.UserID),
		zap.Error(cause),
	)
	sendJSON(w, status, errorBody{Error: cause.Error()})
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
		GiftMessage:   payment.GiftMessage,
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

type errorBody struct {
	Error string `json:"error"`
}

func sendOK(w http.ResponseWriter) {
	_, _ = io.WriteString(w, "ok\n")
}

func sendJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
