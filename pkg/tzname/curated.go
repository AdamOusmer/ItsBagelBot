// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package tzname

// tzEntry is one curated-table target: the zone to load and the label to
// echo back to chat.
type tzEntry struct {
	zone  string
	label string
}

// curatedZones maps a normalized query straight to a zone, bypassing the
// IANA-name/segment index entirely. It exists for two shapes the index can't
// cover on its own: timezone abbreviations (EST, JST, ...) aren't zone names
// at all, and a handful of very-online city names either have no IANA entry
// of their own (Montreal is a link folded into America/Toronto) or would
// otherwise resolve to the wrong one of several same-named places.
//
// Abbreviation ambiguity, resolved by Twitch-chat prevalence (a mispick
// costs one viewer retyping a city, which is cheaper than guessing wrong
// silently):
//   - CST: US Central beats China Standard Time and Cuba Standard Time.
//   - IST: India beats Israel and Ireland.
//   - BST: Britain beats Bangladesh.
//
// City ambiguity:
//   - "kiev" resolves to Europe/Kyiv (IANA renamed the zone in 2022; "Kiev"
//     survives only as the Europe/Kiev backward-compat link, which is
//     excluded from the index's link set on purpose - see legacyLinks in
//     internal/zonelist).
//   - "istanbul" resolves to Europe/Istanbul. TestSegmentCollisions caught
//     this: Asia/Istanbul and Europe/Istanbul both exist as their own zip
//     entries and both share the last segment "Istanbul", which is exactly
//     the ambiguity step 4 (last-segment lookup) can't resolve on its own.
//     Neither is a link in IANA's own data, so there's no rule-based way to
//     prefer one; Europe/Istanbul is what Turkey's own tzdata assignment
//     uses, so it's the one a viewer means.
var curatedZones = map[string]tzEntry{
	// Abbreviations.
	"est": {"America/New_York", "Eastern Time"},
	"edt": {"America/New_York", "Eastern Time"},
	"et":  {"America/New_York", "Eastern Time"},
	"cst": {"America/Chicago", "Central Time"},
	"cdt": {"America/Chicago", "Central Time"},
	"ct":  {"America/Chicago", "Central Time"},
	"mst": {"America/Denver", "Mountain Time"},
	"mdt": {"America/Denver", "Mountain Time"},
	"mt":  {"America/Denver", "Mountain Time"},
	"pst": {"America/Los_Angeles", "Pacific Time"},
	"pdt": {"America/Los_Angeles", "Pacific Time"},
	"pt":  {"America/Los_Angeles", "Pacific Time"},
	"ast": {"America/Halifax", "Atlantic Time"},
	"adt": {"America/Halifax", "Atlantic Time"},
	"nst": {"America/St_Johns", "Newfoundland Time"},
	"ndt": {"America/St_Johns", "Newfoundland Time"},
	"utc": {"UTC", "UTC"},
	"gmt": {"UTC", "UTC"},
	"z":   {"UTC", "UTC"},

	"zulu": {"UTC", "UTC"},
	"bst":  {"Europe/London", "British Time"},
	"cet":  {"Europe/Berlin", "Central European Time"},
	"cest": {"Europe/Berlin", "Central European Time"},
	"eet":  {"Europe/Athens", "Eastern European Time"},
	"eest": {"Europe/Athens", "Eastern European Time"},
	"ist":  {"Asia/Kolkata", "India Time"},
	"jst":  {"Asia/Tokyo", "Japan Time"},
	"kst":  {"Asia/Seoul", "Korea Time"},
	"hkt":  {"Asia/Hong_Kong", "Hong Kong Time"},
	"aest": {"Australia/Sydney", "Eastern Australia Time"},
	"aedt": {"Australia/Sydney", "Eastern Australia Time"},
	"awst": {"Australia/Perth", "Western Australia Time"},
	"nzst": {"Pacific/Auckland", "New Zealand Time"},
	"nzdt": {"Pacific/Auckland", "New Zealand Time"},
	"msk":  {"Europe/Moscow", "Moscow Time"},
	"brt":  {"America/Sao_Paulo", "Brasilia Time"},

	// Canada (Montreal/Ottawa/Quebec have no IANA entry of their own; the
	// tzdb folds all of mainland Eastern Canada into America/Toronto).
	"montreal":    {"America/Toronto", "Montreal"},
	"ottawa":      {"America/Toronto", "Ottawa"},
	"quebec":      {"America/Toronto", "Quebec"},
	"quebec city": {"America/Toronto", "Quebec City"},
	"calgary":     {"America/Edmonton", "Calgary"},
	"victoria":    {"America/Vancouver", "Victoria"},

	// US Eastern.
	"nyc":           {"America/New_York", "New York"},
	"new york city": {"America/New_York", "New York"},
	"boston":        {"America/New_York", "Boston"},
	"miami":         {"America/New_York", "Miami"},
	"atlanta":       {"America/New_York", "Atlanta"},
	"philadelphia":  {"America/New_York", "Philadelphia"},
	"washington":    {"America/New_York", "Washington"},
	"dc":            {"America/New_York", "DC"},
	"washington dc": {"America/New_York", "Washington DC"},

	// US Central.
	"dallas":      {"America/Chicago", "Dallas"},
	"houston":     {"America/Chicago", "Houston"},
	"austin":      {"America/Chicago", "Austin"},
	"minneapolis": {"America/Chicago", "Minneapolis"},

	// US Mountain.
	"salt lake":      {"America/Denver", "Salt Lake City"},
	"salt lake city": {"America/Denver", "Salt Lake City"},

	// US Pacific.
	"seattle":       {"America/Los_Angeles", "Seattle"},
	"san francisco": {"America/Los_Angeles", "San Francisco"},
	"sf":            {"America/Los_Angeles", "San Francisco"},
	"san diego":     {"America/Los_Angeles", "San Diego"},
	"las vegas":     {"America/Los_Angeles", "Las Vegas"},
	"vegas":         {"America/Los_Angeles", "Las Vegas"},
	"portland":      {"America/Los_Angeles", "Portland"},

	// Asia.
	"beijing":      {"Asia/Shanghai", "Beijing"},
	"shenzhen":     {"Asia/Shanghai", "Shenzhen"},
	"mumbai":       {"Asia/Kolkata", "Mumbai"},
	"delhi":        {"Asia/Kolkata", "Delhi"},
	"new delhi":    {"Asia/Kolkata", "New Delhi"},
	"bangalore":    {"Asia/Kolkata", "Bangalore"},
	"bengaluru":    {"Asia/Kolkata", "Bengaluru"},
	"osaka":        {"Asia/Tokyo", "Osaka"},
	"tel aviv":     {"Asia/Jerusalem", "Tel Aviv"},
	"hanoi":        {"Asia/Ho_Chi_Minh", "Hanoi"},
	"kuala lumpur": {"Asia/Kuala_Lumpur", "Kuala Lumpur"},
	"istanbul":     {"Europe/Istanbul", "Istanbul"}, // segment collision, see doc comment above

	// Europe.
	"milan":      {"Europe/Rome", "Milan"},
	"munich":     {"Europe/Berlin", "Munich"},
	"frankfurt":  {"Europe/Berlin", "Frankfurt"},
	"barcelona":  {"Europe/Madrid", "Barcelona"},
	"manchester": {"Europe/London", "Manchester"},
	"birmingham": {"Europe/London", "Birmingham"},
	"glasgow":    {"Europe/London", "Glasgow"},
	"marseille":  {"Europe/Paris", "Marseille"},
	"lyon":       {"Europe/Paris", "Lyon"},
	"kiev":       {"Europe/Kyiv", "Kyiv"},

	// Latin America / Africa.
	"rio":            {"America/Sao_Paulo", "Rio de Janeiro"},
	"rio de janeiro": {"America/Sao_Paulo", "Rio de Janeiro"},
	"guadalajara":    {"America/Mexico_City", "Guadalajara"},
	"monterrey":      {"America/Mexico_City", "Monterrey"},
	"cape town":      {"Africa/Johannesburg", "Cape Town"},
}

// resolveCurated is resolution step 2: the hand-picked abbreviation/city
// table above.
func resolveCurated(normalized string) (Match, bool) {
	entry, ok := curatedZones[normalized]
	if !ok {
		return Match{}, false
	}
	return matchForZone(entry.zone, entry.label)
}
