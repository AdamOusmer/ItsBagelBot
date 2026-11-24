// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package consent

type CampaignStatus string

const (
	CampaignDraft    CampaignStatus = "draft"
	CampaignApproved CampaignStatus = "approved"
	CampaignSending  CampaignStatus = "sending"
	CampaignPaused   CampaignStatus = "paused"
	CampaignDone     CampaignStatus = "done"
)

type SendStatus string

const (
	SendQueued     SendStatus = "queued"
	SendSent       SendStatus = "sent"
	SendDelivered  SendStatus = "delivered"
	SendBounced    SendStatus = "bounced"
	SendComplained SendStatus = "complained"
	SendSkipped    SendStatus = "skipped"
	SendFailed     SendStatus = "failed"
)

const DefaultCampaignLocale = "en"

type CampaignContent struct {
	Subject string `json:"subject"`
	HTML    string `json:"html"`
	Text    string `json:"text"`
}

const (
	CampaignSubjectMax = 200
	CampaignBodyMax    = 200 << 10
)

func (c CampaignContent) Valid() bool {
	return len(c.Subject) <= CampaignSubjectMax && len(c.HTML) <= CampaignBodyMax && len(c.Text) <= CampaignBodyMax
}

func (c CampaignContent) Complete() bool {
	return c.Subject != "" && c.HTML != "" && c.Text != ""
}
