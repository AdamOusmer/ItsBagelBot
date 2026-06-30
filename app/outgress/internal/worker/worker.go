// Package worker drains one outgress lane: it enforces the channel registry,
// the Twitch rate limits, and the premium reservation, then executes the
// Helix request. Handlers nack on anything retryable and rely on the lane
// subscriber's paced redelivery, so a rate-limited or failing message waits
// out its budget instead of spinning.
package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"time"

	"ItsBagelBot/app/outgress/internal/channels"
	"ItsBagelBot/app/outgress/internal/conduit"
	"ItsBagelBot/app/outgress/internal/ratelimit"
	"ItsBagelBot/app/outgress/internal/twitch"
	eventtwitch "ItsBagelBot/internal/domain/event/twitch"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/internal/domain/rpc/manage"
	"ItsBagelBot/pkg/cache"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/bytedance/sonic"
	"github.com/newrelic/go-agent/v3/newrelic"

	"go.uber.org/zap"
)

// Twitch enforces the chat limits per channel (20 messages per 30s, 100 when
// the bot moderates the channel) and one Helix budget per app token (800 per
// minute, shared by every endpoint the app calls).
//
// That 800/min budget is partitioned so the lanes cannot starve each other:
//
//	helixSystemReserve   tokens/min are reserved for the system lane (the
//	                     dashboard's EventSub create/delete jobs), drawn from
//	                     ratelimit:helix:system and nothing else. Reserving
//	                     them means an onboarding burst always has capacity,
//	                     and capping the lane to them means a flood of toggles
//	                     can never drain the budget chat/api traffic needs.
//	helixGeneralCapacity tokens/min (the remainder) back ordinary api traffic
//	                     on ratelimit:helix:app. The standard lane is held to
//	                     half of that by its own bucket, so premium api always
//	                     finds at least half of the general budget free.
//
// The two partitions are disjoint and sum to the real limit, so the fleet
// never exceeds 800/min no matter how the lanes mix.
const (
	chatCapacity    = 20.0
	chatModCapacity = 100.0
	chatWindow      = 30.0

	helixCapacity        = 800.0
	helixWindow          = 60.0
	helixSystemReserve   = 100.0
	helixGeneralCapacity = helixCapacity - helixSystemReserve
)

// Stable bucket parameters are formatted once at process initialization. Chat
// keys remain per-broadcaster, but their numeric Lua arguments do not.
var (
	chatSpec            = ratelimit.NewSpec(chatCapacity, chatCapacity/chatWindow)
	chatStandardSpec    = ratelimit.NewSpec(chatCapacity/2, chatCapacity/chatWindow/2)
	chatModSpec         = ratelimit.NewSpec(chatModCapacity, chatModCapacity/chatWindow)
	chatModStandardSpec = ratelimit.NewSpec(chatModCapacity/2, chatModCapacity/chatWindow/2)
	helixGeneralSpec    = ratelimit.NewSpec(helixGeneralCapacity, helixGeneralCapacity/helixWindow)
	helixStandardSpec   = ratelimit.NewSpec(helixGeneralCapacity/2, helixGeneralCapacity/helixWindow/2)
	helixSystemSpec     = ratelimit.NewSpec(helixSystemReserve, helixSystemReserve/helixWindow)
)

// nodeRegion and nodeName label every transaction so Twitch external-segment
// duration can be faceted by node in New Relic. They are process-wide (one pod
// is one node) and set once at startup via SetNodeIdentity; the empty default
// is harmless when the agent is not configured.
var (
	nodeRegion string
	nodeName   string
)

// SetNodeIdentity records the pod's region and host for transaction labeling.
// Call once at startup before consuming.
func SetNodeIdentity(region, host string) {
	nodeRegion = region
	nodeName = host
}

// Lane identifies which queue a worker drains; it selects the rate-limit
// buckets the worker pays into.
type Lane int

const (
	LanePremium Lane = iota
	LaneStandard
	LaneSystem
)

type expectedNackError string

func (e expectedNackError) Error() string      { return string(e) }
func (e expectedNackError) ExpectedNack() bool { return true }

// Expected backpressure must nack without becoming one warning and one noticed
// error per attempt. pkg/bus recognizes ExpectedNack structurally.
const (
	ErrPaused          expectedNackError = "outgress is paused"
	errRateLimitFirst  expectedNackError = "rate limit exceeded on reserved bucket"
	errRateLimitShared expectedNackError = "rate limit exceeded on shared bucket"
)

// helixRoute is the Helix call a message type maps to when the producer leaves
// endpoint/method empty. as is the default token identity for the type ("" =
// route by endpoint), applied only when the message does not set its own.
type helixRoute struct {
	method   string
	endpoint string
	as       string
}

