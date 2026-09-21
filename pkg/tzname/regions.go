// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package tzname

// regionZones maps a state, province or country name to the zone most of
// its people live in. It exists because of #1003: a viewer who wants to
// share their own time in chat should be able to type "!time texas" and
// stop at that granularity, rather than being pushed to name a city (D2 in
// docs/specs/time-place-lookup.md originally declined states for exactly
// the multi-zone reason below; the privacy argument won).
//
// Multi-zone regions pick the zone of the majority of the population, the
// same "one declared meaning" rule the abbreviation table uses for CST/IST/
// BST (curated.go): Texas -> Chicago (El Paso is Mountain), Florida ->
// New_York (the panhandle is Central), Idaho -> Boise (the northern
// panhandle is Pacific), Indiana -> Indianapolis (the corners near Chicago
// and Evansville are Central), Kentucky -> Louisville (the west is Central),
// Tennessee -> Chicago (Nashville and Memphis outnumber Knoxville and
// Chattanooga), Spain -> Madrid (Canaries), Portugal -> Lisbon (Azores),
// Brazil -> Sao_Paulo, Mexico -> Mexico_City, Indonesia -> Jakarta (Java is
// over half the country). Countries with no majority zone (USA, Canada,
// Australia, Russia) stay out on purpose: any pick would be wrong for most
// viewers, so those still get time.unknown and its "try a city" hint.
//
// Name collisions with the curated table, resolved by Twitch-chat
// prevalence, same as the abbreviations:
//   - "washington" stays the curated city (DC, Eastern). The state is
//     reachable as "washington state".
//   - "victoria" stays the curated city (BC, Pacific). The Australian state
//     is not listed; Melbourne resolves through the segment index.
//   - "georgia" is the US state, not the country (Asia/Tbilisi), which a
//     viewer can still reach as "tbilisi".
//
// TestRegionZonesDisjoint keeps this table minimal: a name that already
// resolves through curated.go or the IANA segment index (new york, hong
// kong, singapore, jamaica, puerto rico) must not be duplicated here.
var regionZones = map[string]tzEntry{
	// US states and DC. "new york" resolves through the segment index.
	"alabama":          {"America/Chicago", "Alabama"},
	"alaska":           {"America/Anchorage", "Alaska"},
	"arizona":          {"America/Phoenix", "Arizona"},
	"arkansas":         {"America/Chicago", "Arkansas"},
	"california":       {"America/Los_Angeles", "California"},
	"colorado":         {"America/Denver", "Colorado"},
	"connecticut":      {"America/New_York", "Connecticut"},
	"delaware":         {"America/New_York", "Delaware"},
	"florida":          {"America/New_York", "Florida"},
	"georgia":          {"America/New_York", "Georgia"},
	"hawaii":           {"Pacific/Honolulu", "Hawaii"},
	"idaho":            {"America/Boise", "Idaho"},
	"illinois":         {"America/Chicago", "Illinois"},
	"indiana":          {"America/Indiana/Indianapolis", "Indiana"},
	"iowa":             {"America/Chicago", "Iowa"},
	"kansas":           {"America/Chicago", "Kansas"},
	"kentucky":         {"America/Kentucky/Louisville", "Kentucky"},
	"louisiana":        {"America/Chicago", "Louisiana"},
	"maine":            {"America/New_York", "Maine"},
	"maryland":         {"America/New_York", "Maryland"},
	"massachusetts":    {"America/New_York", "Massachusetts"},
	"michigan":         {"America/Detroit", "Michigan"},
	"minnesota":        {"America/Chicago", "Minnesota"},
	"mississippi":      {"America/Chicago", "Mississippi"},
	"missouri":         {"America/Chicago", "Missouri"},
	"montana":          {"America/Denver", "Montana"},
	"nebraska":         {"America/Chicago", "Nebraska"},
	"nevada":           {"America/Los_Angeles", "Nevada"},
	"new hampshire":    {"America/New_York", "New Hampshire"},
	"new jersey":       {"America/New_York", "New Jersey"},
	"new mexico":       {"America/Denver", "New Mexico"},
	"north carolina":   {"America/New_York", "North Carolina"},
	"north dakota":     {"America/Chicago", "North Dakota"},
	"ohio":             {"America/New_York", "Ohio"},
	"oklahoma":         {"America/Chicago", "Oklahoma"},
	"oregon":           {"America/Los_Angeles", "Oregon"},
	"pennsylvania":     {"America/New_York", "Pennsylvania"},
	"rhode island":     {"America/New_York", "Rhode Island"},
	"south carolina":   {"America/New_York", "South Carolina"},
	"south dakota":     {"America/Chicago", "South Dakota"},
	"tennessee":        {"America/Chicago", "Tennessee"},
	"texas":            {"America/Chicago", "Texas"},
	"utah":             {"America/Denver", "Utah"},
	"vermont":          {"America/New_York", "Vermont"},
	"virginia":         {"America/New_York", "Virginia"},
	"washington state": {"America/Los_Angeles", "Washington State"},
	"west virginia":    {"America/New_York", "West Virginia"},
	"wisconsin":        {"America/Chicago", "Wisconsin"},
	"wyoming":          {"America/Denver", "Wyoming"},

	// Canadian provinces and territories. "quebec" is in curated.go.
	// Yellowknife became a link to Edmonton in tzdata 2023a, so the
	// Northwest Territories point at the canonical zone directly.
	"alberta":                   {"America/Edmonton", "Alberta"},
	"british columbia":          {"America/Vancouver", "British Columbia"},
	"bc":                        {"America/Vancouver", "British Columbia"},
	"manitoba":                  {"America/Winnipeg", "Manitoba"},
	"new brunswick":             {"America/Moncton", "New Brunswick"},
	"newfoundland":              {"America/St_Johns", "Newfoundland"},
	"newfoundland and labrador": {"America/St_Johns", "Newfoundland and Labrador"},
	"nova scotia":               {"America/Halifax", "Nova Scotia"},
	"ontario":                   {"America/Toronto", "Ontario"},
	"prince edward island":      {"America/Halifax", "Prince Edward Island"},
	"pei":                       {"America/Halifax", "Prince Edward Island"},
	"saskatchewan":              {"America/Regina", "Saskatchewan"},
	"nunavut":                   {"America/Iqaluit", "Nunavut"},
	"northwest territories":     {"America/Edmonton", "Northwest Territories"},
	"yukon":                     {"America/Whitehorse", "Yukon"},

	// Australian states and territories ("victoria" is the curated BC city).
	"new south wales":    {"Australia/Sydney", "New South Wales"},
	"nsw":                {"Australia/Sydney", "New South Wales"},
	"queensland":         {"Australia/Brisbane", "Queensland"},
	"south australia":    {"Australia/Adelaide", "South Australia"},
	"western australia":  {"Australia/Perth", "Western Australia"},
	"tasmania":           {"Australia/Hobart", "Tasmania"},
	"northern territory": {"Australia/Darwin", "Northern Territory"},
	"canberra":           {"Australia/Sydney", "Canberra"},

	// UK nations.
	"uk":               {"Europe/London", "the UK"},
	"united kingdom":   {"Europe/London", "the UK"},
	"britain":          {"Europe/London", "Britain"},
	"great britain":    {"Europe/London", "Britain"},
	"england":          {"Europe/London", "England"},
	"scotland":         {"Europe/London", "Scotland"},
	"wales":            {"Europe/London", "Wales"},
	"northern ireland": {"Europe/London", "Northern Ireland"},

	// Europe.
	"france":         {"Europe/Paris", "France"},
	"germany":        {"Europe/Berlin", "Germany"},
	"italy":          {"Europe/Rome", "Italy"},
	"spain":          {"Europe/Madrid", "Spain"},
	"netherlands":    {"Europe/Amsterdam", "the Netherlands"},
	"holland":        {"Europe/Amsterdam", "the Netherlands"},
	"belgium":        {"Europe/Brussels", "Belgium"},
	"switzerland":    {"Europe/Zurich", "Switzerland"},
	"austria":        {"Europe/Vienna", "Austria"},
	"sweden":         {"Europe/Stockholm", "Sweden"},
	"norway":         {"Europe/Oslo", "Norway"},
	"denmark":        {"Europe/Copenhagen", "Denmark"},
	"finland":        {"Europe/Helsinki", "Finland"},
	"poland":         {"Europe/Warsaw", "Poland"},
	"czechia":        {"Europe/Prague", "Czechia"},
	"czech republic": {"Europe/Prague", "Czechia"},
	"ireland":        {"Europe/Dublin", "Ireland"},
	"portugal":       {"Europe/Lisbon", "Portugal"},
	"greece":         {"Europe/Athens", "Greece"},
	"turkey":         {"Europe/Istanbul", "Turkey"},
	"ukraine":        {"Europe/Kyiv", "Ukraine"},
	"romania":        {"Europe/Bucharest", "Romania"},
	"hungary":        {"Europe/Budapest", "Hungary"},

	// Asia and the Middle East.
	"japan":                {"Asia/Tokyo", "Japan"},
	"korea":                {"Asia/Seoul", "Korea"},
	"south korea":          {"Asia/Seoul", "Korea"},
	"india":                {"Asia/Kolkata", "India"},
	"china":                {"Asia/Shanghai", "China"},
	"taiwan":               {"Asia/Taipei", "Taiwan"},
	"philippines":          {"Asia/Manila", "the Philippines"},
	"vietnam":              {"Asia/Ho_Chi_Minh", "Vietnam"},
	"thailand":             {"Asia/Bangkok", "Thailand"},
	"indonesia":            {"Asia/Jakarta", "Indonesia"},
	"malaysia":             {"Asia/Kuala_Lumpur", "Malaysia"},
	"pakistan":             {"Asia/Karachi", "Pakistan"},
	"bangladesh":           {"Asia/Dhaka", "Bangladesh"},
	"israel":               {"Asia/Jerusalem", "Israel"},
	"saudi arabia":         {"Asia/Riyadh", "Saudi Arabia"},
	"uae":                  {"Asia/Dubai", "the UAE"},
	"united arab emirates": {"Asia/Dubai", "the UAE"},

	// Africa.
	"egypt":        {"Africa/Cairo", "Egypt"},
	"south africa": {"Africa/Johannesburg", "South Africa"},
	"nigeria":      {"Africa/Lagos", "Nigeria"},
	"kenya":        {"Africa/Nairobi", "Kenya"},
	"morocco":      {"Africa/Casablanca", "Morocco"},

	// Americas and Oceania.
	"brazil":      {"America/Sao_Paulo", "Brazil"},
	"mexico":      {"America/Mexico_City", "Mexico"},
	"argentina":   {"America/Argentina/Buenos_Aires", "Argentina"},
	"chile":       {"America/Santiago", "Chile"},
	"colombia":    {"America/Bogota", "Colombia"},
	"peru":        {"America/Lima", "Peru"},
	"venezuela":   {"America/Caracas", "Venezuela"},
	"new zealand": {"Pacific/Auckland", "New Zealand"},
	"nz":          {"Pacific/Auckland", "New Zealand"},
}

// resolveRegion is resolution step 3: state, province and country names.
// It runs after the curated table so curated.go's collision picks
// (washington, victoria) win, and before the IANA name/segment steps so a
// region never shadows a real zone by accident (TestRegionZonesDisjoint).
func resolveRegion(normalized string) (Match, bool) {
	entry, ok := regionZones[normalized]
	if !ok {
		return Match{}, false
	}
	return matchForZone(entry.zone, entry.label)
}
