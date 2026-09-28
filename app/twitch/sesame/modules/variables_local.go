// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/engine/scope"
	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/i18n"
	"context"
	"strconv"
	"strings"
	"time"
)

type variableReaderFactory func(engine.Deps) variableRead

var localVariableGroups = map[string]map[string]variableReaderFactory{
	"time":        {"time": fixedVariableReader(timeVariables), "lookup": fixedVariableReader(timeVariables)},
	"triggers":    {"response": fixedVariableReader(triggerContextVariables)},
	"personality": {"reply": fixedVariableReader(personalityContextVariables)},
	"songqueue":   {"current": songVariables},
	"loyalty":     {"balance": loyaltyVariables},
	"quotes":      {"quote": quoteVariables},
	"stream":      {"channel": streamVariables},
	"queue":       {"status": queueVariables},
	"raffle":      {"status": raffleVariables},
	"followage":   {"reply": fixedVariableReader(commandContextVariables), "status": followageVariables},
	"accountage":  {"reply": fixedVariableReader(commandContextVariables), "status": accountageVariables},
	"uptime":      {"reply": uptimeVariables},
	"title":       {"reply": channelCommandVariables},
	"game":        {"reply": channelCommandVariables},
	"clip":        {"reply": fixedVariableReader(commandContextVariables)},
	"tags":        {"reply": fixedVariableReader(commandContextVariables)},
	"commercial":  {"reply": fixedVariableReader(commandContextVariables)},
	"marker":      {"reply": fixedVariableReader(commandContextVariables)},
}

func localVariableReaders(d engine.Deps, name string) map[string]variableRead {
	groups := localVariableGroups[name]
	if groups == nil {
		return nil
	} // Event palettes have no stored facts.
	readers := make(map[string]variableRead, len(groups))
	for group, factory := range groups {
		readers[group] = factory(d)
	}
	return readers
}

func fixedVariableReader(read variableRead) variableReaderFactory {
	return func(engine.Deps) variableRead { return read }
}

func triggerContextVariables(ctx context.Context, c *module.Context) (map[string]string, error) {
	values, _ := commandContextVariables(ctx, c)
	values["channel"] = c.Env.BroadcasterName()
	return values, nil
}

func personalityContextVariables(_ context.Context, c *module.Context) (map[string]string, error) {
	return map[string]string{"user": strings.TrimPrefix(c.Env.ChatterName(), "@")}, nil
}

// Values tied to a newly-created clip, channel edit or commercial cannot be
// read from a custom command. Shared chatter fields are available without
// performing any action, and the event-specific fields resolve empty.
func commandContextVariables(_ context.Context, c *module.Context) (map[string]string, error) {
	name := strings.TrimPrefix(c.Env.ChatterName(), "@")
	return map[string]string{"user": name, "target": name}, nil
}

func followageVariables(d engine.Deps) variableRead {
	return func(ctx context.Context, c *module.Context) (map[string]string, error) {
		values, _ := commandContextVariables(ctx, c)
		if d.Followage == nil {
			return values, nil
		}
		result, err := d.Followage.Lookup(ctx, c.Env.BroadcasterUserID, c.Env.ChatterUserID, c.Env.ChatterUserLogin)
		if err != nil {
			return nil, err
		}
		if !result.UserFound {
			return values, nil
		}
		if !result.Following {
			return values, nil
		}
		addAgeVariables(values, c, result.FollowedAt, ageVariableFields{duration: "followage", date: "followedat"})
		return values, nil
	}
}

func accountageVariables(d engine.Deps) variableRead {
	return func(ctx context.Context, c *module.Context) (map[string]string, error) {
		values, _ := commandContextVariables(ctx, c)
		if d.AccountAge == nil {
			return values, nil
		}
		result, err := d.AccountAge.Lookup(ctx, c.Env.ChatterUserID, c.Env.ChatterUserLogin)
		if err != nil {
			return nil, err
		}
		if !result.UserFound {
			return values, nil
		}
		addAgeVariables(values, c, result.CreatedAt, ageVariableFields{duration: "accountage", date: "createdat"})
		return values, nil
	}
}

type ageVariableFields struct{ duration, date string }

func addAgeVariables(values map[string]string, c *module.Context, at time.Time, fields ageVariableFields) {
	if at.IsZero() {
		return
	}
	values[fields.duration] = i18n.HumanizeDuration(c.Locale, time.Since(at))
	values[fields.date] = at.UTC().Format(time.RFC3339)
}

func uptimeVariables(d engine.Deps) variableRead {
	return func(ctx context.Context, c *module.Context) (map[string]string, error) {
		if d.Uptime == nil {
			return nil, nil
		}
		result, err := d.Uptime.Lookup(ctx, c.Env.BroadcasterUserID)
		if err != nil {
			return nil, err
		}
		if !result.Live {
			return nil, nil
		}
		if result.StartedAt.IsZero() {
			return nil, nil
		}
		return map[string]string{"uptime": i18n.HumanizeDuration(c.Locale, time.Since(result.StartedAt))}, nil
	}
}

func channelCommandVariables(d engine.Deps) variableRead {
	return func(ctx context.Context, c *module.Context) (map[string]string, error) {
		values, _ := commandContextVariables(ctx, c)
		channel, err := streamVariables(d)(ctx, c)
		if err != nil {
			return nil, err
		}
		for key, value := range channel {
			values[key] = value
		}
		return values, nil
	}
}

