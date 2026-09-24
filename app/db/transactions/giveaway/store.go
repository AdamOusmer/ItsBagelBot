// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package giveaway

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"ItsBagelBot/app/db/transactions/ent"
	"ItsBagelBot/app/db/transactions/ent/giveaway"
	"ItsBagelBot/app/db/transactions/ent/giveawayaward"
	"ItsBagelBot/app/db/transactions/ent/giveawaycandidate"
	"ItsBagelBot/app/db/transactions/ent/giveawaydraw"
	"ItsBagelBot/app/db/transactions/ent/giveawayoutbox"
	"ItsBagelBot/internal/domain/rpc/giveaways"
	"ItsBagelBot/pkg/codec"
)

var (
	ErrCampaignState = errors.New("campaign is not in the required lifecycle state")
	ErrVersion       = errors.New("campaign version is stale")
	ErrPoolDigest    = errors.New("candidate pool digest changed")
	ErrAwardLeased   = errors.New("award fulfillment is currently leased")
)

type Store struct {
	DB           *ent.Client
	RandomReader ioReader
}
type ioReader interface{ Read([]byte) (int, error) }

func NewStore(db *ent.Client) *Store { return &Store{DB: db} }

func (s *Store) Create(ctx context.Context, req giveaways.CreateRequest, rulesVersion string, now time.Time) (*ent.Giveaway, error) {
	if err := validateCreate(req); err != nil {
		return nil, err
	}
	actor, err := parseID(req.ActorID)
	if err != nil {
		return nil, err
	}
	if existing, found, err := s.idempotentCampaign(ctx, req, actor); err != nil || found {
		return existing, err
	}
	id := uuid.NewString()
	created, err := s.DB.Giveaway.Create().SetID(id).SetIdempotencyKey(req.IdempotencyKey).SetTitle(req.Title).SetReason(req.Reason).SetRulesVersion(rulesVersion).SetWinnerCount(req.WinnerCount).SetPrizeMonths(req.PrizeMonths).SetCreatedBy(actor).SetCreatedAt(now).SetUpdatedAt(now).Save(ctx)
	if err == nil || !ent.IsConstraintError(err) {
		return created, err
	}
	return s.idempotentResult(ctx, req, actor, err)
}

// The conditional update is the cross-replica fence; never clear a live lease from a prior read.
func (s *Store) RetryAward(ctx context.Context, awardID string, now time.Time) error {
	if awardID == "" {
		return errors.New("award id required")
	}
	row, err := s.DB.GiveawayOutbox.Query().Where(giveawayoutbox.AggregateIDEQ(awardID), giveawayoutbox.EventTypeEQ("award.fulfill")).Only(ctx)
	if err != nil {
		return err
	}
	if row.State == "processing" && row.LeaseUntil.After(now) {
		return ErrAwardLeased
	}
	ready := giveawayoutbox.Or(
		giveawayoutbox.StateIn("queued", "retry", "completed"),
		giveawayoutbox.And(giveawayoutbox.StateEQ("processing"), giveawayoutbox.Or(giveawayoutbox.LeaseUntilIsNil(), giveawayoutbox.LeaseUntilLTE(now))),
	)
	_, err = row.Update().Where(ready).SetState("queued").SetLastError("").ClearLeaseUntil().SetLeaseOwner("").ClearNextAttemptAt().SetUpdatedAt(now).Save(ctx)
	return err
}

func validateCreate(req giveaways.CreateRequest) error {
	if err := ValidateMonths(req.PrizeMonths); err != nil {
		return err
	}
	if req.WinnerCount <= 0 {
		return ErrInvalidWinnerCount(req.WinnerCount)
	}
	if req.Title == "" || req.Mutation.ActorID == "" {
		return errors.New("title and actor required")
	}
	if req.IdempotencyKey == "" {
		return errors.New("idempotency key required")
	}
	return nil
}

func (s *Store) idempotentCampaign(ctx context.Context, req giveaways.CreateRequest, actor uint64) (*ent.Giveaway, bool, error) {
	existing, err := s.DB.Giveaway.Query().Where(giveaway.IdempotencyKeyEQ(req.IdempotencyKey)).Only(ctx)
	if ent.IsNotFound(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if !sameCreatePayload(existing, req, actor) {
		return nil, true, ErrVersion
	}
	return existing, true, nil
}

func (s *Store) idempotentResult(ctx context.Context, req giveaways.CreateRequest, actor uint64, fallback error) (*ent.Giveaway, error) {
	existing, found, err := s.idempotentCampaign(ctx, req, actor)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fallback
	}
	return existing, nil
}

