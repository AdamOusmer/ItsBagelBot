// Package gatewayrpc holds the shared wire types for the gateway service RPC
// surface: the one request shape every provider endpoint accepts, and the typed
// reply each endpoint answers with.
//
// The gateway proxies and caches external API systems (urchin.gg, MCSR Ranked,
// ...) behind NATS request/reply so no chat-path service ever dials the
// internet itself. Subjects are "<prefix>.<provider>.<endpoint>" (default
// prefix "bagel.rpc.gateway"), e.g. "bagel.rpc.gateway.urchin.daily".
//
// Every reply embeds the fleet's conventional {"error": ""} envelope, so
// bus.RequestJSON callers get a Go error (bus.RPCReplyError) instead of a
// zero-valued success when the provider answered with a failure such as
// "player not found".
package gatewayrpc

// Request covers every gateway endpoint; unused fields are zero.
type Request struct {
	// Account is the provider-side account the lookup targets (a Minecraft
	// username or UUID for urchin/mcsr).
	Account string `json:"account"`
	// ChannelID scopes session-stateful endpoints (mcsr session snapshots) to
	// one broadcaster, so two channels tracking the same player never share a
	// stream session. The govee provider also reads it as the broadcaster whose
	// stored (encrypted) Govee API key the call authenticates with.
	ChannelID string `json:"channel_id,omitempty"`
	// IsPremium indicates whether the caller is on the premium lane, enabling
	// the provider to consume from the reserved premium rate limit bucket.
	IsPremium bool `json:"is_premium,omitempty"`

	// --- govee (per-broadcaster smart-light control) ------------------------
	// The govee provider authenticates with the broadcaster's own stored key
	// (resolved from ChannelID), so these carry only which device to act on and
	// the colour to set. Zero on every non-govee request.

	// Device is the Govee device id (its MAC-style address) govee.control acts
	// on; empty on govee.devices (which lists them).
	Device string `json:"device,omitempty"`
	// SKU is the device model (e.g. "H6159") the Govee control payload pairs
	// with Device.
	SKU string `json:"sku,omitempty"`
	// ColorRGB is the packed 24-bit colour (r<<16|g<<8|b) govee.control sets.
	// Range 0..0xFFFFFF; the caller rejects an unparseable colour before the
	// call, so 0 (sent as omitted) only reaches control as a deliberate black.
	ColorRGB int `json:"color_rgb,omitempty"`
}

// Subject builds the NATS subject for one provider endpoint under prefix.
func Subject(prefix, provider, endpoint string) string {
	return prefix + "." + provider + "." + endpoint
}

// --- urchin (Coral: Hypixel Bed Wars stats + Urchin blacklist) --------------

// UrchinSessionReply is the answer to urchin.daily / urchin.weekly /
// urchin.monthly: the change in a player's Bed Wars stats since the period's
// reset.
type UrchinSessionReply struct {
	Player      string `json:"player"`
	SinceUnix   int64  `json:"since_unix"`
	Wins        int64  `json:"wins"`
	Losses      int64  `json:"losses"`
	FinalKills  int64  `json:"final_kills"`
	FinalDeaths int64  `json:"final_deaths"`
	BedsBroken  int64  `json:"beds_broken"`
	GamesPlayed int64  `json:"games_played"`
	Levels      int64  `json:"levels"`
	Error       string `json:"error,omitempty"`
}

// UrchinStatsReply is the answer to urchin.stats: the player's lifetime Bed
// Wars stats extracted from their Hypixel profile.
type UrchinStatsReply struct {
	Player      string `json:"player"`
	Stars       int64  `json:"stars"`
	Wins        int64  `json:"wins"`
	Losses      int64  `json:"losses"`
	FinalKills  int64  `json:"final_kills"`
	FinalDeaths int64  `json:"final_deaths"`
	BedsBroken  int64  `json:"beds_broken"`
	Error       string `json:"error,omitempty"`
}

// --- hypixel (direct Hypixel API) --------------------------------------------

