// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package web

import (
	"context"
	"errors"
	"net/http"
	"time"

	"ItsBagelBot/app/db/transactions/kofi"
	"ItsBagelBot/pkg/monitor"

	"go.uber.org/zap"
)

type DonationRecorder interface {
	RecordDonation(ctx context.Context, donation kofi.Donation) error
}

func (s *Server) kofiWebhook(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), webhookBudget)
	defer cancel()
	log := monitor.TxnLogger(ctx, s.log)

	if !s.cfg.KofiToken.Configured() || s.cfg.Donations == nil {
		log.Warn("ko-fi webhook not configured")
		sendJSON(w, http.StatusServiceUnavailable, errorBody{Error: "webhook not configured"})
		return
	}
	data, ok := kofiData(w, r)
	if !ok {
		return
	}
	donation, err := kofi.Parse(data, s.cfg.KofiToken, time.Now())
	if err != nil {
		s.kofiRejected(ctx, w, err)
		return
	}
	s.kofiRecord(ctx, w, donation)
}

func kofiData(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	err := r.ParseForm()
	if err == nil {
		return []byte(r.PostForm.Get("data")), true
	}
	status, message := http.StatusBadRequest, "bad form"
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		status, message = http.StatusRequestEntityTooLarge, "body too large"
	}
	sendJSON(w, status, errorBody{Error: message})
	return nil, false
}

func (s *Server) kofiRejected(ctx context.Context, w http.ResponseWriter, err error) {
	log := monitor.TxnLogger(ctx, s.log)
	switch {
	case errors.Is(err, kofi.ErrUnsupported), errors.Is(err, kofi.ErrStale):
		log.Warn("ko-fi webhook event ignored", zap.Error(err))
		w.WriteHeader(http.StatusOK)
	case errors.Is(err, kofi.ErrToken):
		log.Warn("ko-fi webhook token rejected")
		w.WriteHeader(http.StatusUnauthorized)
	case errors.Is(err, kofi.ErrMalformed):
		log.Warn("ko-fi webhook data rejected", zap.Error(err))
		sendJSON(w, http.StatusBadRequest, errorBody{Error: err.Error()})
	default:
		log.Error("ko-fi webhook payload rejected", zap.Error(err))
		sendJSON(w, http.StatusBadRequest, errorBody{Error: err.Error()})
	}
}

func (s *Server) kofiRecord(ctx context.Context, w http.ResponseWriter, donation kofi.Donation) {
	log := monitor.TxnLogger(ctx, s.log).With(
		zap.String("kofi_transaction_id", donation.TransactionID),
		zap.String("kofi_message_id", donation.MessageID),
	)
	if donation.Test() {
		log.Info("ko-fi test webhook acknowledged")
		w.WriteHeader(http.StatusOK)
		return
	}
	if err := s.cfg.Donations.RecordDonation(ctx, donation); err != nil {
		log.Error("ko-fi donation save failed", zap.Error(err))
		sendJSON(w, http.StatusInternalServerError, errorBody{Error: "failed to save donation"})
		return
	}
	log.Info("ko-fi donation recorded", zap.String("kind", string(donation.Kind)), zap.String("currency", donation.Currency))
	w.WriteHeader(http.StatusOK)
}
