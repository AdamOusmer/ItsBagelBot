package twitch

import "ItsBagelBot/internal/domain/event"

const (
	EventTwitchType                 = "twitch"
	EventTypeChatCommand            = "twitch.chat.command"
	EventTypeStreamOnline           = "twitch.stream.online"
	EventTypeStreamOffline          = "twitch.stream.offline"
	EventTypeNewFollower            = "twitch.follower.new"
	EventTypeNewSubscriber          = "twitch.monetization.subscription"
	EventTypeGiftedSubscription     = "twitch.monetization.subscription.gift"
	EventTypeBitsCheer              = "twitch.bits.cheer"
	EventTypeChannelPointRedemption = "twitch.channel.point.redemption"
	EventTypeRaidIn                 = "twitch.raid.in"
	EventTypeRaidOut                = "twitch.raid.out"
)

type PermissionLevel struct {
	isBroadcaster   bool
	isLeadModerator bool
	isModerator     bool
	isVIP           bool
	isSubscriber    bool
}

func (p PermissionLevel) IsBroadcaster() bool {
	return p.isBroadcaster
}

func (p PermissionLevel) IsLeadModerator() bool {
	return p.isLeadModerator
}

func (p PermissionLevel) IsModerator() bool {
	return p.isModerator
}

func (p PermissionLevel) IsVIP() bool {
	return p.isVIP
}

func (p PermissionLevel) IsSubscriber() bool {
	return p.isSubscriber
}

type EventTwitchDTO struct {
	Channel string          `json:"channel"`
	Event   event.BaseEvent `json:"event"`
}

type EventTwitch struct {
	channel string
	event   event.BaseEvent
}

func (e EventTwitch) Channel() string {
	return e.channel
}

func NewTwitchEvent(channel string, priority bool) (EventTwitch, error) {
	baseEvent, err := event.NewBaseEvent(EventTwitchType, priority)
	if err != nil {
		return EventTwitch{}, err
	}

	return EventTwitch{
		channel: channel,
		event:   baseEvent,
	}, nil
}
