// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/i18n"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
	"context"
	"strconv"
	"strings"
	"time"
)

func localVariableReaders(d engine.Deps, name string) map[string]variableRead {
	switch name {
	case "time":
		return map[string]variableRead{"time": timeVariables, "lookup": timeVariables}
	case "triggers":
		return map[string]variableRead{"response": func(ctx context.Context, c *module.Context) (map[string]string, error) {
			values, _ := commandContextVariables(ctx, c)
			values["channel"] = c.Env.BroadcasterName()
			return values, nil
		}}
	case "personality":
		return map[string]variableRead{"reply": func(_ context.Context, c *module.Context) (map[string]string, error) {
			return map[string]string{"user": strings.TrimPrefix(c.Env.ChatterName(), "@")}, nil
		}}
	case "songqueue":
		return map[string]variableRead{"current": songVariables(d)}
	case "loyalty":
		return map[string]variableRead{"balance": loyaltyVariables(d)}
	case "quotes":
		return map[string]variableRead{"quote": quoteVariables(d)}
	case "stream":
		return map[string]variableRead{"channel": streamVariables(d)}
	case "queue":
		return map[string]variableRead{"status": queueVariables(d)}
	case "raffle":
		return map[string]variableRead{"status": raffleVariables(d)}
	case "followage", "accountage":
		read := viewerAgeVariables(d, name)
		return map[string]variableRead{"reply": commandContextVariables, "status": read}
	case "uptime":
		return map[string]variableRead{"reply": uptimeVariables(d)}
	case "title", "game":
		return map[string]variableRead{"reply": channelCommandVariables(d)}
	case "clip", "tags", "commercial", "marker":
		return map[string]variableRead{"reply": commandContextVariables}
	default:
		return nil // These palettes describe events, not stored facts.
	}
}

// Values tied to a newly-created clip, channel edit or commercial cannot be
// read from a custom command. Shared chatter fields are available without
// performing any action, and the event-specific fields resolve empty.
func commandContextVariables(_ context.Context, c *module.Context) (map[string]string, error) {
	name := strings.TrimPrefix(c.Env.ChatterName(), "@")
	return map[string]string{"user": name, "target": name}, nil
}

func viewerAgeVariables(d engine.Deps, name string) variableRead {
	return func(ctx context.Context, c *module.Context) (map[string]string, error) {
		values, _ := commandContextVariables(ctx, c)
		if name == "followage" && d.Followage != nil {
			result, err := d.Followage.Lookup(ctx, c.Env.BroadcasterUserID, c.Env.ChatterUserID, c.Env.ChatterUserLogin)
			if err != nil {
				return nil, err
			}
			if result.UserFound && result.Following && !result.FollowedAt.IsZero() {
				values["followage"] = i18n.HumanizeDuration(c.Locale, time.Since(result.FollowedAt))
				values["followedat"] = result.FollowedAt.UTC().Format(time.RFC3339)
			}
		}
		if name == "accountage" && d.AccountAge != nil {
			result, err := d.AccountAge.Lookup(ctx, c.Env.ChatterUserID, c.Env.ChatterUserLogin)
			if err != nil {
				return nil, err
			}
			if result.UserFound && !result.CreatedAt.IsZero() {
				values["accountage"] = i18n.HumanizeDuration(c.Locale, time.Since(result.CreatedAt))
				values["createdat"] = result.CreatedAt.UTC().Format(time.RFC3339)
			}
		}
		return values, nil
	}
}

func uptimeVariables(d engine.Deps) variableRead {
	return func(ctx context.Context, c *module.Context) (map[string]string, error) {
		if d.Uptime == nil {
			return nil, nil
		}
		result, err := d.Uptime.Lookup(ctx, c.Env.BroadcasterUserID)
		if err != nil || !result.Live || result.StartedAt.IsZero() {
			return nil, err
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
	return map[string]string{"time": engine.FormatClock(now, cfg.Format), "date": now.Format("Monday, January 2"), "timezone": zone.String(), "place": zone.String(), "user": strings.TrimPrefix(c.Env.ChatterName(), "@")}, nil
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
		var r gossiprpc.SpotifyNowPlayingReply
		if d.Gossip != nil {
			if err := d.Gossip.Call(ctx, engine.GossipRoute{Provider: "spotify", Endpoint: "nowplaying"}, gossiprpc.Request{ChannelID: strconv.FormatUint(c.BroadcasterID, 10)}, &r); err != nil {
				return nil, err
			}
			if r.Error != "" {
				return nil, nil
			}
		}
		var current *engine.SongEntry
		if d.SongQueue != nil {
			snapshot, err := d.SongQueue.Snapshot(ctx, c.BroadcasterID, 0)
			if err != nil && (!r.IsPlaying || r.Track == nil) {
				return nil, err
			}
			current = snapshot.Current
		}
		if r.IsPlaying && r.Track != nil {
			requester := ""
			if current != nil && current.TrackID == r.Track.ID {
				requester = current.RequesterName
			}
			return songVariableValues(r.Track.Name, r.Track.Artists, r.Track.URL, requester), nil
		}
		if current != nil {
			return songVariableValues(current.Title, current.Artists, current.URL, current.RequesterName), nil
		}
		return nil, nil
	}
}

func songVariableValues(title string, artists []string, url, requester string) map[string]string {
	artist := strings.Join(artists, ", ")
	song := title
	if artist != "" {
		song += " by " + artist
	}
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
		duration := time.Duration(1<<63 - 1)
		if balance.WatchSeconds <= uint64((1<<63-1)/time.Second) {
			duration = time.Duration(balance.WatchSeconds) * time.Second
		}
		watchtime := i18n.HumanizeDuration(c.Locale, duration)
		return map[string]string{
			"points": i64(balance.Points), "pointsname": cfg.Name(), "watchtime": watchtime,
			"duration": watchtime, "user": c.Env.ChatterUserLogin, "name": cfg.Name(),
			"hours": strconv.FormatFloat(float64(balance.WatchSeconds)/3600, 'f', 1, 64),
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
		date := ""
		if created, err := time.Parse(time.RFC3339, quote.CreatedAt); err == nil {
			date = created.UTC().Format("2006-01-02")
		}
		values := module.StringPalette{"num": strconv.FormatUint(quote.Number, 10), "text": quote.Text, "date": date}
		values["quote"] = values.ExpandNamespaced("quotes", i18n.T(c.Locale, "quote.show"))
		return values, nil
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