// typeRoutes lets outgress own the Helix endpoint per type, so producers send
// intent ("chat", "ban", "ad", "clip") plus the body instead of hardcoding
// paths. Types absent here (e.g. "api") are generic passthroughs and must carry
// their own endpoint.
//
//   - chat:        bot send (app token honors user:bot + channel:bot).
//   - ban/unban:   bot acts as moderator (/helix/moderation/* → bot user token).
//   - timeout:     same endpoint as ban; the body's duration makes it a timeout.
//   - ad/commercial: the broadcaster starts the ad (channel:edit:commercial).
//   - clip:        the broadcaster's grant creates the clip (clips:edit).
var typeRoutes = map[string]helixRoute{
	outgress.TypeChat:       {http.MethodPost, "/helix/chat/messages", outgress.AsApp},
	outgress.TypeBan:        {http.MethodPost, "/helix/moderation/bans", outgress.AsBot},
	outgress.TypeTimeout:    {http.MethodPost, "/helix/moderation/bans", outgress.AsBot},
	outgress.TypeUnban:      {http.MethodDelete, "/helix/moderation/bans", outgress.AsBot},
	outgress.TypeAd:         {http.MethodPost, "/helix/channels/commercial", outgress.AsBroadcaster},
	outgress.TypeCommercial: {http.MethodPost, "/helix/channels/commercial", outgress.AsBroadcaster},
	outgress.TypeClip:       {http.MethodPost, "/helix/clips", outgress.AsBroadcaster},
	// Cloud-bot chat actions use the app token. Twitch only awards the Chat Bot
	// badge to Send Chat Message calls made with an app access token, backed by
	// the bot's user:bot/action grants and the broadcaster's channel:bot grant.
	outgress.TypeAnnounce: {http.MethodPost, "/helix/chat/announcements", outgress.AsApp},
	outgress.TypeShoutout: {http.MethodPost, "/helix/chat/shoutouts", outgress.AsApp},
}

type Worker struct {
	log      *zap.Logger
	limiter  ratelimit.Manager
	registry *channels.Registry
	twitch   *twitch.Client
	botID    string
	owner    string // pod identity for the enroll lock (os.Hostname)
	conduit  *conduit.Resolver
	lane     Lane
	// userIDs caches login->id resolutions (shoutout targets) so a repeated
	// /shoutout to the same channel does not re-hit Helix Get Users each time.
	userIDs *cache.Cache[string]
	// modVerifier resolves stale moderator state asynchronously so chat sends
	// never wait for a paginated Twitch lookup or OAuth refresh.
	modVerifier *ModVerifier
	// live writes the result of a Twitch live re-check back into the projection.
	// Only the system lane sets it (via SetLiveWriter); nil elsewhere.
	live *LiveWriter
}

// SetLiveWriter attaches the live re-check write-back, used by the system lane
// worker that handles stream_status jobs.
func (w *Worker) SetLiveWriter(lw *LiveWriter) { w.live = lw }

func (w *Worker) SetModVerifier(v *ModVerifier) { w.modVerifier = v }

func New(log *zap.Logger, limiter ratelimit.Manager, registry *channels.Registry, tw *twitch.Client, botID, owner string, conduitResolver *conduit.Resolver, lane Lane) *Worker {
	return &Worker{
		log:      log,
		limiter:  limiter,
		registry: registry,
		twitch:   tw,
		botID:    botID,
		owner:    owner,
		conduit:  conduitResolver,
		lane:     lane,
		userIDs:  cache.New[string](cache.DefaultCapacity, 10*time.Minute),
	}
}

// wireMessage keeps the nested payload as a zero-copy view into Watermill's
// message buffer. The buffer remains owned for the whole synchronous handler.
type wireMessage struct {
	Type          string                 `json:"type"`
	BroadcasterID string                 `json:"broadcaster_id"`
	SenderID      string                 `json:"sender_id"`
	Endpoint      string                 `json:"endpoint"`
	Method        string                 `json:"method"`
	Payload       sonic.NoCopyRawMessage `json:"payload"`
	As            string                 `json:"as,omitempty"`
	Color         string                 `json:"color,omitempty"`
	To            string                 `json:"to,omitempty"`
}

// PrepareJSON compiles Sonic's decoders during startup rather than on the first
// latency-sensitive message.
func PrepareJSON() error {
	return sonic.PretouchMany([]reflect.Type{
		reflect.TypeOf(wireMessage{}),
		reflect.TypeOf(outgress.EventSubJob{}),
		reflect.TypeOf(outgress.StreamStatusJob{}),
	})
}

func decodeMessage(data []byte, destination *outgress.Message) error {
	var wire wireMessage
	if err := sonic.ConfigFastest.Unmarshal(data, &wire); err != nil {
		return err
	}
	*destination = outgress.Message{
		Type: wire.Type, BroadcasterID: wire.BroadcasterID, SenderID: wire.SenderID,
		Endpoint: wire.Endpoint, Method: wire.Method, Payload: json.RawMessage(wire.Payload),
		As: wire.As, Color: wire.Color, To: wire.To,
	}
	return nil
}

func recordStageDuration(ctx context.Context, attribute string, started time.Time) {
	if txn := newrelic.FromContext(ctx); txn != nil {
		txn.AddAttribute(attribute, float64(time.Since(started).Microseconds())/1000)
	}
}

