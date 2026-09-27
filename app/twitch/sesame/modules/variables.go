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
		var cfg C
		if err := c.Decode(&cfg); err != nil && len(c.Config) > 0 {
			return nil, err
		}
		req := request(statsCall[C]{Ctx: c, Cfg: cfg})
		if req.Account == "" && route.Endpoint != "shop" && route.Endpoint != "leaderboard" {
			return nil, nil
		}
		if route.Endpoint == "versus" && req.AccountB == "" {
			return nil, nil
		}
		var r R
		if err := d.Gossip.Call(ctx, route, req, &r); err != nil {
			return nil, err
		}
		// All provider replies share these envelope/empty-state fields. Decode
		// through the fleet codec rather than guessing from zero-valued stats.
		raw, err := codec.Marshal(r)
		if err != nil {
			return nil, err
		}
		var state struct {
			Error       string `json:"error"`
			Empty       bool   `json:"empty"`
			Unranked    bool   `json:"unranked"`
			HasSnapshot *bool  `json:"has_snapshot"`
		}
		if err := codec.Unmarshal(raw, &state); err != nil {
			return nil, err
		}
		if state.Error != "" {
			return nil, errors.New(state.Error)
		}
		values := palette(c, &r)
		if state.Empty || state.Unranked || state.HasSnapshot != nil && !*state.HasSnapshot {
			for field := range values {
				if field != "player" && field != "region" && field != "tag" {
					delete(values, field)
				}
			}
			if state.Unranked && route.Provider == "valorant" {
				values["tier"] = "Unranked"
			}
		}
		return values, nil
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
