package main

import (
	"encoding/json"

	"ItsBagelBot/app/projector/store"
	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/internal/domain/validate"

	"github.com/ThreeDotsLabs/watermill/message"

	"go.uber.org/zap"
)

// Projector folds the change events of the data services into the Valkey
// settings projection. Every handler is an overwrite of the new state carried
// by the event itself, which makes redeliveries and full replays safe. The
// message context carries the New Relic transaction opened by the consumer,
// so the store's Valkey segments land on the right trace.
//
// Payloads are validated before they touch Valkey: the bus is internal, but a
// buggy or compromised publisher must not be able to forge projection fields.
// Malformed or invalid events are dropped (logged and acked, never nacked),
// because redelivering a poison message forever helps no one.
type Projector struct {
	store *store.Valkey
	log   *zap.Logger
}

func NewProjector(store *store.Valkey, log *zap.Logger) *Projector {
	return &Projector{store: store, log: log}
}

func (p *Projector) HandleUserChanged(msg *message.Message) error {

	var dto data.UserChangedDTO
	if err := json.Unmarshal(msg.Payload, &dto); err != nil {
		p.drop(msg, data.SubjectUserChanged, err)
		return nil
	}

	if err := validate.UserID(dto.UserID); err != nil {
		p.drop(msg, data.SubjectUserChanged, err)
		return nil
	}
	if err := validate.Status(dto.Status); err != nil {
		p.drop(msg, data.SubjectUserChanged, err)
		return nil
	}

	return p.store.SetUser(msg.Context(), dto.UserID, dto.Status, dto.IsActive)
}

func (p *Projector) HandleUserDeleted(msg *message.Message) error {

	var dto data.UserDeletedDTO
	if err := json.Unmarshal(msg.Payload, &dto); err != nil {
		p.drop(msg, data.SubjectUserDeleted, err)
		return nil
	}

	if err := validate.UserID(dto.UserID); err != nil {
		p.drop(msg, data.SubjectUserDeleted, err)
		return nil
	}

	return p.store.DeleteUser(msg.Context(), dto.UserID)
}

func (p *Projector) HandleModuleChanged(msg *message.Message) error {

	var dto data.ModuleChangedDTO
	if err := json.Unmarshal(msg.Payload, &dto); err != nil {
		p.drop(msg, data.SubjectModuleChanged, err)
		return nil
	}

	if err := validate.UserID(dto.UserID); err != nil {
		p.drop(msg, data.SubjectModuleChanged, err)
		return nil
	}
	if err := validate.ModuleName(dto.Name); err != nil {
		p.drop(msg, data.SubjectModuleChanged, err)
		return nil
	}
	if err := validate.ConfigsJSON(dto.Configs); err != nil {
		p.drop(msg, data.SubjectModuleChanged, err)
		return nil
	}

	return p.store.SetModule(msg.Context(), dto.UserID, dto.Name, dto.IsEnabled, dto.Configs)
}

func (p *Projector) drop(msg *message.Message, subject string, err error) {

	p.log.Warn("dropping invalid event",
		zap.String("subject", subject),
		zap.String("message_id", msg.UUID),
		zap.Error(err),
	)
}