func (w *Worker) Process(msg *message.Message) error {
	ctx := msg.Context()
	processStarted := time.Now()
	defer recordStageDuration(ctx, "outgress.total_ms", processStarted)

	var payload outgress.Message
	decodeStarted := time.Now()
	if err := decodeMessage(msg.Payload, &payload); err != nil {
		recordStageDuration(ctx, "outgress.decode_ms", decodeStarted)
		w.log.Error("dropping malformed outgress message", zap.Error(err))
		if txn := newrelic.FromContext(ctx); txn != nil {
			txn.NoticeError(err)
		}
		return nil
	}
	recordStageDuration(ctx, "outgress.decode_ms", decodeStarted)

	if txn := newrelic.FromContext(ctx); txn != nil {
		txn.AddAttribute("node.region", nodeRegion)
		txn.AddAttribute("node.name", nodeName)
		txn.AddAttribute("event.type", payload.Type)
		txn.AddAttribute("event.broadcaster_id", payload.BroadcasterID)
		if payload.Endpoint != "" {
			txn.AddAttribute("event.endpoint", payload.Endpoint)
		}
	}

	pauseStarted := time.Now()
	paused, err := w.registry.Paused(ctx)
	recordStageDuration(ctx, "outgress.pause_ms", pauseStarted)
	if err != nil {
		return err
	}
	if paused {
		return ErrPaused
	}

	if payload.Type == outgress.TypeEventSub {
		return w.processEventSub(ctx, payload)
	}

	if payload.Type == outgress.TypeStreamStatus {
		return w.processStreamStatus(ctx, payload)
	}

	// Helix path: "chat", "api", and the mapped intents (ban, unban, ad, clip…).
	// Fill endpoint/method/as from the type when the producer left them empty, so
	// a job only needs its intent + body. "api" has no mapping (generic
	// passthrough) and must carry its own endpoint. An explicit field always
	// wins, so any default can be overridden.
	r, ok := typeRoutes[payload.Type]
	if payload.Type != outgress.TypeChat && payload.Type != outgress.TypeAPI && !ok {
		w.log.Error("dropping message with unknown type", zap.String("type", payload.Type))
		return nil
	}
	if ok {
		if payload.Endpoint == "" {
			payload.Endpoint = r.endpoint
		}
		if payload.Method == "" {
			payload.Method = r.method
		}
		if payload.As == "" {
			payload.As = r.as
		}
	}
	if !strings.HasPrefix(payload.Endpoint, "/helix/") || payload.Method == "" {
		w.log.Error("dropping message with invalid request",
			zap.String("type", payload.Type),
			zap.String("endpoint", payload.Endpoint),
			zap.String("method", payload.Method))
		return nil
	}

	// Announce needs moderator_id + broadcaster_id in the query string (not the
	// body) and a default color merged in, so it gets its own handler before the
	// generic helix path.
	if payload.Type == outgress.TypeAnnounce {
		return w.processAnnounce(ctx, payload)
	}

	// Shoutout resolves the target login to an id, then puts from/to/moderator
	// ids in the query string (no body), so it gets its own handler too.
	if payload.Type == outgress.TypeShoutout {
		return w.processShoutout(ctx, payload)
	}

	// Only "chat" pays the chat rate buckets; every other Helix call pays the
	// general bucket.
	if payload.Type == outgress.TypeChat {
		return w.processChat(ctx, payload)
	}
	return w.processAPI(ctx, payload)
}

func (w *Worker) processChat(ctx context.Context, payload outgress.Message) error {

	// The enabled/disabled decision belongs to the worker, not outgress: by the
	// time a chat send reaches here it is already authorized. Outgress only reads
	// the registry for the bot's mod status (which sets the chat rate capacity).
	registryStarted := time.Now()
	ch, found, err := w.registry.Get(ctx, payload.BroadcasterID)
	recordStageDuration(ctx, "outgress.registry_ms", registryStarted)
	if err != nil {
		return err
	}

	sharedSpec := chatSpec
	standardSpec := chatStandardSpec
	if w.modStatus(ctx, payload, ch, found) {
		sharedSpec = chatModSpec
		standardSpec = chatModStandardSpec
	}

	// The standard lane is constrained by BOTH a restricted standard bucket and the shared bucket.
	// We use takeOrdered to atomically check and consume both. A denial on either bucket
	// leaves both buckets untouched, avoiding token waste during retry storms.
	shared := sharedSpec.ForDynamicKey("ratelimit:chat:", "chat", payload.BroadcasterID)
	if w.lane == LaneStandard {
		standard := standardSpec.ForDynamicKey("ratelimit:chat:standard:", "chat:standard", payload.BroadcasterID)
		if err := w.takeOrdered(ctx, standard, shared); err != nil {
			return err
		}
	} else if err := w.take(ctx, shared); err != nil {
		return err
	}

	// Helix Send Chat Message requires sender_id (the bot) in the body. Producers
	// only carry the target broadcaster_id + message; the bot identity is owned
	// here, so inject it. An explicit message sender_id wins; otherwise the
	// configured bot id, falling back to the message's SenderID.
	sender := payload.SenderID
	if sender == "" {
		sender = w.botID
	}
	if sender == "" {
		w.log.Error("dropping chat message: no bot sender id configured",
			zap.String("broadcaster_id", payload.BroadcasterID))
		return nil
	}
	payload.Payload = withSenderID(payload.Payload, sender)

	return w.execute(ctx, payload)
}

