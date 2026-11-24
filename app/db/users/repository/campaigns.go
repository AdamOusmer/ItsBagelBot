// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"errors"
	"strings"
	"time"

	"ItsBagelBot/app/db/users/ent"
	"ItsBagelBot/app/db/users/ent/marketingcampaign"
	"ItsBagelBot/app/db/users/ent/predicate"
	"ItsBagelBot/internal/domain/consent"
	"ItsBagelBot/pkg/db"
)

const (
	campaignListLimit  = 100
	campaignNameMax    = 120
	campaignLocaleMax  = 8
	campaignLocalesMax = 16
	campaignActorMax   = 32
)

var (
	ErrCampaignState   = errors.New("campaign state does not allow this change")
	ErrCampaignInvalid = errors.New("campaign is invalid")
)

type Campaign struct {
	ID         int
	Name       string
	Status     consent.CampaignStatus
	Content    map[string]consent.CampaignContent
	CreatedBy  string
	CreatedAt  time.Time
	ApprovedBy string
	ApprovedAt *time.Time
	Revision   int
	Cursor     uint64
	Sends      map[consent.SendStatus]int
}

type CampaignDraft struct {
	ID      int
	Name    string
	Content map[string]consent.CampaignContent
	ActorID string
}

type CampaignApproval struct {
	ID       int
	Revision int
	ActorID  string
}

type CampaignLease struct {
	ID     int
	Holder string
	Now    time.Time
	TTL    time.Duration
}

func (d CampaignDraft) validate() error {
	if !validCampaignName(d.Name) || !validCampaignActor(d.ActorID) {
		return ErrCampaignInvalid
	}
	if len(d.Content) > campaignLocalesMax {
		return ErrCampaignInvalid
	}
	for locale, content := range d.Content {
		if !validCampaignLocale(locale) || !content.Valid() {
			return ErrCampaignInvalid
		}
	}
	return nil
}

func validCampaignName(name string) bool {
	return name != "" && len(name) <= campaignNameMax
}

func validCampaignActor(actor string) bool {
	return actor != "" && len(actor) <= campaignActorMax
}

func validCampaignLocale(locale string) bool {
	return locale != "" && len(locale) <= campaignLocaleMax && locale == strings.ToLower(locale)
}

func contentApprovable(content map[string]consent.CampaignContent) bool {
	fallback, ok := content[consent.DefaultCampaignLocale]
	if !ok || !fallback.Complete() {
		return false
	}
	for _, c := range content {
		if !c.Complete() {
			return false
		}
	}
	return true
}

func campaignFrom(row *ent.MarketingCampaign) Campaign {
	return Campaign{
		ID: row.ID, Name: row.Name, Status: consent.CampaignStatus(row.Status), Content: row.Content,
		CreatedBy: row.CreatedBy, CreatedAt: row.CreatedAt, ApprovedBy: row.ApprovedBy, ApprovedAt: row.ApprovedAt,
		Revision: row.Revision, Cursor: row.AudienceCursor, Sends: map[consent.SendStatus]int{},
	}
}

func campaignStatuses(statuses ...consent.CampaignStatus) []marketingcampaign.Status {
	out := make([]marketingcampaign.Status, len(statuses))
	for i, s := range statuses {
		out[i] = marketingcampaign.Status(s)
	}
	return out
}

func (r *Users) CampaignSave(ctx context.Context, draft CampaignDraft) (Campaign, error) {
	if err := draft.validate(); err != nil {
		return Campaign{}, err
	}
	if draft.ID == 0 {
		row, err := db.WithQuery(ctx, func(ctx context.Context) (*ent.MarketingCampaign, error) {
			return r.client.MarketingCampaign.Create().SetName(draft.Name).SetContent(draft.Content).SetCreatedBy(draft.ActorID).Save(ctx)
		})
		if err != nil {
			return Campaign{}, err
		}
		return campaignFrom(row), nil
	}
	err := r.transition(ctx, draft.ID, transitionSpec{
		from:   []consent.CampaignStatus{consent.CampaignDraft},
		to:     consent.CampaignDraft,
		mutate: func(u *ent.MarketingCampaignUpdate) { u.SetName(draft.Name).SetContent(draft.Content).AddRevision(1) },
	})
	if err != nil {
		return Campaign{}, err
	}
	return r.CampaignGet(ctx, draft.ID)
}

type transitionSpec struct {
	pin    []predicate.MarketingCampaign
	from   []consent.CampaignStatus
	to     consent.CampaignStatus
	mutate func(*ent.MarketingCampaignUpdate)
}

