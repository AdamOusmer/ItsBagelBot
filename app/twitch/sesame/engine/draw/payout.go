// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package draw

import (
	"context"
	"errors"
	"strconv"

	"go.uber.org/zap"
)

func payWin(ctx context.Context, s *Store, w slotWork) Slot {
	slot := w.slot
	err := s.cfg.Payer.Pay(ctx, Payment{
		Channel: w.round.Channel, Viewer: slot.Viewer, Login: slot.Login, Instance: slot.Instance,
		Points: w.round.Terms.Prize.Points / int64(w.round.Winners),
		Key:    PaymentKey("raffle:" + string(w.round.ID) + ":" + strconv.Itoa(slot.N)),
	})
	switch {
	case err == nil:
		return finalSlot(slot, SlotPaid)
	case errors.Is(err, ErrPaymentRejected):
		return finalSlot(slot, SlotUnpaid)
	default:
		s.cfg.Log.Warn("draw payout failed, retrying", zap.Uint64("channel", uint64(w.round.Channel)),
			zap.String("round", string(w.round.ID)), zap.Int("slot", slot.N), zap.Error(err))
		return retry(slot, w.now.Add(payoutRetry))
	}
}

func expireWin(_ context.Context, _ *Store, w slotWork) Slot {
	return finalSlot(w.slot, SlotNoShow)
}
