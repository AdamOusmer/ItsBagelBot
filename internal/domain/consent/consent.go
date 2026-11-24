// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package consent

import "slices"

const (
	TermsVersion           = "2026-11"
	MarketingPolicyVersion = "2026-11"
	MarketingTextKey       = "marketing.optIn"
)

var publishedTermsVersions = [...]string{"2026-09", "2026-10", TermsVersion}

func IsPublishedTermsVersion(version string) bool {
	return slices.Contains(publishedTermsVersions[:], version)
}

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

type SuppressionReason string

const (
	SuppressUnsubscribe SuppressionReason = "unsubscribe"
	SuppressComplaint   SuppressionReason = "complaint"
	SuppressHardBounce  SuppressionReason = "hard_bounce"
)

func (r SuppressionReason) Reported() bool {
	return r == SuppressComplaint || r == SuppressHardBounce
}

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

func (s Source) Suppresses() bool { return s != Complaint && s != AddressChanged }
