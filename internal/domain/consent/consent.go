// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package consent

const (
	TermsVersion           = "2026-11"
	MarketingPolicyVersion = "2026-11"
	MarketingTextKey       = "marketing.optIn"
)

type MarketingStatus string

type MarketingSkipReason string

const (
	MarketingSkipNoEmail     MarketingSkipReason = "no_email"
	MarketingSkipUnavailable MarketingSkipReason = "unavailable"
)

const (
	None      MarketingStatus = "none"
	Granted   MarketingStatus = "granted"
	Withdrawn MarketingStatus = "withdrawn"
)

type Source string

const (
	Welcome        Source = "welcome"
	Consent        Source = "consent"
	Settings       Source = "settings"
	Prompt         Source = "prompt"
	PreferencePage Source = "preference_page"
	OneClick       Source = "one_click"
	Complaint      Source = "complaint"
	Admin          Source = "admin"
	AddressChanged Source = "address_changed"
)