func sameCreatePayload(existing *ent.Giveaway, req giveaways.CreateRequest, actor uint64) bool {
	return existing.Title == req.Title && existing.Reason == req.Reason && existing.WinnerCount == req.WinnerCount && existing.PrizeMonths == req.PrizeMonths && existing.CreatedBy == actor
}

// Membership comes from Users, never the browser; the request carries only the approved digest.
func (s *Store) FreezeCandidates(ctx context.Context, req giveaways.FreezeRequest, candidates []Candidate, now time.Time) (*ent.Giveaway, error) {
	if err := validateFreezeRequest(req); err != nil {
		return nil, err
	}
	tx, err := s.DB.Tx(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	updated, err := freezeOperation{store: s, tx: tx, request: req, candidates: candidates, now: now}.run(ctx)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return updated, nil
}

func (s *Store) FreezeReplay(ctx context.Context, req giveaways.FreezeRequest) (*ent.Giveaway, bool, error) {
	campaign, err := s.DB.Giveaway.Get(ctx, req.CampaignID)
	if err != nil {
		return nil, false, err
	}
	if !freezeReplay(campaign, req) {
		return nil, false, nil
	}
	return campaign, true, nil
}

type freezeOperation struct {
	store      *Store
	tx         *ent.Tx
	request    giveaways.FreezeRequest
	candidates []Candidate
	now        time.Time
}

func (operation freezeOperation) run(ctx context.Context) (*ent.Giveaway, error) {
	campaign, err := lockedCampaign(ctx, operation.tx, operation.request.CampaignID)
	if err != nil {
		return nil, err
	}
	if replay := freezeReplay(campaign, operation.request); replay {
		return campaign, nil
	}
	if err = validateFreezeState(campaign, operation.request); err != nil {
		return nil, err
	}
	if err = validateFreezePool(operation.request, operation.candidates); err != nil {
		return nil, err
	}
	if err = (candidateBatch{tx: operation.tx, campaignID: campaign.ID, digest: operation.request.PoolDigest, candidates: operation.candidates}).save(ctx); err != nil {
		return nil, err
	}
	updated, err := campaign.Update().SetStatus(string(giveaways.StatusFrozen)).SetFrozenAt(operation.now).SetFreezeIdempotencyKey(freezeKey(operation.request)).SetFrozenPoolDigest(operation.request.PoolDigest).SetVersion(campaign.Version + 1).SetUpdatedAt(operation.now).Save(ctx)
	if err != nil {
		return nil, err
	}
	return updated, nil
}

func freezeReplay(campaign *ent.Giveaway, req giveaways.FreezeRequest) bool {
	return campaign.Status == string(giveaways.StatusFrozen) && campaign.FreezeIdempotencyKey == freezeKey(req) && campaign.FrozenPoolDigest == req.PoolDigest
}

func validateFreezeState(campaign *ent.Giveaway, req giveaways.FreezeRequest) error {
	if campaign.Status != string(giveaways.StatusDraft) {
		return ErrCampaignState
	}
	if req.ExpectedVersion != 0 && campaign.Version != req.ExpectedVersion {
		return ErrVersion
	}
	return nil
}

func validateFreezeRequest(req giveaways.FreezeRequest) error {
	if req.CampaignID == "" || req.PoolDigest == "" {
		return errors.New("campaign and pool digest required")
	}
	return nil
}

func validateFreezePool(req giveaways.FreezeRequest, candidates []Candidate) error {
	digest, err := PoolDigest(candidates)
	if err != nil {
		return err
	}
	if digest != req.PoolDigest {
		return ErrPoolDigest
	}
	return nil
}

func freezeKey(req giveaways.FreezeRequest) string {
	if req.IdempotencyKey != "" {
		return req.IdempotencyKey
	}
	return "freeze:" + req.PoolDigest
}

type candidateBatch struct {
	tx         *ent.Tx
	campaignID string
	digest     string
	candidates []Candidate
}

func (batch candidateBatch) save(ctx context.Context) error {
	for _, candidate := range batch.candidates {
		facts, err := codec.Marshal(candidate.Eligibility)
		if err != nil {
			return err
		}
		_, err = batch.tx.GiveawayCandidate.Create().SetID(uuid.NewString()).SetGiveawayID(batch.campaignID).SetUserID(candidate.UserID).SetTwitchLogin(candidate.Username).SetEligibilityJSON(string(facts)).SetPoolDigest(batch.digest).SetEligible(candidate.Eligible).SetExclusionReason(candidate.ExclusionReason).Save(ctx)
		if err != nil {
			return err
		}
	}
	return nil
}

type DrawResult struct {
	Draw   *ent.GiveawayDraw
	Awards []*ent.GiveawayAward
}

func (s *Store) Draw(ctx context.Context, req giveaways.DrawRequest, now time.Time) (*DrawResult, error) {
	if err := validateDraw(req); err != nil {
		return nil, err
	}
	tx, err := s.DB.Tx(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	result, err := s.drawTransaction(ctx, tx, req, now)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Store) drawTransaction(ctx context.Context, tx *ent.Tx, req giveaways.DrawRequest, now time.Time) (*DrawResult, error) {
	campaign, err := lockedCampaign(ctx, tx, req.CampaignID)
	if err != nil {
		return nil, err
	}
	if replay, found, replayErr := replayDraw(ctx, tx, campaign.ID, req.IdempotencyKey); found || replayErr != nil {
		return replay, replayErr
	}
	if err = validateDrawState(campaign, req); err != nil {
		return nil, err
	}
	return drawOperation{store: s, tx: tx, campaign: campaign, request: req, now: now}.run(ctx)
}

type drawOperation struct {
	store    *Store
	tx       *ent.Tx
	campaign *ent.Giveaway
	request  giveaways.DrawRequest
	now      time.Time
}

func (operation drawOperation) run(ctx context.Context) (*DrawResult, error) {
	digest, chosen, candidateCount, err := operation.store.selectWinners(ctx, operation.tx, operation.campaign, operation.request)
	if err != nil {
		return nil, err
	}
	draw, err := operation.store.createDraw(ctx, operation.tx, drawCommit{campaign: operation.campaign, request: operation.request, digest: digest, candidateCount: candidateCount, chosen: chosen, now: operation.now})
	if err != nil {
		return nil, err
	}
	awards, err := operation.store.createAwards(ctx, operation.tx, awardBatch{campaign: operation.campaign, draw: draw, chosen: chosen, now: operation.now})
	if err != nil {
		return nil, err
	}
	if _, err = operation.campaign.Update().SetStatus(string(giveaways.StatusDrawn)).SetDrawnAt(operation.now).SetVersion(operation.campaign.Version + 1).SetUpdatedAt(operation.now).Save(ctx); err != nil {
		return nil, err
	}
	return &DrawResult{Draw: draw, Awards: awards}, nil
}

func validateDrawState(campaign *ent.Giveaway, req giveaways.DrawRequest) error {
	if campaign.Status != string(giveaways.StatusFrozen) {
		return ErrCampaignState
	}
	if req.ExpectedVersion != 0 && campaign.Version != req.ExpectedVersion {
		return ErrVersion
	}
	return nil
}

func validateDraw(req giveaways.DrawRequest) error {
	if req.CampaignID == "" || req.IdempotencyKey == "" {
		return errors.New("campaign and idempotency key required")
	}
	return nil
}

func replayDraw(ctx context.Context, tx *ent.Tx, campaignID, operationKey string) (*DrawResult, bool, error) {
	existing, err := tx.GiveawayDraw.Query().Where(giveawaydraw.GiveawayIDEQ(campaignID), giveawaydraw.OperationKeyEQ(operationKey)).Only(ctx)
	if ent.IsNotFound(err) {
		existing, err = tx.GiveawayDraw.Query().Where(giveawaydraw.GiveawayIDEQ(campaignID)).Only(ctx)
	}
	if ent.IsNotFound(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	awards, err := tx.GiveawayAward.Query().Where(giveawayaward.DrawIDEQ(existing.ID)).Order(ent.Asc(giveawayaward.FieldOrdinal)).All(ctx)
	if err != nil {
		return nil, false, err
	}
	return &DrawResult{Draw: existing, Awards: awards}, true, nil
}

func (s *Store) selectWinners(ctx context.Context, tx *ent.Tx, campaign *ent.Giveaway, req giveaways.DrawRequest) (string, []Candidate, int, error) {
	rows, err := tx.GiveawayCandidate.Query().Where(giveawaycandidate.GiveawayIDEQ(campaign.ID), giveawaycandidate.EligibleEQ(true)).Order(ent.Asc(giveawaycandidate.FieldUserID)).All(ctx)
	if err != nil {
		return "", nil, 0, err
	}
	if len(rows) < campaign.WinnerCount {
		return "", nil, 0, ErrTooManyWinners
	}
	pool := make([]Candidate, len(rows))
	for i, row := range rows {
		pool[i] = Candidate{UserID: row.UserID, Username: row.TwitchLogin}
	}
	digest, err := PoolDigest(pool)
	if err != nil {
		return "", nil, 0, err
	}
	if req.PoolDigest != "" && req.PoolDigest != digest {
		return "", nil, 0, ErrPoolDigest
	}
	chosen, err := DrawWithReader(readerOrDefault(s.RandomReader), pool, campaign.WinnerCount)
	return digest, chosen, len(pool), err
}

type drawCommit struct {
	campaign       *ent.Giveaway
	request        giveaways.DrawRequest
	digest         string
	candidateCount int
	chosen         []Candidate
	now            time.Time
}

func (s *Store) createDraw(ctx context.Context, tx *ent.Tx, commit drawCommit) (*ent.GiveawayDraw, error) {
	ids := make([]uint64, len(commit.chosen))
	for i, candidate := range commit.chosen {
		ids[i] = candidate.UserID
	}
	winnerJSON, _ := codec.Marshal(ids)
	auditJSON, _ := codec.Marshal(map[string]any{"candidate_count": commit.candidateCount, "winner_count": len(commit.chosen), "committed_at": commit.now.UTC()})
	actor, err := parseID(commit.request.ActorID)
	if err != nil {
		return nil, err
	}
	return tx.GiveawayDraw.Create().SetID(uuid.NewString()).SetGiveawayID(commit.campaign.ID).SetOperationKey(commit.request.IdempotencyKey).SetPoolDigest(commit.digest).SetAlgorithmVersion(DrawAlgorithmVersion).SetAuditJSON(string(auditJSON)).SetWinnerIdsJSON(string(winnerJSON)).SetActorID(actor).SetCreatedAt(commit.now).Save(ctx)
}

type awardBatch struct {
	campaign *ent.Giveaway
	draw     *ent.GiveawayDraw
	chosen   []Candidate
	now      time.Time
}

func (s *Store) createAwards(ctx context.Context, tx *ent.Tx, batch awardBatch) ([]*ent.GiveawayAward, error) {
	awards := make([]*ent.GiveawayAward, 0, len(batch.chosen))
	for i, winner := range batch.chosen {
		award, err := tx.GiveawayAward.Create().SetID(uuid.NewString()).SetGiveawayID(batch.campaign.ID).SetDrawID(batch.draw.ID).SetUserID(winner.UserID).SetOrdinal(uint64(i + 1)).SetPrizeMonths(batch.campaign.PrizeMonths).SetIntervalRule("provider-monthly-unverified").SetSelectedAt(batch.now).Save(ctx)
		if err != nil {
			return nil, err
		}
		awards = append(awards, award)
		payload, _ := codec.Marshal(map[string]string{"award_id": award.ID, "campaign_id": batch.campaign.ID})
		for _, eventType := range []string{"award.fulfill", "award.email.selection"} {
			if _, err = tx.GiveawayOutbox.Create().SetID(uuid.NewString()).SetAggregateID(award.ID).SetEventType(eventType).SetPayloadJSON(string(payload)).Save(ctx); err != nil {
				return nil, err
			}
		}
	}
	return awards, nil
}

func parseID(raw string) (uint64, error) {
	var id uint64
	if _, err := fmt.Sscan(raw, &id); err != nil || id == 0 {
		return 0, errors.New("actor id must be a positive integer")
	}
	return id, nil
}

func lockedCampaign(ctx context.Context, tx *ent.Tx, id string) (*ent.Giveaway, error) {
	campaign, err := tx.Giveaway.Query().Where(giveaway.IDEQ(id)).ForUpdate().Only(ctx)
	if err == nil || !strings.Contains(err.Error(), "FOR UPDATE/SHARE not supported") {
		return campaign, err
	}
	return tx.Giveaway.Query().Where(giveaway.IDEQ(id)).Only(ctx)
}
func readerOrDefault(r ioReader) ioReader {
	if r == nil {
		return cryptoReader{}
	}
	return r
}

type cryptoReader struct{}

func (cryptoReader) Read(p []byte) (int, error) { return rand.Read(p) }