func timeVariables(_ context.Context, c *module.Context) (map[string]string, error) {
	var cfg engine.TimeModuleConfig
	_ = c.Decode(&cfg)
	zone, ok := cfg.Zone()
	if !ok || zone == nil {
		return nil, nil
	}
	now := time.Now().In(zone)
	return map[string]string{"time": engine.FormatClock(now, cfg.Format), "date": now.Format(timeDateLayout), "timezone": zone.String(), "place": zone.String(), "user": strings.TrimPrefix(c.Env.ChatterName(), "@")}, nil
}

func queueVariables(d engine.Deps) variableRead {
	return func(ctx context.Context, c *module.Context) (map[string]string, error) {
		if d.Queue == nil {
			return nil, nil
		}
		entries, size, err := d.Queue.List(ctx, c.BroadcasterID, 10)
		if err != nil {
			return nil, err
		}
		open, err := d.Queue.IsOpen(ctx, c.BroadcasterID)
		if err != nil {
			return nil, err
		}
		return map[string]string{"size": i64(size), "entries": strings.Join(entries, ", "), "open": strconv.FormatBool(open)}, nil
	}
}

func raffleVariables(d engine.Deps) variableRead {
	return func(ctx context.Context, c *module.Context) (map[string]string, error) {
		if d.Raffle == nil {
			return nil, nil
		}
		status, err := d.Raffle.Status(ctx, c.BroadcasterID)
		if err != nil {
			return nil, err
		}
		return map[string]string{"entrants": i64(status.Entrants), "seconds": i64(max(0, status.SecondsLeft)), "open": strconv.FormatBool(status.Open)}, nil
	}
}

func songVariables(d engine.Deps) variableRead {
	return func(ctx context.Context, c *module.Context) (map[string]string, error) {
		qc, ok := newSongQueueCmd(d, c, songQueueLog(d))
		if !ok {
			return nil, nil
		}
		return qc.nowPlayingVariables(ctx)
	}
}

// A failure resolves empty: chat failure text must never land in a custom command reply.
func (qc songQueueCmd) nowPlayingVariables(ctx context.Context) (map[string]string, error) {
	player, failure := qc.readPlayer(ctx)
	if failure != "" {
		return nil, nil
	}
	if track := player.track; track != nil {
		return songVariableValues(track.Name, track.Artists, track.URL, qc.requesterOf(ctx, track.ID)), nil
	}
	snap, err := qc.store.Snapshot(ctx, qc.c.BroadcasterID, songqueueListLen)
	if err != nil {
		return nil, err
	}
	if queued := snap.Current; queued != nil {
		return songVariableValues(queued.Title, queued.Artists, queued.URL, queued.RequesterName), nil
	}
	return nil, nil
}

func songVariableValues(title string, artists []string, url, requester string) map[string]string {
	artist := strings.Join(artists, ", ")
	song := scope.Track{Title: title, Artist: artist}.Line()
	return map[string]string{"song": song, "title": title, "artist": artist, "url": url, "req": requester}
}

func loyaltyVariables(d engine.Deps) variableRead {
	return func(ctx context.Context, c *module.Context) (map[string]string, error) {
		if d.Loyalty == nil {
			return nil, nil
		}
		id, err := strconv.ParseUint(c.Env.ChatterUserID, 10, 64)
		if err != nil {
			return nil, err
		}
		balance, err := d.Loyalty.BalanceGet(ctx, c.BroadcasterID, id)
		if err != nil {
			return nil, err
		}
		var cfg engine.LoyaltyModuleConfig
		_ = c.Decode(&cfg)
		watchtime := watchTime(c.Locale, balance.WatchSeconds)
		return map[string]string{
			"points": i64(balance.Points), "pointsname": cfg.Name(), "watchtime": watchtime,
			"duration": watchtime, "user": c.Env.ChatterUserLogin, "name": cfg.Name(),
			"hours": watchHours(balance.WatchSeconds),
		}, nil
	}
}

func quoteVariables(d engine.Deps) variableRead {
	return func(ctx context.Context, c *module.Context) (map[string]string, error) {
		if d.Quotes == nil {
			return nil, nil
		}
		quote, found, err := d.Quotes.QuoteRandom(ctx, c.BroadcasterID)
		if err != nil || !found {
			return nil, err
		}
		return map[string]string{
			"num": strconv.FormatUint(quote.Number, 10), "text": quote.Text, "date": engine.QuoteDate(quote.CreatedAt),
			"quote": engine.QuoteLine(c.Locale, quote),
		}, nil
	}
}

func streamVariables(d engine.Deps) variableRead {
	return func(ctx context.Context, c *module.Context) (map[string]string, error) {
		if d.StreamInfo == nil {
			return nil, nil
		}
		info, err := d.StreamInfo.Lookup(ctx, c.Env.BroadcasterUserID, "")
		if err != nil || !info.UserFound {
			return nil, err
		}
		values := map[string]string{"title": info.Title, "game": info.GameName, "viewers": "0"}
		if info.Live {
			values["viewers"] = strconv.Itoa(info.ViewerCount)
			values["uptime"] = i18n.HumanizeDuration(c.Locale, time.Since(info.StartedAt))
		}
		return values, nil
	}
}
