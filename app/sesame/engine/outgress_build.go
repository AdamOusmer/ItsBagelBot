package engine

import (
	"ItsBagelBot/app/sesame/module"
	"ItsBagelBot/internal/domain/outgress"

	"github.com/bytedance/sonic"
)

// banData is the inner object of a Helix Ban User request body. Duration is
// omitted for a permanent ban and set (in seconds) for a timeout; reason is
// optional.
type banData struct {
	UserID   string `json:"user_id"`
	Duration int    `json:"duration,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

// buildOutgress translates a module Output into the marshaled bytes of the full
// outgress.Message wire contract. The inner Payloads are built from small typed
// structs rather than maps so sonic escapes emoji and quotes in the body
// correctly. This runs only when a handler actually emits, so the allocation it
// costs never touches the no-output plain-chat path.
func buildOutgress(o *module.Output) ([]byte, error) {
	build, ok := outgressBuilders[o.Type]
	if !ok {
		return sonic.Marshal(outgress.Message{
			Type:          o.Type,
			BroadcasterID: o.BroadcasterID,
		})
	}
	msg, err := build(o)
	if err != nil {
		return nil, err
	}
	return sonic.Marshal(msg)
}

// outgressBuilders maps each intent to its wire-message construction; types
// absent here (generic passthroughs) carry only type + broadcaster.
var outgressBuilders = map[string]func(*module.Output) (outgress.Message, error){
	outgress.TypeChat:             chatOutgress,
	outgress.TypeAnnounce:         announceOutgress,
	outgress.TypeShoutout:         shoutoutOutgress,
	outgress.TypeClip:             clipOutgress,
	outgress.TypeBan:              banOutgress,
	outgress.TypeTimeout:          banOutgress,
	outgress.TypeShieldMode:       shieldOutgress,
	outgress.TypeDelete:           deleteOutgress,
	outgress.TypeWarn:             warnOutgress,
	outgress.TypeRedemptionUpdate: redemptionUpdateOutgress,
}

// payloadMessage marshals one typed payload and wraps it in the wire message.
func payloadMessage(msgType, broadcasterID string, payload any) (outgress.Message, error) {
	body, err := sonic.Marshal(payload)
	if err != nil {
		return outgress.Message{}, err
	}
	return outgress.Message{
		Type:          msgType,
		BroadcasterID: broadcasterID,
		Payload:       body,
	}, nil
}

func chatOutgress(o *module.Output) (outgress.Message, error) {
	return payloadMessage(outgress.TypeChat, o.BroadcasterID, struct {
		BroadcasterID string `json:"broadcaster_id"`
		Message       string `json:"message"`
	}{o.BroadcasterID, o.Text})
}

func announceOutgress(o *module.Output) (outgress.Message, error) {
	msg, err := payloadMessage(outgress.TypeAnnounce, o.BroadcasterID, struct {
		Message string `json:"message"`
	}{o.Text})
	msg.Color = o.Color
	return msg, err
}

func shoutoutOutgress(o *module.Output) (outgress.Message, error) {
	return outgress.Message{
		Type:          outgress.TypeShoutout,
		BroadcasterID: o.BroadcasterID,
		To:            o.To,
		Payload:       []byte("{}"),
	}, nil
}

// clipOutgress builds the Create Clip job. The call takes no body:
// broadcaster_id, title and duration all ride the query string, which outgress
// builds. This payload carries what outgress needs — the title and duration to
// pass to Twitch, the clipper's display name, and the broadcaster's custom reply
// template — to compose the reply posted with the clip URL (outgress expands
// its {clip} token). Duration 0 (plain !clip) and an empty reply are omitted.
func clipOutgress(o *module.Output) (outgress.Message, error) {
	return payloadMessage(outgress.TypeClip, o.BroadcasterID, struct {
		Title    string  `json:"title,omitempty"`
		Clipper  string  `json:"clipper,omitempty"`
		Duration float64 `json:"duration,omitempty"`
		Reply    string  `json:"reply,omitempty"`
	}{o.Text, o.To, o.Duration, o.Template})
}

// banOutgress builds the Helix Ban User body: {"data":{"user_id","duration",
// "reason"}}. A ban omits duration (permanent); a timeout sets it (whole
// seconds; Output shares the Duration field with clip, which carries a
// fraction). broadcaster_id and moderator_id are added by outgress on the
// query string, not here.
func banOutgress(o *module.Output) (outgress.Message, error) {
	return payloadMessage(o.Type, o.BroadcasterID, struct {
		Data banData `json:"data"`
	}{banData{UserID: o.TargetUserID, Duration: int(o.Duration), Reason: o.Reason}})
}

// shieldOutgress builds the Helix Update Shield Mode Status body:
// {"is_active":true}. The automod only ever activates (mass-raid escalation);
// deactivation stays a human decision. broadcaster_id and moderator_id ride
// the query string, added by outgress.
func shieldOutgress(o *module.Output) (outgress.Message, error) {
	return outgress.Message{
		Type:          outgress.TypeShieldMode,
		BroadcasterID: o.BroadcasterID,
		Payload:       []byte(`{"is_active":true}`),
	}, nil
}

// deleteOutgress builds the Delete Chat Messages job; Helix takes everything
// on the query string (broadcaster_id + moderator_id added by outgress,
// message_id from MsgID); no body.
func deleteOutgress(o *module.Output) (outgress.Message, error) {
	return outgress.Message{
		Type:          outgress.TypeDelete,
		BroadcasterID: o.BroadcasterID,
		MsgID:         o.MsgID,
	}, nil
}

// warnOutgress builds the Helix Warn Chat User body: {"data":{"user_id",
// "reason"}} (a warning requires a reason; Twitch shows it to the chatter).
// broadcaster_id and moderator_id ride the query string, added by outgress.
func warnOutgress(o *module.Output) (outgress.Message, error) {
	return payloadMessage(outgress.TypeWarn, o.BroadcasterID, struct {
		Data banData `json:"data"`
	}{banData{UserID: o.TargetUserID, Reason: o.Reason}})
}

// redemptionUpdateOutgress builds the Update Redemption Status job. Everything
// rides dedicated Message fields (reward id, redemption id, target status) that
// outgress puts on the query string / a small body; there is no payload here.
func redemptionUpdateOutgress(o *module.Output) (outgress.Message, error) {
	return outgress.Message{
		Type:          outgress.TypeRedemptionUpdate,
		BroadcasterID: o.BroadcasterID,
		RewardID:      o.RewardID,
		RedemptionID:  o.RedemptionID,
		Status:        o.Status,
	}, nil
}