// withSenderID ensures the chat body carries sender_id without disturbing the
// other fields the producer set. A sender_id already present is left untouched.
func withSenderID(body []byte, senderID string) []byte {
	return withField(body, "sender_id", senderID)
}

// withField inserts "field":"value" into a JSON object body without decoding it.
// If the field already appears, body is returned unchanged. Used to inject the
// bot identity (sender_id / moderator_id) Twitch requires but producers omit, or
// a default announce color, without paying a full marshal/unmarshal round-trip.
//
// Twitch ids/logins and the fixed color/identity values are alphanumeric plus
// underscore, so value needs no JSON escaping; callers pass only such safe
// strings.
func withField(body []byte, field, value string) []byte {

	if bytes.Contains(body, []byte("\""+field+"\"")) {
		return body // already present; leave the producer's value alone
	}

	insert := "\"" + field + "\":\"" + value + "\""

	end := bytes.LastIndexByte(body, '}')
	if end < 0 {
		// No closing '}' to splice into. Only synthesize a fresh object when the
		// body is empty or all whitespace; a non-empty, non-object body (e.g. a
		// top-level JSON array) is not ours to rewrite, so return it unchanged
		// rather than discarding it.
		if len(bytes.TrimSpace(body)) == 0 {
			return []byte("{" + insert + "}")
		}
		return body
	}

	// Find the previous non-space byte before the closing '}': if it is the
	// opening '{', the object is empty and the field goes in bare; otherwise it
	// follows the last field, so prefix a comma.
	i := end - 1
	for i >= 0 {
		switch body[i] {
		case ' ', '\t', '\n', '\r':
			i--
			continue
		}
		break
	}
	if i >= 0 && body[i] != '{' {
		insert = "," + insert
	}

	out := make([]byte, 0, len(body)+len(insert))
	out = append(out, body[:end]...)
	out = append(out, insert...)
	out = append(out, body[end:]...)
	return out
}

func (w *Worker) processAPI(ctx context.Context, payload outgress.Message) error {
	if err := w.takeGeneralHelix(ctx); err != nil {
		return err
	}

	return w.execute(ctx, payload)
}

// processAnnounce sends a Helix chat announcement as the bot. The endpoint
// carries broadcaster_id + moderator_id as query params (Twitch reads them from
// the query, not the body), while the body carries the message plus a color
// (defaulting to "primary"). It pays the general Helix budget like processAPI,
// then hands the assembled request to execute() for the shared status handling.
func (w *Worker) processAnnounce(ctx context.Context, payload outgress.Message) error {

	// The announcing moderator is the bot: prefer an explicit sender, else the
	// configured bot id. Without one there is nobody to announce as, so drop the
	// job (mirroring processChat's no-sender guard).
	mod := payload.SenderID
	if mod == "" {
		mod = w.botID
	}
	if mod == "" {
		w.log.Error("dropping announce: no bot moderator id configured",
			zap.String("broadcaster_id", payload.BroadcasterID))
		return nil
	}

	if err := w.takeGeneralHelix(ctx); err != nil {
		return err
	}

	color := payload.Color
	if color == "" {
		color = "primary"
	}

	payload.As = outgress.AsApp
	payload.Method = http.MethodPost
	payload.Endpoint = "/helix/chat/announcements?broadcaster_id=" +
		url.QueryEscape(payload.BroadcasterID) + "&moderator_id=" + url.QueryEscape(mod)
	payload.Payload = withField(payload.Payload, "color", color)

	return w.execute(ctx, payload)
}

// shoutoutEndpoint assembles the Helix Send a Shoutout path. All three ids ride
// the query string (Twitch reads them from the query, not a body) and are
// URL-escaped. Factored out so the construction can be pinned without a network
// round-trip.
func shoutoutEndpoint(fromBroadcasterID, toID, moderatorID string) string {
	return "/helix/chat/shoutouts?from_broadcaster_id=" + url.QueryEscape(fromBroadcasterID) +
		"&to_broadcaster_id=" + url.QueryEscape(toID) +
		"&moderator_id=" + url.QueryEscape(moderatorID)
}

