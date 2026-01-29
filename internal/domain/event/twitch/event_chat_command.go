package twitch

type EventChatCommandDTO struct {
	Event         EventTwitchDTO  `json:"event"`
	Command       string          `json:"command"`
	Arguments     string          `json:"arguments"`
	Username      string          `json:"username"`
	UserID        string          `json:"user_id"`
	PermissionLvl PermissionLevel `json:"-"`
}

type EventChatCommand struct {
	event         EventTwitch
	command       string
	arguments     string
	username      string
	userID        string
	permissionLvl PermissionLevel
}

func NewEventChatCommand(dto EventChatCommandDTO) EventChatCommand {
	return EventChatCommand{
		command:       dto.Command,
		arguments:     dto.Arguments,
		username:      dto.Username,
		userID:        dto.UserID,
		permissionLvl: dto.PermissionLvl,
	}
}
