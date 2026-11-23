// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package campaign

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
	"golang.org/x/time/rate"

	"ItsBagelBot/app/db/users/repository"
	"ItsBagelBot/internal/domain/consent"
	"ItsBagelBot/pkg/email"
)

const (
	DefaultRate    = 5
	DefaultSupport = "support@itsbagelbot.com"
	DefaultSender  = "ItsBagelBot"

	tickInterval   = 10 * time.Second
	leaseTTL       = time.Minute
	pageSize       = 100
	runnableLimit  = 5
	rateLimitTries = 3
	maxAttempts    = 3
	defaultBackoff = time.Second
	testSubject    = "[Test] "
)

type Sender interface {
	Send(context.Context, email.Message) (email.Receipt, error)
}

type Settings struct {
	From           string
	Origin         string
	MailingAddress string
	SupportEmail   string
	SenderName     string
	Rate           float64
	Sleep          func(context.Context, time.Duration) error
}

type Service struct {
	repo     *repository.Users
	sender   Sender
	settings Settings
	pace     *rate.Limiter
	holder   string
	log      *zap.Logger
	testSeq  atomic.Uint64
}

var holderSeq atomic.Uint64

func New(repo *repository.Users, sender Sender, settings Settings, log *zap.Logger) *Service {
	settings.Origin = strings.TrimRight(settings.Origin, "/")
	settings.SupportEmail = orDefault(settings.SupportEmail, DefaultSupport)
	settings.SenderName = orDefault(settings.SenderName, DefaultSender)
	if settings.Rate <= 0 {
		settings.Rate = DefaultRate
	}
	if settings.Sleep == nil {
		settings.Sleep = sleepContext
	}
	return &Service{
		repo: repo, sender: sender, settings: settings, log: log,
		pace:   rate.NewLimiter(rate.Limit(settings.Rate), 1),
		holder: fmt.Sprintf("users-%d-%d", time.Now().UnixNano(), holderSeq.Add(1)),
	}
}