// processShoutout sends a Helix Send a Shoutout as the bot. The producer carries
// the source channel (BroadcasterID) plus the target login (To); outgress
// resolves the login to a numeric id (cached, single-flight) and owns the
// moderator identity. from/to/moderator ids ride the query string, never a body.
// It pays the general Helix budget like processAPI/processAnnounce, then hands
// the assembled request to execute() for the shared status handling.
func (w *Worker) processShoutout(ctx context.Context, payload outgress.Message) error {

	if payload.To == "" {
		w.log.Error("dropping shoutout: no target login",
			zap.String("broadcaster_id", payload.BroadcasterID))
		return nil
	}

	// The moderator issuing the shoutout is the bot: prefer an explicit sender,
	// else the configured bot id. Without one there is nobody to act as, so drop
	// (mirroring processAnnounce's no-moderator guard).
	mod := payload.SenderID
	if mod == "" {
		mod = w.botID
	}
	if mod == "" {
		w.log.Error("dropping shoutout: no bot moderator id configured",
			zap.String("broadcaster_id", payload.BroadcasterID))
		return nil
	}

	// Resolve the target login to its numeric id (cached, single-flight). A
	// loader error is transient (nack so paced redelivery retries); a "" id means
	// no such user, which retrying can never fix, so drop instead of nacking.
	toID, err := w.userIDs.GetOrLoad(ctx, "login:"+strings.ToLower(payload.To),
		func(ctx context.Context) (string, error) {
			return w.twitch.UserIDByLogin(ctx, payload.To)
		})
	if err != nil {
		w.log.Warn("shoutout target resolve failed, will retry",
			zap.String("broadcaster_id", payload.BroadcasterID),
			zap.String("to", payload.To), zap.Error(err))
		return err
	}
	if toID == "" {
		w.log.Warn("dropping shoutout: no such target user",
			zap.String("broadcaster_id", payload.BroadcasterID),
			zap.String("to", payload.To))
		return nil
	}

	if err := w.takeGeneralHelix(ctx); err != nil {
		return err
	}

	payload.As = outgress.AsApp
	payload.Method = http.MethodPost
	payload.Endpoint = shoutoutEndpoint(payload.BroadcasterID, toID, mod)
	payload.Payload = nil

	return w.execute(ctx, payload)
}

// processEventSub applies the receive toggle for one channel, paying the
// reserved system Helix bucket once per HTTP call. Conduit EventSub
// management runs under the app token. Chat (channel.chat.message) is read in
// the bot account's user context (the bot's user:read:chat / user:bot grant
// plus the broadcaster's channel:bot grant); the broadcaster events (subs,
// cheers, follows, channel.update title changes) are authorized by the
// broadcaster's own consent (channel:read:subscriptions, bits:read,
// moderator:read:followers). No bot user token is involved here. Creates are 409-idempotent and deletes
// 404-idempotent, so a job nacked halfway (rate limit, transient Twitch
// error) converges when redelivery re-runs it.
func (w *Worker) processEventSub(ctx context.Context, payload outgress.Message) error {

	if payload.BroadcasterID == "" {
		w.log.Error("dropping eventsub job without broadcaster id")
		return nil
	}

	conduitID, err := w.conduit.Get(ctx)
	if err != nil {
		w.log.Warn("eventsub job cannot resolve conduit id, will retry",
			zap.String("broadcaster_id", payload.BroadcasterID),
			zap.Error(err))
		return err
	}

	var job outgress.EventSubJob
	if err := sonic.Unmarshal(payload.Payload, &job); err != nil {
		w.log.Error("dropping malformed eventsub job", zap.Error(err))
		if txn := newrelic.FromContext(ctx); txn != nil {
			txn.NoticeError(err)
		}
		return nil
	}

	// Resolve effective mode: explicit Mode wins; empty falls back to legacy Enabled field.
	mode := job.Mode
	if mode == "" {
		if job.Enabled {
			mode = outgress.ModeEnable
		} else {
			mode = outgress.ModeDisable
		}
	}

	switch mode {
	case outgress.ModeEnable:
		return w.enableEventSubs(ctx, payload.BroadcasterID, conduitID)
	case outgress.ModeDisable:
		return w.disableEventSubs(ctx, payload.BroadcasterID, conduitID)
	case outgress.ModeReconnect:
		return w.reconnectEventSubs(ctx, payload.BroadcasterID, conduitID)
	default:
		w.log.Error("dropping eventsub job with unknown mode",
			zap.String("mode", mode),
			zap.String("broadcaster_id", payload.BroadcasterID))
		return nil
	}
}

