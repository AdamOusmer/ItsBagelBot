// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package overlaysrpc

import (
	"errors"
	"slices"

	"ItsBagelBot/internal/domain/rpc"
)

const (
	PoweredGetSubject    = "rpc.overlays.powered.get"
	PoweredSaveSubject   = "rpc.overlays.powered.save"
	PoweredRotateSubject = "rpc.overlays.powered.rotate"

	OwnerPoweredSubject        = "rpc.internal.overlays.powered.get"
	OwnerPoweredByTokenSubject = "rpc.internal.overlays.powered.by_token"

	PoweredResolveSubject = "rpc.internal.projector.overlays.powered.resolve"

	PoweredURLPrefix = "https://overlay.itsbagelbot.com/powered#"
)

const (
	MinEveryMinutes     = 5
	MaxEveryMinutes     = 120
	DefaultEveryMinutes = 15
	MinShowSeconds      = 6
	MaxShowSeconds      = 20
	DefaultShowSeconds  = 8
)

type Direction string

const (
	DirectionTop    Direction = "top"
	DirectionBottom Direction = "bottom"
	DirectionLeft   Direction = "left"
	DirectionRight  Direction = "right"
)

var Directions = []Direction{DirectionTop, DirectionBottom, DirectionLeft, DirectionRight}

type Align string

const (
	AlignStart  Align = "start"
	AlignCenter Align = "center"
	AlignEnd    Align = "end"
)

var Aligns = []Align{AlignStart, AlignCenter, AlignEnd}

type PoweredConfig struct {
	EveryMinutes int         `json:"every_minutes"`
	ShowSeconds  int         `json:"show_seconds"`
	Directions   []Direction `json:"directions"`
	Align        Align       `json:"align"`
}

func DefaultPoweredConfig() PoweredConfig {
	return PoweredConfig{
		EveryMinutes: DefaultEveryMinutes,
		ShowSeconds:  DefaultShowSeconds,
		Directions:   slices.Clone(Directions),
		Align:        AlignEnd,
	}
}

var (
	ErrEveryRange = errors.New("every_minutes out of range")
	ErrShowRange  = errors.New("show_seconds out of range")
	ErrDirections = errors.New("directions must be a non-empty set of top, bottom, left, right")
	ErrAlign      = errors.New("align must be start, center or end")
)

func (c PoweredConfig) Validate() error {
	if c.EveryMinutes < MinEveryMinutes || c.EveryMinutes > MaxEveryMinutes {
		return ErrEveryRange
	}
	if c.ShowSeconds < MinShowSeconds || c.ShowSeconds > MaxShowSeconds {
		return ErrShowRange
	}
	if !validDirections(c.Directions) {
		return ErrDirections
	}
	if !slices.Contains(Aligns, c.Align) {
		return ErrAlign
	}
	return nil
}

func validDirections(list []Direction) bool {
	if len(list) == 0 || len(list) > len(Directions) {
		return false
	}
	seen := make(map[Direction]bool, len(list))
	for _, d := range list {
		if seen[d] || !slices.Contains(Directions, d) {
			return false
		}
		seen[d] = true
	}
	return true
}

type PoweredOverlay struct {
	UserID   uint64        `json:"user_id"`
	Token    string        `json:"token"`
	Config   PoweredConfig `json:"config"`
	Revision uint64        `json:"revision"`
}

type PoweredGetRequest struct {
	UserID string `json:"user_id"`
}

type PoweredView struct {
	URL      string        `json:"url"`
	Config   PoweredConfig `json:"config"`
	Revision uint64        `json:"revision"`
	rpc.Refusal
}

type PoweredSaveRequest struct {
	UserID   string        `json:"user_id"`
	Config   PoweredConfig `json:"config"`
	Revision uint64        `json:"revision"`
}

type PoweredRotateRequest struct {
	UserID string `json:"user_id"`
}

type OwnerPoweredRequest struct {
	UserID uint64 `json:"user_id"`
}

type OwnerPoweredByTokenRequest struct {
	Token string `json:"token"`
}

type OwnerPoweredReply struct {
	Overlay *PoweredOverlay `json:"overlay,omitempty"`
	rpc.Refusal
}

type PoweredResolveRequest struct {
	Token string `json:"token"`
}

type PoweredResolveReply struct {
	Config  PoweredConfig `json:"config"`
	Premium bool          `json:"premium"`
	rpc.Refusal
}
