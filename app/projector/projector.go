package main

import (
	"encoding/json"

	"ItsBagelBot/internal/projection"
	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/internal/domain/validate"
	"ItsBagelBot/pkg/bus"

	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/nats-io/nats.go"
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
	store *projection.Store
	log   *zap.Logger
}

func NewProjector(store *projection.Store, log *zap.Logger) *Projector {
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

	return p.store.SetUser(msg.Context(), dto.UserID, dto.Status, dto.IsActive, dto.Banned)
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

func (p *Projector) HandleCommandChanged(msg *message.Message) error {
	var dto data.CommandChangedDTO
	if err := json.Unmarshal(msg.Payload, &dto); err != nil {
		p.drop(msg, data.SubjectCommandChanged, err)
		return nil
	}

	if err := validate.UserID(dto.UserID); err != nil {
		p.drop(msg, data.SubjectCommandChanged, err)
		return nil
	}
	if err := validate.CommandName(dto.Name); err != nil {
		p.drop(msg, data.SubjectCommandChanged, err)
		return nil
	}
	if !dto.Deleted {
		if err := validate.CommandResponse(dto.Response); err != nil {
			p.drop(msg, data.SubjectCommandChanged, err)
			return nil
		}
		if err := validate.Perm(dto.Perm); err != nil {
			p.drop(msg, data.SubjectCommandChanged, err)
			return nil
		}
		if err := validate.Cooldown(dto.Cooldown); err != nil {
			p.drop(msg, data.SubjectCommandChanged, err)
			return nil
		}
		if dto.AllowedUserID != 0 {
			if err := validate.UserID(dto.AllowedUserID); err != nil {
				p.drop(msg, data.SubjectCommandChanged, err)
				return nil
			}
		}
	}

	return p.store.SetCommand(msg.Context(), dto)
}

func (p *Projector) drop(msg *message.Message, subject string, err error) {

	p.log.Warn("dropping invalid event",
		zap.String("subject", subject),
		zap.String("message_id", msg.UUID),
		zap.Error(err),
	)
}

type eventSubMessage struct {
	Type         string `json:"type"`
	Subscription struct {
		Type string `json:"type"`
	} `json:"subscription"`
	Event struct {
		BroadcasterUserID string `json:"broadcaster_user_id"`
	} `json:"event"`
}

func (e eventSubMessage) eventType() string {
	if e.Type != "" {
		return e.Type
	}
	return e.Subscription.Type
}

func (p *Projector) HandleStreamEvent(msg *nats.Msg, nc *nats.Conn, usersTopic, modulesTopic, commandsTopic string) {
	var payload eventSubMessage
	if err := json.Unmarshal(msg.Data, &payload); err != nil {
		return
	}

	eventType := payload.eventType()
	if eventType != "stream.online" && eventType != "stream.offline" {
		return
	}

	id, err := strconv.ParseUint(payload.Event.BroadcasterUserID, 10, 64)
	if err != nil {
		return
	}

	liveCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := p.store.SetStreamLive(liveCtx, id, eventType == "stream.online"); err != nil {
		p.log.Warn("failed to project stream live state", zap.Uint64("user_id", id), zap.Error(err))
	}

	if eventType != "stream.online" {
		return
	}

	p.log.Info("pre-warming cache for stream online", zap.Uint64("user_id", id))

	req := map[string]string{"user_id": fmt.Sprint(id)}

	// 1. Fetch & Cache Users
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		reply, err := bus.RequestJSON[struct {
			Status   string `json:"status"`
			IsActive bool   `json:"is_active"`
			Banned   bool   `json:"banned"`
		}](ctx, nc, usersTopic, req)
		if err == nil {
			_ = p.store.SetUser(ctx, id, reply.Status, reply.IsActive, reply.Banned)
		}
	}()

	// 2. Fetch & Cache Modules
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		reply, err := bus.RequestJSON[struct {
			Modules []projection.ModuleView `json:"modules"`
		}](ctx, nc, modulesTopic, req)
		if err == nil {
			_ = p.store.SetModules(ctx, id, reply.Modules)
		}
	}()

	// 3. Fetch & Cache Commands
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		reply, err := bus.RequestJSON[struct {
			Commands []projection.CommandView `json:"commands"`
		}](ctx, nc, commandsTopic, req)
		if err == nil {
			_ = p.store.SetCommands(ctx, id, reply.Commands)
		}
	}()
}