// processStreamStatus resolves one broadcaster's live state from Twitch (Helix
// Get Streams) and writes it back into the live projection. It pays the reserved
// system Helix bucket and runs only on the system lane (where SetLiveWriter has
// attached the write-back). A permanent Twitch rejection is dropped; transient
// errors nack so the paced redelivery retries.
func (w *Worker) processStreamStatus(ctx context.Context, payload outgress.Message) error {

	if w.live == nil {
		w.log.Error("dropping stream_status job off the system lane")
		return nil
	}
	if payload.BroadcasterID == "" {
		w.log.Error("dropping stream_status job without broadcaster id")
		return nil
	}

	if err := w.takeSystemHelix(ctx); err != nil {
		return err
	}

	isLive, err := w.twitch.IsStreamLive(ctx, payload.BroadcasterID)
	if err != nil {
		if isPermanent(err) {
			w.log.Error("dropping stream_status twitch rejected",
				zap.String("broadcaster_id", payload.BroadcasterID), zap.Error(err))
			if txn := newrelic.FromContext(ctx); txn != nil {
				txn.NoticeError(err)
			}
			return nil
		}
		w.log.Warn("stream_status check failed, will retry",
			zap.String("broadcaster_id", payload.BroadcasterID), zap.Error(err))
		return err
	}

	if err := w.live.Write(ctx, payload.BroadcasterID, isLive); err != nil {
		return err
	}

	if isLive {
		// Proactively re-verify in the background when a channel goes live.
		w.scheduleModStatus(payload.BroadcasterID, payload.SenderID)
	}

	w.log.Debug("stream_status resolved",
		zap.String("broadcaster_id", payload.BroadcasterID), zap.Bool("live", isLive))
	return nil
}

// HandleStreamEvent reacts to a real Twitch stream.online / stream.offline
// EventSub message off the ingress stream lane (env NATS_SUBJECT_LANE_STREAM).
//
// Background: the worker fleet escalates a cold live query to the system lane's
// stream_status path, which re-verifies the bot's mod status as a side effect.
// Once stream.online events flow and the projector writes the live key directly,
// that live query is no longer cold, so the escalation (and its mod-status
// re-verify) never runs. This handler restores the re-verify by reacting to the
// real go-live event itself.
//
// It is bound under outgress's OWN durable group (separate from the projector's),
// so every event is delivered here once in addition to the projector's copy. It
// does NOT write live state (that is the projector's job); it only re-verifies
// mod status, best-effort. Decoding is shared with the projector via the domain
// stream_status decoder. Always acks (returns nil): a re-verify is advisory and
// must never poison or replay the lane.
func (w *Worker) HandleStreamEvent(msg *message.Message) error {

	status, ok := eventtwitch.DecodeStreamStatus(msg.Payload)
	if !ok {
		// Not a stream.online/offline we understand (or malformed). Ack and move
		// on; the decoder already rejects everything but those two types.
		return nil
	}

	// Only go-live triggers the re-verify; an offline event needs no mod check.
	if !status.Live {
		return nil
	}

	broadcasterID := strconv.FormatUint(status.BroadcasterID, 10)

	w.scheduleModStatus(broadcasterID, "")

	w.log.Debug("mod status refresh scheduled on go-live",
		zap.String("broadcaster_id", broadcasterID))
	return nil
}

func (w *Worker) enableEventSubs(ctx context.Context, broadcasterID, conduitID string) error {

	if w.botID == "" {
		w.log.Warn("bot user id not configured, channel chat subscription will be skipped",
			zap.String("broadcaster_id", broadcasterID))
	}

	for _, spec := range twitch.ChannelSubscriptions(broadcasterID, w.botID) {
		if err := w.takeSystemHelix(ctx); err != nil {
			return err
		}
		if err := w.twitch.CreateEventSub(ctx, spec, conduitID); err != nil {
			w.conduit.Invalidate()
			return w.eventSubFailure(ctx, err, "eventsub create", broadcasterID, spec.Type)
		}
	}

	w.log.Info("eventsub subscriptions created", zap.String("broadcaster_id", broadcasterID))
	return nil
}

func (w *Worker) disableEventSubs(ctx context.Context, broadcasterID, conduitID string) error {

	deleted := 0
	cursor := ""
	for {
		if err := w.takeSystemHelix(ctx); err != nil {
			return err
		}
		subs, next, err := w.twitch.ListEventSubs(ctx, broadcasterID, cursor)
		if err != nil {
			return w.eventSubFailure(ctx, err, "eventsub list", broadcasterID, "")
		}

		for _, sub := range subs {
			if sub.Transport.ConduitID != conduitID {
				continue
			}
			// The list query (?user_id) also returns subs where this id is the
			// condition's user_id/moderator, not the broadcaster: notably every
			// channel's channel.chat.message carries the bot as user_id. Only
			// delete subs this broadcaster actually owns, or reconnecting the bot
			// account would wipe every channel's chat subscription.
			if sub.Condition.BroadcasterUserID != "" && sub.Condition.BroadcasterUserID != broadcasterID {
				continue
			}
			if err := w.takeSystemHelix(ctx); err != nil {
				return err
			}
			if err := w.twitch.DeleteEventSub(ctx, sub.ID); err != nil {
				return w.eventSubFailure(ctx, err, "eventsub delete", broadcasterID, "")
			}
			deleted++
		}

		if next == "" {
			break
		}
		cursor = next
	}

	w.log.Info("eventsub subscriptions removed",
		zap.String("broadcaster_id", broadcasterID), zap.Int("deleted", deleted))
	return nil
}

