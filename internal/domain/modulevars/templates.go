// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modulevars

import (
	"strings"

	"ItsBagelBot/pkg/codec"
	"ItsBagelBot/pkg/tmpl"
)

// NamespaceTemplate converts only a module's known, unqualified fields. It
// retains literal text, unknown variables, dynamic payloads and fallbacks.
func NamespaceTemplate(moduleID string, fields []string, text string) string {
	namespace := newTemplateNamespace(replyPalette{moduleID: moduleID, fields: fields})
	return replyTemplate{namespace: namespace, text: text}.rewrite()
}

// replyPalette fixes the public fields a saved reply is allowed to rewrite.
type replyPalette struct {
	moduleID string
	fields   []string
}

type templateNamespace struct {
	moduleID string
	fields   map[string]bool
}

func newTemplateNamespace(palette replyPalette) templateNamespace {
	namespace := templateNamespace{moduleID: palette.moduleID, fields: make(map[string]bool, len(palette.fields))}
	for _, field := range palette.fields {
		namespace.fields[strings.ToLower(field)] = true
	}
	return namespace
}

type replyTemplate struct {
	namespace templateNamespace
	text      string
}

func (r replyTemplate) rewrite() string {
	var out strings.Builder
	for _, token := range tmpl.Lex(r.text) {
		out.WriteString(r.namespace.rewrite(token))
	}
	return out.String()
}

func (n templateNamespace) rewrite(token tmpl.Token) string {
	if token.Kind == tmpl.KindLiteral {
		return token.Text
	}
	if condition, ok := token.Cond(); ok {
		return n.rewriteCondition(token, condition)
	}
	return n.rewriteVariable(token)
}

func (n templateNamespace) rewriteVariable(token tmpl.Token) string {
	if token.HasPayload || !n.fields[token.Name] {
		return token.Raw
	}
	converted := "{" + n.moduleID + ":" + token.Name
	if token.HasFallback {
		converted += "|" + token.Fallback
	}
	return converted + "}"
}

func (n templateNamespace) rewriteCondition(token tmpl.Token, condition tmpl.Cond) string {
	if token.HasInnerBrace() {
		return token.Raw
	}
	if condition.Ref.HasPayload || !n.fields[condition.Ref.Name] {
		return token.Raw
	}
	start := strings.IndexByte(token.Raw, ':') + 1
	end := start + len(condition.Ref.Name)
	converted := token.Raw[:start] + n.moduleID + ":" + condition.Ref.Name + token.Raw[end:]
	return preserveConditionalElse(conditionalRewrite{token: token, converted: converted})
}

type conditionalRewrite struct {
	token     tmpl.Token
	converted string
}

func preserveConditionalElse(rewrite conditionalRewrite) string {
	// A namespaced reference consumes an extra ':' segment. A single then
	// branch therefore needs an explicit empty else to preserve its meaning.
	if strings.Count(rewrite.token.Payload, ":") != 1 {
		return rewrite.converted
	}
	at := len(rewrite.converted) - 1
	if rewrite.token.HasFallback {
		at = strings.LastIndexByte(rewrite.converted, '|')
	}
	return rewrite.converted[:at] + ":" + rewrite.converted[at:]
}

// MigrateConfig returns only changed top-level config keys, suitable for the
// modules.patch-existing RPC. Ownership comes from the public reply catalogue;
// arbitrary custom-command responses have no inferred module ownership.
func MigrateConfig(moduleID string, raw []byte) (map[string]codec.RawMessage, error) {
	if len(strings.TrimSpace(string(raw))) == 0 {
		return nil, nil
	}
	var config map[string]codec.RawMessage
	if err := codec.Unmarshal(raw, &config); err != nil {
		return nil, err
	}
	migration := newConfigMigration(savedConfig{owner: Module{ID: moduleID}, values: config})
	if len(migration.fields) == 0 {
		return nil, nil
	}
	migration.migrateReplyGroups()
	if err := migration.migrateAdditionalReplies(); err != nil {
		return nil, err
	}
	if len(migration.patch) == 0 {
		return nil, nil
	}
	return migration.patch, nil
}

// savedConfig binds catalogue ownership to the complete persisted values.
type savedConfig struct {
	owner  Module
	values map[string]codec.RawMessage
}

type configMigration struct {
	owner         Module
	config, patch map[string]codec.RawMessage
	fields        []string
}

func newConfigMigration(saved savedConfig) configMigration {
	migration := configMigration{owner: saved.owner, config: saved.values, patch: make(map[string]codec.RawMessage)}
	migration.owner.Groups = replyGroups(saved.owner)
	for _, group := range migration.owner.Groups {
		migration.fields = append(migration.fields, group.Fields...)
	}
	return migration
}

func replyGroups(owner Module) []Group {
	for _, module := range Catalog() {
		if module.ID == owner.ID {
			return module.Groups
		}
	}
	return nil
}

func (m configMigration) migrateReplyGroups() {
	for _, group := range m.owner.Groups {
		if group.MessageKey != "" {
			m.reply(group).migrateString()
		}
	}
}

// replyMigration owns the exact palette, saved key, and partial patch target.
// Nested objects can reuse the same definition with their own value maps.
type replyMigration struct {
	namespace     templateNamespace
	key           string
	config, patch map[string]codec.RawMessage
}

func (m configMigration) reply(group Group) replyMigration {
	palette := replyPalette{moduleID: m.owner.ID, fields: group.Fields}
	return replyMigration{namespace: newTemplateNamespace(palette), key: group.MessageKey, config: m.config, patch: m.patch}
}