// HypixelStatsReply is the answer to hypixel.stats: the player's lifetime Bed
// Wars stats read straight from the Hypixel API. It is the wire the urchin
// dashboard module's !bwstats rides — same shape as UrchinStatsReply, owned by
// the hypixel provider (Coral's profile endpoint needs a key permission ours
// does not carry, so lifetime stats bypass Coral entirely).
type HypixelStatsReply struct {
	Player      string `json:"player"`
	Stars       int64  `json:"stars"`
	Wins        int64  `json:"wins"`
	Losses      int64  `json:"losses"`
	FinalKills  int64  `json:"final_kills"`
	FinalDeaths int64  `json:"final_deaths"`
	BedsBroken  int64  `json:"beds_broken"`
	Error       string `json:"error,omitempty"`
}

// UrchinSniperReply is the answer to urchin.sniper: the player's Urchin
// (Cubelify overlay) sniper score.
type UrchinSniperReply struct {
	Player string `json:"player"`
	// Score is the overlay score value; Mode describes how the overlay should
	// interpret it (as returned by the API).
	Score    float64 `json:"score"`
	Mode     string  `json:"mode"`
	TagCount int     `json:"tag_count"`
	Error    string  `json:"error,omitempty"`
}

// UrchinTag is one active blacklist tag on a player.
type UrchinTag struct {
	Type    string `json:"type"`
	Reason  string `json:"reason,omitempty"`
	AddedOn int64  `json:"added_on,omitempty"`
}

// UrchinTagsReply is the answer to urchin.tags: the blacklist tags currently
// active on a player.
type UrchinTagsReply struct {
	Player string      `json:"player"`
	Tags   []UrchinTag `json:"tags"`
	Error  string      `json:"error,omitempty"`
}

// --- mcsr (MCSR Ranked) ------------------------------------------------------

// McsrUserReply is the answer to mcsr.user: the player's current MCSR Ranked
// standing. Elo and Rank are -1 when the player is unrated this season.
type McsrUserReply struct {
	Nickname string `json:"nickname"`
	UUID     string `json:"uuid"`
	Elo      int    `json:"elo"`
	Rank     int    `json:"rank"`
	Country  string `json:"country,omitempty"`
	// Season counters (ranked queue).
	Wins   int `json:"wins"`
	Loses  int `json:"loses"`
	Played int `json:"played"`
	// BestTimeMS is the season's best ranked completion in milliseconds; 0 when
	// none.
	BestTimeMS int64  `json:"best_time_ms"`
	Error      string `json:"error,omitempty"`
}

// McsrSnapshotReply is the answer to mcsr.session_start: acknowledges the
// stream-start snapshot the session delta is later computed against.
type McsrSnapshotReply struct {
	Nickname string `json:"nickname"`
	Elo      int    `json:"elo"`
	Error    string `json:"error,omitempty"`
}

// --- govee (smart-light control over the broadcaster's own key) -------------

// GoveeDevice is one controllable device on a broadcaster's Govee account, as
// govee.devices lists it for the dashboard's device picker.
type GoveeDevice struct {
	// Device is the device id (MAC-style address) control calls target.
	Device string `json:"device"`
	// SKU is the model code paired with Device in a control payload.
	SKU string `json:"sku"`
	// Name is the user-facing device name set in the Govee app.
	Name string `json:"name"`
	// Color reports whether the device advertises the RGB colour capability, so
	// the picker can hide lights the color reward could never drive.
	Color bool `json:"color"`
}

// GoveeDevicesReply is the answer to govee.devices: the broadcaster's
// controllable devices. A missing/invalid key surfaces as Error so the
// dashboard can prompt the broadcaster to re-enter it.
type GoveeDevicesReply struct {
	Devices []GoveeDevice `json:"devices"`
	Error   string        `json:"error,omitempty"`
}

// GoveeControlReply is the answer to govee.control: it acknowledges that the
// device was powered on and set to the requested colour, or reports why not.
type GoveeControlReply struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// McsrSessionReply is the answer to mcsr.session: the change in a player's
// ranked standing since the stream-start snapshot for this channel.
//
// HasSnapshot is false when no snapshot existed for the channel (module was
// enabled mid-stream, or the gateway lost it); the gateway then takes one, so
// the next call has a baseline.
type McsrSessionReply struct {
	Nickname    string `json:"nickname"`
	Elo         int    `json:"elo"`
	EloChange   int    `json:"elo_change"`
	Wins        int    `json:"wins"`
	Loses       int    `json:"loses"`
	Played      int    `json:"played"`
	SinceUnix   int64  `json:"since_unix"`
	HasSnapshot bool   `json:"has_snapshot"`
	Error       string `json:"error,omitempty"`
}