func (r *Users) transition(ctx context.Context, id int, spec transitionSpec) error {
	changed, err := db.WithQuery(ctx, func(ctx context.Context) (int, error) {
		update := r.client.MarketingCampaign.Update().
			Where(marketingcampaign.IDEQ(id), marketingcampaign.StatusIn(campaignStatuses(spec.from...)...)).
			Where(spec.pin...).
			SetStatus(marketingcampaign.Status(spec.to))
		if spec.mutate != nil {
			spec.mutate(update)
		}
		return update.Save(ctx)
	})
	if err != nil || changed > 0 {
		return err
	}
	exists, err := db.WithQuery(ctx, func(ctx context.Context) (bool, error) {
		return r.client.MarketingCampaign.Query().Where(marketingcampaign.IDEQ(id)).Exist(ctx)
	})
	if err != nil {
		return err
	}
	if !exists {
		return &ent.NotFoundError{}
	}
	return ErrCampaignState
}

func (r *Users) CampaignGet(ctx context.Context, id int) (Campaign, error) {
	row, err := db.WithQuery(ctx, func(ctx context.Context) (*ent.MarketingCampaign, error) {
		return r.client.MarketingCampaign.Get(ctx, id)
	})
	if err != nil {
		return Campaign{}, err
	}
	campaign := campaignFrom(row)
	counts, err := r.sendCounts(ctx, id)
	if err != nil {
		return Campaign{}, err
	}
	campaign.Sends = counts[id]
	return campaign, nil
}

func (r *Users) CampaignList(ctx context.Context) ([]Campaign, error) {
	rows, err := db.WithQuery(ctx, func(ctx context.Context) ([]*ent.MarketingCampaign, error) {
		return r.client.MarketingCampaign.Query().Order(ent.Desc(marketingcampaign.FieldID)).Limit(campaignListLimit).All(ctx)
	})
	if err != nil {
		return nil, err
	}
	ids := make([]int, len(rows))
	for i, row := range rows {
		ids[i] = row.ID
	}
	counts, err := r.sendCounts(ctx, ids...)
	if err != nil {
		return nil, err
	}
	campaigns := make([]Campaign, len(rows))
	for i, row := range rows {
		campaigns[i] = campaignFrom(row)
		campaigns[i].Sends = counts[row.ID]
	}
	return campaigns, nil
}

func (r *Users) CampaignApprove(ctx context.Context, approval CampaignApproval) (Campaign, error) {
	current, err := r.CampaignGet(ctx, approval.ID)
	if err != nil {
		return Campaign{}, err
	}
	if !contentApprovable(current.Content) || !validCampaignActor(approval.ActorID) {
		return Campaign{}, ErrCampaignInvalid
	}
	err = r.transition(ctx, approval.ID, transitionSpec{
		pin:  []predicate.MarketingCampaign{marketingcampaign.RevisionEQ(approval.Revision)},
		from: []consent.CampaignStatus{consent.CampaignDraft},
		to:   consent.CampaignApproved,
		mutate: func(u *ent.MarketingCampaignUpdate) {
			u.SetApprovedBy(approval.ActorID).SetApprovedAt(time.Now().UTC())
		},
	})
	if err != nil {
		return Campaign{}, err
	}
	return r.CampaignGet(ctx, approval.ID)
}

var (
	pauseSpec = transitionSpec{
		from: []consent.CampaignStatus{consent.CampaignApproved, consent.CampaignSending},
		to:   consent.CampaignPaused,
	}
	resumeSpec = transitionSpec{
		from: []consent.CampaignStatus{consent.CampaignPaused},
		to:   consent.CampaignApproved,
	}
)

func (r *Users) CampaignPause(ctx context.Context, id int) (Campaign, error) {
	return r.moveCampaign(ctx, id, pauseSpec)
}

func (r *Users) CampaignResume(ctx context.Context, id int) (Campaign, error) {
	return r.moveCampaign(ctx, id, resumeSpec)
}

func (r *Users) moveCampaign(ctx context.Context, id int, spec transitionSpec) (Campaign, error) {
	if err := r.transition(ctx, id, spec); err != nil {
		return Campaign{}, err
	}
	return r.CampaignGet(ctx, id)
}

func (r *Users) AudienceLocaleCounts(ctx context.Context) (map[string]int, error) {
	counts := map[string]int{}
	var after uint64
	for {
		page, err := r.MarketingAudience(ctx, after, MaxAudiencePage)
		if err != nil {
			return nil, err
		}
		for _, recipient := range page.Recipients {
			counts[recipient.Locale]++
		}
		if page.Next == 0 {
			return counts, nil
		}
		after = page.Next
	}
}

func CampaignContentFor(content map[string]consent.CampaignContent, locale string) (consent.CampaignContent, bool) {
	for _, key := range []string{locale, consent.DefaultCampaignLocale} {
		if c, ok := content[key]; ok && c.Complete() {
			return c, true
		}
	}
	return consent.CampaignContent{}, false
}
