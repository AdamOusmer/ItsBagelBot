// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package scope

import (
	"context"
	"strconv"
	"time"

	"ItsBagelBot/internal/domain/i18n"
)

const (
	UptimeToken    = "uptime"
	TitleToken     = "title"
	GameToken      = "game"
	ViewersToken   = "channel.viewers"
	FollowersToken = "followers"
	SubsToken      = "subs"
)

const MaxChannelLogins = 3

type Stream struct {
	UserFound   bool
	Live        bool
	Title       string
	GameName    string
	ViewerCount int
	StartedAt   time.Time
}

type Streams interface {
	Stream(ctx context.Context, login string) Stream
}

type ChannelCountsResult struct {
	Followers   int
	FollowersOK bool
	Subs        int
	SubsOK      bool
}

type ChannelCounts interface {
	Counts(ctx context.Context) ChannelCountsResult
}

type Channel struct {
	Locale   string
	Streams  Streams
	OwnLogin string
	Uptime   bool
	Title    bool
	Game     bool
	Viewers  bool
	Now      func() time.Time
	Counts   ChannelCounts
}

func (c Channel) Owns(v Var) bool {
	name := v.Name
	if name == FollowersToken || name == SubsToken {
		return c.Counts != nil
	}
	if c.Streams == nil {
		return false
	}
	switch v.Name {
	case UptimeToken:
		return c.Uptime
	case TitleToken:
		return c.Title
	case GameToken:
		return c.Game
	case ViewersToken:
		return c.Viewers
	}
	return false
}

func (c Channel) Plan(ctx context.Context, wants []Var) (Values, error) {
	out := &channelValues{
		locale: c.Locale, now: c.Now, ownLogin: c.OwnLogin,
		streams: make(map[string]Stream, len(wants)),
	}
	for _, want := range wants {
		c.planOne(ctx, out, want)
	}
	if out.wantCounts && c.Counts != nil {
		out.counts = c.Counts.Counts(ctx)
	}
	return out, nil
}

func (c Channel) planOne(ctx context.Context, out *channelValues, want Var) {
	if want.Name == FollowersToken || want.Name == SubsToken {
		out.wantCounts = out.wantCounts || !want.HasPayload
		return
	}
	login, ok := out.loginOf(want)
	if !ok {
		return
	}
	if _, done := out.streams[login]; done {
		return
	}
	if !out.admit(login) {
		return
	}
	out.streams[login] = c.Streams.Stream(ctx, login)
}

func (v *channelValues) loginOf(tok Var) (string, bool) {
	if !tok.HasPayload {
		return "", true
	}
	if tok.Name == ViewersToken {
		return "", false
	}
	login := normalizeLogin(tok.Payload)
	if login == "" {
		return "", false
	}
	if login == v.ownLogin {
		return "", true
	}
	return login, true
}

type channelValues struct {
	locale     string
	ownLogin   string
	now        func() time.Time
	streams    map[string]Stream
	named      int
	wantCounts bool
	counts     ChannelCountsResult
}

func (v *channelValues) admit(login string) bool {
	if login == "" {
		return true
	}
	if v.named >= MaxChannelLogins {
		return false
	}
	v.named++
	return true
}

func (v *channelValues) Get(tok Var) (string, bool) {
	if tok.Name == FollowersToken || tok.Name == SubsToken {
		return v.count(tok)
	}
	login, ok := v.loginOf(tok)
	if !ok {
		return "", false
	}
	return v.render(tok.Name, v.streams[login]), true
}

func (v *channelValues) count(tok Var) (string, bool) {
	if tok.HasPayload {
		return "", false
	}
	if tok.Name == FollowersToken {
		if !v.counts.FollowersOK {
			return "", false
		}
		return strconv.Itoa(v.counts.Followers), true
	}
	if !v.counts.SubsOK {
		return "", false
	}
	return strconv.Itoa(v.counts.Subs), true
}

func (v *channelValues) render(name string, s Stream) string {
	if !s.UserFound {
		return ""
	}
	switch name {
	case TitleToken:
		return s.Title
	case GameToken:
		return s.GameName
	case ViewersToken:
		return viewerCount(s)
	}
	return v.uptime(s)
}

func viewerCount(s Stream) string {
	if !s.Live {
		return "0"
	}
	return strconv.Itoa(s.ViewerCount)
}

func (v *channelValues) uptime(s Stream) string {
	if !s.Live {
		return ""
	}
	return i18n.HumanizeDuration(v.locale, v.clock().Sub(s.StartedAt))
}

func (v *channelValues) clock() time.Time {
	if v.now != nil {
		return v.now()
	}
	return time.Now()
}