func orDefault(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func sleepContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (s *Service) Run(ctx context.Context) {
	ticker := time.NewTicker(tickInterval)
	defer ticker.Stop()
	for {
		if err := s.Tick(ctx); err != nil && ctx.Err() == nil {
			s.log.Error("marketing campaign tick failed", zap.Error(err))
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Service) Tick(ctx context.Context) error {
	ids, err := s.repo.CampaignsRunnable(ctx, runnableLimit)
	if err != nil {
		return err
	}
	var failures []error
	for _, id := range ids {
		if err := s.process(ctx, id); err != nil {
			failures = append(failures, fmt.Errorf("campaign %d: %w", id, err))
		}
	}
	return errors.Join(failures...)
}

func (s *Service) lease(id int) repository.CampaignLease {
	return repository.CampaignLease{ID: id, Holder: s.holder, Now: time.Now().UTC(), TTL: leaseTTL}
}

func (s *Service) process(ctx context.Context, id int) error {
	held, err := s.repo.AcquireCampaignLease(ctx, s.lease(id))
	if err != nil || !held {
		return err
	}
	campaign, err := s.repo.CampaignGet(ctx, id)
	if err != nil {
		return err
	}
	cursor := campaign.Cursor
	for {
		page, err := s.repo.MarketingAudience(ctx, cursor, pageSize)
		if err != nil {
			return err
		}
		active, err := s.sendPage(ctx, campaign, page.Recipients)
		if err != nil || !active {
			return err
		}
		if page.Next == 0 {
			return s.repo.CampaignFinish(ctx, s.lease(id))
		}
		cursor = page.Next
		if err := s.repo.CampaignAdvance(ctx, id, cursor); err != nil {
			return err
		}
	}
}

func (s *Service) sendPage(ctx context.Context, campaign repository.Campaign, recipients []repository.AudienceRecipient) (bool, error) {
	for _, recipient := range recipients {
		held, err := s.repo.AcquireCampaignLease(ctx, s.lease(campaign.ID))
		if err != nil || !held {
			return false, err
		}
		if err := s.deliver(ctx, campaign, recipient.UserID); err != nil {
			return false, err
		}
	}
	return true, nil
}

func (s *Service) deliver(ctx context.Context, campaign repository.Campaign, userID uint64) error {
	if err := s.pace.Wait(ctx); err != nil {
		return err
	}
	recipient, err := s.repo.MarketingRecipient(ctx, userID)
	if errors.Is(err, repository.ErrNotEligible) {
		return s.repo.SkipQueuedSend(ctx, campaign.ID, userID)
	}
	if err != nil {
		return err
	}
	ticket, err := s.repo.ClaimSend(ctx, repository.SendClaim{CampaignID: campaign.ID, UserID: userID, AddressHMAC: recipient.AddressHMAC})
	if err != nil || ticket.Status != consent.SendQueued {
		return err
	}
	content, found := repository.CampaignContentFor(campaign.Content, recipient.Locale)
	if !found {
		return s.repo.RecordSend(ctx, repository.SendOutcome{ID: ticket.ID, Status: consent.SendSkipped})
	}
	return s.sendQueued(ctx, campaign.ID, ticket, rendering{recipient: recipient, content: content})
}

func (s *Service) sendQueued(ctx context.Context, campaignID int, ticket repository.SendTicket, r rendering) error {
	for attempt := 1; ; attempt++ {
		receipt, err := s.dispatch(ctx, campaignID, r, ticket.Key)
		if err == nil {
			return s.repo.RecordSend(ctx, repository.SendOutcome{ID: ticket.ID, Status: consent.SendSent, ResendID: receipt.ID})
		}
		if !s.retryable(err, attempt) {
			return s.settleFailure(ctx, ticket, err)
		}
		if err := s.pauseAndRefresh(ctx, &r, backoff(err)); err != nil {
			return s.settleRefresh(ctx, ticket, err)
		}
	}
}

func (s *Service) retryable(err error, attempt int) bool {
	return errors.Is(err, email.ErrRateLimited) && attempt < rateLimitTries
}

func (s *Service) settleFailure(ctx context.Context, ticket repository.SendTicket, cause error) error {
	switch {
	case errors.Is(cause, email.ErrInvalidMessage), errors.Is(cause, email.ErrRejected):
		return s.repo.RecordSend(ctx, repository.SendOutcome{ID: ticket.ID, Status: consent.SendFailed})
	case errors.Is(cause, email.ErrUnconfirmed), errors.Is(cause, email.ErrRateLimited):
		return s.countAttempt(ctx, ticket, cause)
	}
	return cause
}

func (s *Service) countAttempt(ctx context.Context, ticket repository.SendTicket, cause error) error {
	attempts, err := s.repo.RecordAttempt(ctx, ticket.ID)
	if err != nil {
		return err
	}
	if attempts < maxAttempts {
		return cause
	}
	return s.repo.RecordSend(ctx, repository.SendOutcome{ID: ticket.ID, Status: consent.SendFailed})
}

func (s *Service) settleRefresh(ctx context.Context, ticket repository.SendTicket, cause error) error {
	if !errors.Is(cause, repository.ErrNotEligible) {
		return cause
	}
	return s.repo.RecordSend(ctx, repository.SendOutcome{ID: ticket.ID, Status: consent.SendSkipped})
}

func (s *Service) pauseAndRefresh(ctx context.Context, r *rendering, wait time.Duration) error {
	if err := s.settings.Sleep(ctx, wait); err != nil {
		return err
	}
	recipient, err := s.repo.MarketingRecipient(ctx, r.recipient.UserID)
	if err != nil {
		return err
	}
	r.recipient = recipient
	return nil
}

func backoff(err error) time.Duration {
	if wait, ok := email.RetryAfter(err); ok {
		return wait
	}
	return defaultBackoff
}

func (s *Service) dispatch(ctx context.Context, campaignID int, r rendering, key string) (email.Receipt, error) {
	token, err := s.repo.UnsubscribeToken(ctx, r.recipient.UserID)
	if err != nil {
		return email.Receipt{}, err
	}
	r.token = token
	return s.sender.Send(ctx, s.message(campaignID, r, key))
}

func (s *Service) SendTest(ctx context.Context, campaignID int, userID uint64) error {
	campaign, err := s.repo.CampaignGet(ctx, campaignID)
	if err != nil {
		return err
	}
	recipient, err := s.repo.MarketingRecipient(ctx, userID)
	if err != nil {
		return err
	}
	content, found := repository.CampaignContentFor(campaign.Content, recipient.Locale)
	if !found {
		return fmt.Errorf("%w: no content for locale %q", repository.ErrCampaignInvalid, recipient.Locale)
	}
	content.Subject = testSubject + content.Subject
	key := fmt.Sprintf("mkt-test-%d-%d-%d-%d", campaignID, userID, time.Now().UnixNano(), s.testSeq.Add(1))
	_, err = s.dispatch(ctx, campaignID, rendering{recipient: recipient, content: content}, key)
	return err
}