func (m configMigration) migrateAdditionalReplies() error {
	switch m.owner.ID {
	case "govee":
		m.reply(Group{MessageKey: "replyMessage", Fields: m.fields}).migrateString()
		return m.migrateReplyArray(replyCollection{key: "bindings", group: Group{MessageKey: "replyMessage", Fields: m.fields}})
	case "channelpoints":
		return m.migrateReplyArray(replyCollection{key: "rewards", group: Group{MessageKey: "message", Fields: m.fields}})
	case "queue":
		m.reply(Group{MessageKey: "openedMessage", Fields: []string{"user"}}).migrateString()
		m.reply(Group{MessageKey: "closedMessage", Fields: []string{"user"}}).migrateString()
	case "songqueue":
		return m.migrateSongqueueReplies()
	case "triggers":
		return m.migrateTriggerReplies()
	}
	return nil
}

type replyCollection struct {
	key   string
	group Group
}

func (m configMigration) migrateReplyArray(collection replyCollection) error {
	plan := replyArrayMigration{reply: m.reply(collection.group), raw: m.config[collection.key]}
	updated, changed, err := plan.migrate()
	if err != nil {
		return err
	}
	if changed {
		m.patch[collection.key] = updated
	}
	return nil
}

func (m configMigration) redeemFields() []string {
	for _, group := range m.owner.Groups {
		if group.Name == "redeem" {
			return group.Fields
		}
	}
	return nil
}

func (m configMigration) migrateSongqueueReplies() error {
	// Legacy chat replies have exact palettes distinct from current/redeem.
	m.reply(Group{MessageKey: "addMessage", Fields: []string{"user", "title", "artist", "pos"}}).migrateString()
	m.reply(Group{MessageKey: "playingMessage", Fields: []string{"user", "title", "artist", "req"}}).migrateString()
	m.reply(Group{MessageKey: "retractMessage", Fields: []string{"user", "title"}}).migrateString()
	m.reply(Group{MessageKey: "currentMessage", Fields: []string{"user", "title", "artist", "url", "req"}}).migrateString()
	return m.migrateRedeemReply()
}

func (m configMigration) migrateRedeemReply() error {
	raw := m.config["redeem"]
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var object map[string]codec.RawMessage
	if err := codec.Unmarshal(raw, &object); err != nil {
		return err
	}
	reply := m.reply(Group{MessageKey: "replyMessage", Fields: m.redeemFields()})
	reply.config = object
	reply.patch = make(map[string]codec.RawMessage)
	if !reply.migrateString() {
		return nil
	}
	object[reply.key] = reply.patch[reply.key]
	encoded, err := codec.Marshal(object)
	if err != nil {
		return err
	}
	m.patch["redeem"] = encoded
	return nil
}

func (m configMigration) migrateTriggerReplies() error {
	var rules string
	if codec.Unmarshal(m.config["rules"], &rules) != nil {
		return nil
	}
	plan := triggerMigration{reply: m.reply(Group{MessageKey: "response", Fields: m.fields}), rules: rules}
	updated, err := plan.rewrite()
	if err != nil {
		return err
	}
	if updated != rules {
		m.patch["rules"], err = codec.Marshal(updated)
	}
	return err
}

// triggerMigration keeps the structured/legacy rule source with its reply plan.
type triggerMigration struct {
	reply replyMigration
	rules string
}

func (m triggerMigration) rewrite() (string, error) {
	if !strings.HasPrefix(strings.TrimSpace(m.rules), "[") {
		return m.rewriteLegacyRules(), nil
	}
	plan := replyArrayMigration{reply: m.reply, raw: []byte(m.rules)}
	encoded, changed, err := plan.migrate()
	if err != nil {
		return "", err
	}
	if !changed {
		return m.rules, nil
	}
	return string(encoded), nil
}

func (m triggerMigration) rewriteLegacyRules() string {
	lines := strings.Split(m.rules, "\n")
	for i, line := range lines {
		lines[i] = m.rewriteLegacyLine(line)
	}
	return strings.Join(lines, "\n")
}

func (m triggerMigration) rewriteLegacyLine(line string) string {
	if strings.HasPrefix(strings.TrimSpace(line), "#") {
		return line
	}
	at := strings.Index(line, "=>")
	if at < 0 {
		return line
	}
	reply := replyTemplate{namespace: m.reply.namespace, text: line[at+2:]}
	return line[:at+2] + reply.rewrite()
}

func (m replyMigration) migrateString() bool {
	var text string
	if codec.Unmarshal(m.config[m.key], &text) != nil {
		return false
	}
	converted := replyTemplate{namespace: m.namespace, text: text}.rewrite()
	if converted == text {
		return false
	}
	// Marshal of a string cannot fail.
	m.patch[m.key], _ = codec.Marshal(converted)
	return true
}

type replyArrayMigration struct {
	reply replyMigration
	raw   codec.RawMessage
}

func (m replyArrayMigration) migrate() ([]byte, bool, error) {
	if len(m.raw) == 0 || string(m.raw) == "null" {
		return m.raw, false, nil
	}
	var objects []map[string]codec.RawMessage
	if err := codec.Unmarshal(m.raw, &objects); err != nil {
		return nil, false, err
	}
	changed := false
	for _, object := range objects {
		reply := m.reply
		reply.config = object
		reply.patch = make(map[string]codec.RawMessage)
		if reply.migrateString() {
			object[reply.key] = reply.patch[reply.key]
			changed = true
		}
	}
	if !changed {
		return m.raw, false, nil
	}
	encoded, err := codec.Marshal(objects)
	return encoded, true, err
}
