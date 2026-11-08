// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Package protocol owns the desktop bridge's typed protobuf contract and
// validation. Native channels, credentials and I/O belong to shared packages; account
// binding, retries, heartbeat deadlines and migration belong to the supervisor.
package protocol

import (
	"errors"
	"unicode/utf8"

	"ItsBagelBot/pkg/protobuf"
)

const Version = 1
const MaxFrameBytes = 64 << 10
const MaxWindowMS = 120_000
const Subprotocol = "bagel.bridge.v1"
const EventsPath = "/bridge/v1/events"
const BindPath = "/bridge/v1/bind"
const MigrationPath = "/bridge/v1/migration"

type Message = ServerMessage

const (
	Unknown        = Kind_UNKNOWN
	SessionStarted = Kind_SESSION_STARTED
	SessionBound   = Kind_SESSION_BOUND
	Reconnect      = Kind_RECONNECT
	HandoffReady   = Kind_HANDOFF_READY
	EventMessage   = Kind_EVENT
	AuthError      = Kind_AUTH_ERROR
	Keepalive      = Kind_KEEPALIVE
)

var (
	ErrVersion = errors.New("bridge protocol: unsupported version")
	ErrMessage = errors.New("bridge protocol: invalid message")
	ErrField   = errors.New("bridge protocol: unexpected field")
)

const (
	sessionField uint16 = 1 << iota
	bindWindowField
	migrationField
	windowField
	targetField
	throughField
	eventField
	codeField
)

func Validate(message *Message) error {
	if message == nil {
		return ErrMessage
	}
	if protobuf.HasUnknown(message) {
		return ErrField
	}
	if message.Version != Version {
		return ErrVersion
	}
	allowed, ok := allowedFields(message.Kind)
	if !ok {
		return ErrMessage
	}
	if presentFields(message) & ^allowed != 0 {
		return ErrField
	}
	return validateRequired(message)
}

func presentFields(message *Message) uint16 {
	var fields uint16
	if message.SessionId != "" {
		fields |= sessionField
	}
	if message.BindWithinMs != 0 {
		fields |= bindWindowField
	}
	if message.Migration != "" {
		fields |= migrationField
	}
	if message.WithinMs != 0 {
		fields |= windowField
	}
	if message.Target != "" {
		fields |= targetField
	}
	if message.Through != 0 {
		fields |= throughField
	}
	if message.Event != nil {
		fields |= eventField
	}
	if message.Code != "" {
		fields |= codeField
	}
	return fields
}

func allowedFields(kind Kind) (uint16, bool) {
	switch kind {
	case SessionStarted:
		return sessionField | bindWindowField, true
	case SessionBound:
		return sessionField, true
	case Reconnect:
		return migrationField | windowField | targetField, true
	case HandoffReady:
		return migrationField | throughField, true
	case EventMessage:
		return eventField, true
	case AuthError:
		return codeField, true
	case Keepalive:
		return 0, true
	default:
		return 0, false
	}
}

func validateRequired(message *Message) error {
	switch message.Kind {
	case SessionStarted:
		return validateStarted(message)
	case SessionBound:
		return requireID(message.SessionId)
	case Reconnect:
		return validateReconnect(message)
	case HandoffReady:
		return requireID(message.Migration)
	case EventMessage:
		return ValidateEvent(message.Event)
	case AuthError:
		return requireID(message.Code)
	case Keepalive:
		return nil
	default:
		return ErrMessage
	}
}

func validateStarted(message *Message) error {
	if !validID(message.SessionId) || !validWindow(message.BindWithinMs) {
		return ErrMessage
	}
	return nil
}

func validateReconnect(message *Message) error {
	if !validID(message.Migration) || !validWindow(message.WithinMs) || !validText(message.Target, 2048) {
		return ErrMessage
	}
	return nil
}

func ValidateEvent(event *Event) error {
	if event == nil || event.Sequence == 0 {
		return ErrMessage
	}
	if protobuf.HasUnknown(event) {
		return ErrField
	}
	switch event.Kind {
	case EventKind_CHAT:
		return validateChat(event)
	case EventKind_ALERT:
		return validateAlert(event)
	default:
		return ErrMessage
	}
}

func validateChat(event *Event) error {
	if event.Chat == nil || event.Alert != nil {
		return ErrMessage
	}
	chat := event.Chat
	if protobuf.HasUnknown(chat) {
		return ErrField
	}
	if !validID(chat.ChannelId) || !validID(chat.UserId) {
		return ErrMessage
	}
	if !validText(chat.DisplayName, 256) || !validText(chat.Text, 16<<10) {
		return ErrMessage
	}
	return nil
}

func validateAlert(event *Event) error {
	if event.Alert == nil || event.Chat != nil {
		return ErrMessage
	}
	alert := event.Alert
	if protobuf.HasUnknown(alert) {
		return ErrField
	}
	if alert.Kind == "" || !validText(alert.Kind, 256) || !validText(alert.Name, 256) {
		return ErrMessage
	}
	return nil
}

func ValidateBind(request *BindAccount) error {
	if request == nil || !validID(request.SessionId) || !validText(request.Migration, 512) {
		return ErrMessage
	}
	if protobuf.HasUnknown(request) {
		return ErrField
	}
	return nil
}

func ValidateMigrationRequest(request *MigrationRequest) error {
	if request == nil {
		return ErrMessage
	}
	if protobuf.HasUnknown(request) {
		return ErrField
	}
	return requireID(request.SessionId)
}

func requireID(value string) error {
	if !validID(value) {
		return ErrMessage
	}
	return nil
}
func validID(value string) bool              { return value != "" && validText(value, 512) }
func validText(value string, limit int) bool { return len(value) <= limit && utf8.ValidString(value) }
func validWindow(value int64) bool           { return value > 0 && value <= MaxWindowMS }
