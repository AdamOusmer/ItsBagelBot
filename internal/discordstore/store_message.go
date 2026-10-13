// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discordstore

import (
	"context"
	"time"

	pkg_valkey "ItsBagelBot/pkg/valkey"
)

const (
	messageCacheTTL = time.Hour
	factCacheTTL    = 24 * time.Hour
	messageMaxRunes = 1024
)

type Message struct{ ID string }

type CachedMessage struct {
	ID          string   `json:"id"`
	GuildID     string   `json:"guildId"`
	ChannelID   string   `json:"channelId"`
	AuthorID    string   `json:"authorId"`
	AuthorName  string   `json:"authorName"`
	Bot         bool     `json:"bot,omitempty"`
	Content     string   `json:"content"`
	Attachments []string `json:"attachments,omitempty"`
}

type MemberRoles struct {
	Member Member
	Roles  []string
}

type LabelKind string

const (
	LabelRole  LabelKind = "role"
	LabelGuild LabelKind = "guildname"
	LabelNick  LabelKind = "nick"
	LabelChan  LabelKind = "channel"
)

type LabelRef struct {
	Kind    LabelKind
	GuildID string
	ID      string
}

type Label struct {
	Ref  LabelRef
	Name string
}

func messageKey(m Message) string { return "discord:msg:" + m.ID }

func memberRolesKey(m Member) string { return "discord:mroles:" + m.key() }

func labelKey(r LabelRef) string { return "discord:" + string(r.Kind) + ":" + r.GuildID + ":" + r.ID }

func clipRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

func prepareMessage(msg CachedMessage) CachedMessage {
	msg.Content = clipRunes(msg.Content, messageMaxRunes)
	return msg
}

func (s valkeyStore) RememberMessage(ctx context.Context, msg CachedMessage) error {
	if msg.ID == "" {
		return nil
	}
	at := pkg_valkey.Key{Name: messageKey(Message{ID: msg.ID}), TTL: messageCacheTTL}
	return pkg_valkey.SetJSON(ctx, s.kv(), at, prepareMessage(msg))
}

func (s valkeyStore) RecallMessage(ctx context.Context, m Message) (CachedMessage, bool) {
	return pkg_valkey.GetJSON[CachedMessage](ctx, s.kv(), messageKey(m))
}

func (s valkeyStore) RememberRoles(ctx context.Context, r MemberRoles) error {
	at := pkg_valkey.Key{Name: memberRolesKey(r.Member), TTL: factCacheTTL}
	return pkg_valkey.SetJSON(ctx, s.kv(), at, r.Roles)
}

func (s valkeyStore) RecallRoles(ctx context.Context, m Member) ([]string, bool) {
	return pkg_valkey.GetJSON[[]string](ctx, s.kv(), memberRolesKey(m))
}

func (s valkeyStore) RememberLabel(ctx context.Context, l Label) error {
	return s.kv().Set(ctx, pkg_valkey.Key{Name: labelKey(l.Ref), TTL: factCacheTTL}, l.Name)
}

func (s valkeyStore) RecallLabel(ctx context.Context, r LabelRef) (string, bool) {
	return s.kv().GetString(ctx, labelKey(r))
}

func (m *Mem) RememberMessage(_ context.Context, msg CachedMessage) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages[msg.ID] = prepareMessage(msg)
	return nil
}

func (m *Mem) RecallMessage(_ context.Context, msg Message) (CachedMessage, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	got, ok := m.messages[msg.ID]
	return got, ok
}

func (m *Mem) RememberRoles(_ context.Context, r MemberRoles) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.memberRoles[r.Member.key()] = r.Roles
	return nil
}

func (m *Mem) RecallRoles(_ context.Context, mem Member) ([]string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	got, ok := m.memberRoles[mem.key()]
	return got, ok
}

func (m *Mem) RememberLabel(_ context.Context, l Label) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.labels[labelKey(l.Ref)] = l.Name
	return nil
}

func (m *Mem) RecallLabel(_ context.Context, r LabelRef) (string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	got, ok := m.labels[labelKey(r)]
	return got, ok
}
