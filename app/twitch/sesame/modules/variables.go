// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/modulevars"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
	"ItsBagelBot/pkg/codec"
	"context"
	"errors"
)

var variableCatalog = modulevars.Catalog()

type variableRead = func(context.Context, *module.Context) (map[string]string, error)

func withVariables(d engine.Deps, m module.Module) module.Module {
	readers := moduleVariableReaders(d, m.Name)
	for _, spec := range variableCatalog {
		if spec.ID != m.Name {
			continue
		}
		for _, group := range spec.Groups {
			m.Variables = append(m.Variables, module.VariableGroup{Name: group.Name, Fields: group.Fields, Read: readers[group.Name]})
		}
	}
	return m
}

func moduleVariableReaders(d engine.Deps, name string) map[string]variableRead {
	switch name {
	case "valorant":
		return valorantVariableReaders(d)
	case "clashroyale":
		return clashVariableReaders(d)
	case "fortnite":
		return fortniteVariableReaders(d)
	case "codm":
		return codmVariableReaders(d)
	case "mcsr":
		return mcsrVariableReaders(d)
	case "urchin":
		return urchinVariableReaders(d)
	default:
		return localVariableReaders(d, name)
	}
}

// gossipVariables reads the same typed replies and palettes as the module's
// commands, independently of their trigger/toggle. It never dispatches a
// command or action endpoint. Session reads retain the provider's existing
// behavior of starting a baseline when none exists. Unknown/empty upstream
// data resolves to empty facts.
func gossipVariables[C any, R any](d engine.Deps, route engine.GossipRoute,
	request func(statsCall[C]) gossiprpc.Request,
	palette func(*module.Context, *R) module.StringPalette) variableRead {
	return func(ctx context.Context, c *module.Context) (map[string]string, error) {
		if d.Gossip == nil {
			return nil, nil
		}
		call, err := variableStatsCall[C](c)
		if err != nil {
			return nil, err
		}
		req := request(call)
		if !variableRequestHasTarget(route.Endpoint, req) {
			return nil, nil
		}
		var reply R
		if err := d.Gossip.Call(ctx, route, req, &reply); err != nil {
			return nil, err
		}
		return gossipVariablePalette(c, route, &reply, palette)
	}
}

func variableStatsCall[C any](c *module.Context) (statsCall[C], error) {
	var cfg C
	if len(c.Config) > 0 {
		if err := c.Decode(&cfg); err != nil {
			return statsCall[C]{}, err
		}
	}
	return statsCall[C]{Ctx: c, Cfg: cfg}, nil
}

func variableRequestHasTarget(endpoint string, request gossiprpc.Request) bool {
	switch endpoint {
	case "shop", "leaderboard":
		return true
	case "versus":
		return request.Account != "" && request.AccountB != ""
	default:
		return request.Account != ""
	}
}

// Decode provider state through the fleet codec instead of treating zero-valued
// stats as proof that a valid reply is empty.
type variableReplyState struct {
	Error       string `json:"error"`
	Empty       bool   `json:"empty"`
	Unranked    bool   `json:"unranked"`
	HasSnapshot *bool  `json:"has_snapshot"`
}

func variableStateOf(reply any) (variableReplyState, error) {
	raw, err := codec.Marshal(reply)
	if err != nil {
		return variableReplyState{}, err
	}
	var state variableReplyState
	if err := codec.Unmarshal(raw, &state); err != nil {
		return state, err
	}
	if state.Error != "" {
		return state, errors.New(state.Error)
	}
	return state, nil
}

func (s variableReplyState) unavailable() bool {
	if s.Empty || s.Unranked {
		return true
	}
	return s.HasSnapshot != nil && !*s.HasSnapshot
}

func gossipVariablePalette[R any](c *module.Context, route engine.GossipRoute, reply *R,
	palette func(*module.Context, *R) module.StringPalette) (map[string]string, error) {
	state, err := variableStateOf(*reply)
	if err != nil {
		return nil, err
	}
	values := palette(c, reply)
	if !state.unavailable() {
		return values, nil
	}
	retainVariableIdentity(values)
	if state.Unranked && route.Provider == "valorant" {
		values["tier"] = "Unranked"
	}
	return values, nil
}

func retainVariableIdentity(values module.StringPalette) {
	for field := range values {
		switch field {
		case "player", "region", "tag":
		default:
			delete(values, field)
		}
	}
}

func tokenValues[R any](tokens module.TokenExpander[R]) func(*module.Context, *R) module.StringPalette {
	return func(_ *module.Context, r *R) module.StringPalette {
		values := make(module.StringPalette, len(tokens))
		for field, read := range tokens {
			values[field] = read(r)
		}
		return values
	}
}

func variableAccount[C linkedConfig](preferUUID bool) func(statsCall[C]) gossiprpc.Request {
	return func(call statsCall[C]) gossiprpc.Request {
		return accountRequest(call, linkedTarget[C](preferUUID)(call))
	}
}
