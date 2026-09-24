// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package tzname

type tzEntry struct {
	zone  string
	label string
}

var curatedZones = map[string]tzEntry{
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

	"montreal":    {"America/Toronto", "Montreal"},
	"ottawa":      {"America/Toronto", "Ottawa"},
	"quebec":      {"America/Toronto", "Quebec"},
	"quebec city": {"America/Toronto", "Quebec City"},
	"calgary":     {"America/Edmonton", "Calgary"},
	"victoria":    {"America/Vancouver", "Victoria"},

	"nyc":           {"America/New_York", "New York"},
	"new york city": {"America/New_York", "New York"},
	"boston":        {"America/New_York", "Boston"},
	"miami":         {"America/New_York", "Miami"},
	"atlanta":       {"America/New_York", "Atlanta"},
	"philadelphia":  {"America/New_York", "Philadelphia"},
	"washington":    {"America/New_York", "Washington"},
	"dc":            {"America/New_York", "DC"},
	"washington dc": {"America/New_York", "Washington DC"},

	"dallas":      {"America/Chicago", "Dallas"},
	"houston":     {"America/Chicago", "Houston"},
	"austin":      {"America/Chicago", "Austin"},
	"minneapolis": {"America/Chicago", "Minneapolis"},

	"salt lake":      {"America/Denver", "Salt Lake City"},
	"salt lake city": {"America/Denver", "Salt Lake City"},

	"seattle":       {"America/Los_Angeles", "Seattle"},
	"san francisco": {"America/Los_Angeles", "San Francisco"},
	"sf":            {"America/Los_Angeles", "San Francisco"},
	"san diego":     {"America/Los_Angeles", "San Diego"},
	"las vegas":     {"America/Los_Angeles", "Las Vegas"},
	"vegas":         {"America/Los_Angeles", "Las Vegas"},
	"portland":      {"America/Los_Angeles", "Portland"},

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
	"istanbul":     {"Europe/Istanbul", "Istanbul"},

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

	"rio":            {"America/Sao_Paulo", "Rio de Janeiro"},
	"rio de janeiro": {"America/Sao_Paulo", "Rio de Janeiro"},
	"guadalajara":    {"America/Mexico_City", "Guadalajara"},
	"monterrey":      {"America/Mexico_City", "Monterrey"},
	"cape town":      {"Africa/Johannesburg", "Cape Town"},
}

func resolveCurated(normalized string) (Match, bool) {
	entry, ok := curatedZones[normalized]
	if !ok {
		return Match{}, false
	}
	return matchForZone(entry.zone, entry.label)
}
