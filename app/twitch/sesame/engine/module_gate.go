// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"

	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/projection"

	"go.uber.org/zap"
)

type ModuleState uint8

const (
	ModuleUnavailable ModuleState = iota
	ModuleOff
	ModuleOn
)

type ModuleLookup struct {
	Proj          projection.Reader
	BroadcasterID uint64
	Name          string
	Absent        ModuleState
}

func (l ModuleLookup) Resolve(ctx context.Context) (projection.ModuleView, ModuleState, error) {
	if l.Proj == nil {
		return projection.ModuleView{}, ModuleUnavailable, nil
	}
	view, ok, err := l.Proj.Module(ctx, l.BroadcasterID, l.Name)
	if err != nil {
		return projection.ModuleView{}, ModuleUnavailable, err
	}
	if !ok {
		return projection.ModuleView{}, l.Absent, nil
	}
	if !view.IsEnabled {
		return view, ModuleOff, nil
	}
	return view, ModuleOn, nil
}

type ModuleGate struct {
	Proj          projection.Reader
	Log           *zap.Logger
	BroadcasterID uint64
	Name          string
}

func (g ModuleGate) BuiltinEnabled(ctx context.Context) bool {
	_, state, err := ModuleLookup{Proj: g.Proj, BroadcasterID: g.BroadcasterID, Name: g.Name, Absent: ModuleOn}.Resolve(ctx)
	if err != nil && g.Log != nil {
		g.Log.Warn(g.Name+": module state read failed, allowing", module.BIDField(g.BroadcasterID), zap.Error(err))
	}
	return state != ModuleOff
}

func (g ModuleGate) OptInView(ctx context.Context) (projection.ModuleView, bool) {
	view, state, err := ModuleLookup{Proj: g.Proj, BroadcasterID: g.BroadcasterID, Name: g.Name, Absent: ModuleOff}.Resolve(ctx)
	if err != nil && g.Log != nil {
		g.Log.Warn(g.Name+": module state read failed, token stays literal", module.BIDField(g.BroadcasterID), zap.Error(err))
	}
	return view, state == ModuleOn
}