// eventSubFailure splits permanent rejections (bad request, missing consent
// scopes) from everything retryable. Permanent ones are dropped with a log
// line; retrying them would burn the whole redelivery budget for nothing.
func (w *Worker) eventSubFailure(ctx context.Context, err error, op, broadcasterID, subType string) error {

	var status *twitch.StatusError
	if errors.As(err, &status) &&
		status.Status >= 400 && status.Status < 500 &&
		status.Status != http.StatusTooManyRequests &&
		status.Status != http.StatusUnauthorized {
		w.log.Error("dropping eventsub job twitch rejected",
			zap.String("op", op),
			zap.String("broadcaster_id", broadcasterID),
			zap.String("subscription", subType),
			zap.Error(err))
		if txn := newrelic.FromContext(ctx); txn != nil {
			txn.NoticeError(err)
		}
		return nil
	}

	w.log.Warn("eventsub job failed, will retry",
		zap.String("op", op),
		zap.String("broadcaster_id", broadcasterID),
		zap.Error(err))
	return err
}

// reconnectEventSubs performs an atomic drop-then-recreate of all eventsub
// subscriptions for one broadcaster. It acquires a Valkey single-flight lock
// so only one replica works the reconnect; others ack and return immediately.
// The recreate phase is retried up to 3 times for transient errors. Outcome is
// persisted to the registry (pending -> ok | failing) so the dashboard can
// surface it without polling Twitch.
func (w *Worker) reconnectEventSubs(ctx context.Context, broadcasterID, conduitID string) error {

	got, err := w.registry.AcquireEnrollLock(ctx, broadcasterID, w.owner, 60*time.Second)
	if err != nil {
		return err // transient valkey error: nak, let paced redelivery retry
	}
	if !got {
		w.log.Info("reconnect already in progress on another replica",
			zap.String("broadcaster_id", broadcasterID))
		return nil // ack: another replica owns it
	}
	defer func() { _ = w.registry.ReleaseEnrollLock(ctx, broadcasterID, w.owner) }()

	_ = w.registry.SetSubState(ctx, broadcasterID, "pending", "")

	// Best-effort drop: if listing/deleting fails we still try to recreate;
	// 409 idempotency on create means the end state converges either way.
	if derr := w.disableEventSubs(ctx, broadcasterID, conduitID); derr != nil {
		w.log.Warn("reconnect: drop phase failed, proceeding to recreate",
			zap.String("broadcaster_id", broadcasterID),
			zap.Error(derr))
	}

	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		lastErr = w.createAllEventSubs(ctx, broadcasterID, conduitID)
		if lastErr == nil {
			_ = w.registry.SetSubState(ctx, broadcasterID, "ok", "")
			w.log.Info("reconnect: all eventsubs accepted",
				zap.String("broadcaster_id", broadcasterID))
			return nil
		}
		if isPermanent(lastErr) {
			break // 403 etc: retrying will not help
		}
		// transient: small back-off before next attempt
		select {
		case <-ctx.Done():
			break
		case <-time.After(time.Duration(attempt) * 500 * time.Millisecond):
		}
	}

	_ = w.registry.SetSubState(ctx, broadcasterID, "failing", lastErr.Error())
	w.log.Error("reconnect: eventsubs not fully accepted, marked failing",
		zap.String("broadcaster_id", broadcasterID),
		zap.Error(lastErr))
	return nil // ack: failing state is surfaced for the operator
}

// createAllEventSubs creates every SubSpec for the channel; returns the first
// error (with the failing subscription type) or nil when all are accepted
// (202 or 409-idempotent).
func (w *Worker) createAllEventSubs(ctx context.Context, broadcasterID, conduitID string) error {

	if w.botID == "" {
		// chat sub cannot be built without a bot id; treat as a hard failure
		// because an all-or-nothing reconnect must not silently skip it.
		return fmt.Errorf("bot user id not configured: channel.chat.message cannot be created")
	}

	for _, spec := range twitch.ChannelSubscriptions(broadcasterID, w.botID) {
		if err := w.takeSystemHelix(ctx); err != nil {
			return err
		}
		if err := w.twitch.CreateEventSub(ctx, spec, conduitID); err != nil {
			w.conduit.Invalidate()
			return fmt.Errorf("create %s: %w", spec.Type, err)
		}
	}

	return nil
}

// isPermanent reports whether err is a non-retryable Twitch rejection:
// any 4xx except 429 (rate limit) and 401 (auth may recover).
func isPermanent(err error) bool {
	var se *twitch.StatusError
	if errors.As(err, &se) {
		return se.Status >= 400 && se.Status < 500 &&
			se.Status != http.StatusTooManyRequests &&
			se.Status != http.StatusUnauthorized
	}
	return false
}

// takeGeneralHelix consumes one token from the general Helix budget, the
// partition that backs ordinary api traffic.
func (w *Worker) takeGeneralHelix(ctx context.Context) error {
	shared := helixGeneralSpec.ForKey("ratelimit:helix:app")
	if w.lane == LaneStandard {
		standard := helixStandardSpec.ForKey("ratelimit:helix:app:standard")
		return w.takeOrdered(ctx, standard, shared)
	}
	return w.take(ctx, shared)
}

