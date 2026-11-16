// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package web

import (
	"context"
	"errors"
	"net/http"

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

	donation, ok := s.kofiDonation(w, r)
	if !ok {
		return
	}
	if donation.Test() {
		w.WriteHeader(http.StatusOK)
		return
	}
	if err := s.cfg.Donations.RecordDonation(ctx, donation); err != nil {
		monitor.TxnLogger(ctx, s.log).Error("ko-fi donation save failed", zap.String("transaction_id", donation.TransactionID), zap.Error(err))
		sendJSON(w, http.StatusInternalServerError, errorBody{Error: "failed to save donation"})
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (s *Server) kofiDonation(w http.ResponseWriter, r *http.Request) (kofi.Donation, bool) {
	log := monitor.TxnLogger(r.Context(), s.log)
	if s.cfg.KofiToken == "" || s.cfg.Donations == nil {
		log.Warn("ko-fi webhook not configured")
		sendJSON(w, http.StatusServiceUnavailable, errorBody{Error: "webhook not configured"})
		return kofi.Donation{}, false
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	if err := r.ParseForm(); err != nil {
		sendJSON(w, http.StatusBadRequest, errorBody{Error: "bad form"})
		return kofi.Donation{}, false
	}
	donation, err := kofi.Parse([]byte(r.PostForm.Get("data")), s.cfg.KofiToken)
	if errors.Is(err, kofi.ErrToken) {
		log.Warn("ko-fi webhook token rejected", zap.String("remote", r.RemoteAddr))
		w.WriteHeader(http.StatusUnauthorized)
		return kofi.Donation{}, false
	}
	if err != nil {
		log.Warn("ko-fi webhook payload rejected", zap.Error(err))
		sendJSON(w, http.StatusBadRequest, errorBody{Error: err.Error()})
		return kofi.Donation{}, false
	}
	return donation, true
}
