// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"context"
	"fmt"
	"strconv"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/internal/activity"
	"ItsBagelBot/internal/domain/i18n"
)

type activityObserver struct{}

func (activityObserver) Observe(ev engine.ObservedEvent) {
	if !ev.Handled || ev.Command == "" {
		return
	}
	row := activity.Row{
		Kind:       activity.KindCommand,
		Text:       fmt.Sprintf(i18n.T(ev.Locale, "activity.command.answered"), ev.Command, ev.Actor),
		Meta:       strconv.Itoa(ev.DurationMS) + "ms",
		At:         ev.At,
		DurationMS: ev.DurationMS,
	}
	activity.Emit(context.Background(), strconv.FormatUint(ev.BroadcasterID, 10), row)
}