// takeSystemHelix consumes one token from the reserved system partition.
// Only the system lane pays here, so dashboard EventSub jobs always have
// their reserved capacity and can never spend the general api budget.
func (w *Worker) takeSystemHelix(ctx context.Context) error {
	return w.take(ctx, helixSystemSpec.ForKey("ratelimit:helix:system"))
}

// take consumes one token or returns an error that nacks the message, so the
// paced redelivery retries it once the bucket has refilled.
func (w *Worker) take(ctx context.Context, req ratelimit.Request) error {
	started := time.Now()
	defer recordStageDuration(ctx, "outgress.limiter_ms", started)
	allowed, err := w.limiter.Allow(ctx, req)
	if err != nil {
		return err
	}
	if !allowed {
		return errRateLimitShared
	}
	return nil
}

func (w *Worker) takeOrdered(ctx context.Context, first, shared ratelimit.Request) error {
	started := time.Now()
	defer recordStageDuration(ctx, "outgress.limiter_ms", started)
	denied, err := w.limiter.AllowOrdered(ctx, first, shared)
	if err != nil {
		return err
	}
	switch denied {
	case 0:
		return nil
	case 1:
		return errRateLimitFirst
	default:
		return errRateLimitShared
	}
}

// modStatus is deliberately non-blocking: use the last known value and let the
// shared verifier refresh stale state away from the chat handler.
func (w *Worker) modStatus(_ context.Context, payload outgress.Message, ch manage.Channel, found bool) bool {
	if w.modVerifier == nil {
		return found && ch.IsMod
	}
	return w.modVerifier.Status(ch, found, payload.BroadcasterID, payload.SenderID)
}

func (w *Worker) scheduleModStatus(broadcasterID, senderID string) {
	if w.modVerifier != nil {
		w.modVerifier.Schedule(broadcasterID, senderID)
	}
}

func (w *Worker) execute(ctx context.Context, payload outgress.Message) error {
	started := time.Now()
	defer recordStageDuration(ctx, "outgress.twitch_ms", started)

	res, err := w.twitch.ExecuteAs(ctx, twitch.ParseIdentity(payload.As),
		payload.BroadcasterID, payload.Method, payload.Endpoint, payload.Payload)
	if err != nil {
		w.log.Error("twitch request failed", zap.Error(err))
		return err
	}
	defer drainResponse(res)

	switch {
	case res.StatusCode == http.StatusTooManyRequests:
		w.log.Warn("twitch rate limited the app",
			zap.String("endpoint", payload.Endpoint),
			zap.Duration("retry_after", twitch.RetryAfter(res)))
		return fmt.Errorf("twitch 429 on %s", payload.Endpoint)

	case res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden:
		// The client already retried once with a freshly minted token and Twitch
		// still rejected it. A fresh token being refused is a PERMANENT
		// authorization problem (a missing scope, the bot not being a moderator
		// of the channel, or a moderator_id/token mismatch), not a recoverable
		// token expiry, so redelivering it just loops forever and poisons the
		// lane. Drop it (ack) and surface it loudly + to New Relic for a human to
		// fix (re-auth / mod the bot). Twitch's body states which of the three.
		body, _ := io.ReadAll(io.LimitReader(res.Body, 2048))
		w.log.Error("dropping request: twitch rejected our credentials (permanent authz problem, not retryable)",
			zap.Int("status", res.StatusCode),
			zap.String("endpoint", payload.Endpoint),
			zap.String("as", payload.As),
			zap.String("body", string(body)))
		if txn := newrelic.FromContext(ctx); txn != nil {
			txn.NoticeError(fmt.Errorf("twitch auth failure: %d %s", res.StatusCode, string(body)))
		}
		return nil

	case res.StatusCode >= 500:
		return fmt.Errorf("twitch server error: %d", res.StatusCode)

	case res.StatusCode >= 400:
		body, _ := io.ReadAll(io.LimitReader(res.Body, 2048))
		w.log.Error("dropping request twitch rejected",
			zap.Int("status", res.StatusCode),
			zap.String("endpoint", payload.Endpoint),
			zap.String("body", string(body)))
		if txn := newrelic.FromContext(ctx); txn != nil {
			txn.NoticeError(fmt.Errorf("twitch rejected request: %d %s", res.StatusCode, string(body)))
		}
		return nil
	}

	if strings.HasPrefix(payload.Endpoint, "/helix/chat/messages") {
		body, err := io.ReadAll(io.LimitReader(res.Body, 2048))
		if err == nil {
			w.log.Info("twitch chat message response",
				zap.String("endpoint", payload.Endpoint),
				zap.String("body", string(body)))
		}
	}

	return nil
}

const maxResponseDrain = 64 << 10

// drainResponse makes small HTTP/1.1 responses reusable without allowing an
// unexpectedly large or non-terminating body to pin a worker indefinitely. The
// client's total timeout still bounds a slow body.
func drainResponse(res *http.Response) {
	_, _ = io.CopyN(io.Discard, res.Body, maxResponseDrain+1)
	_ = res.Body.Close()
}
