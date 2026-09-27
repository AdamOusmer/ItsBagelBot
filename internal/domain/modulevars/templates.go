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
	namespace := templateNamespace{moduleID: moduleID, fields: make(map[string]bool, len(fields))}
	for _, field := range fields {
		namespace.fields[strings.ToLower(field)] = true
	}
	var out strings.Builder
	for _, token := range tmpl.Lex(text) {
		out.WriteString(namespace.rewrite(token))
	}
	return out.String()
}

type templateNamespace struct {
	moduleID string
	fields   map[string]bool
}

func (n templateNamespace) rewrite(token tmpl.Token) string {
	if token.Kind == tmpl.KindLiteral {
		return token.Text
	}
	if condition, ok := token.Cond(); ok {
		return n.rewriteCondition(token, condition)
	}
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
	return preserveConditionalElse(token, converted)
}

func preserveConditionalElse(token tmpl.Token, converted string) string {
	// A namespaced reference consumes an extra ':' segment. A single then
	// branch therefore needs an explicit empty else to preserve its meaning.
	if strings.Count(token.Payload, ":") != 1 {
		return converted
	}
	at := len(converted) - 1
	if token.HasFallback {
		at = strings.LastIndexByte(converted, '|')
	}
	return converted[:at] + ":" + converted[at:]
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
	migration := newConfigMigration(moduleID, config)
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

type configMigration struct {
	moduleID      string
	config, patch map[string]codec.RawMessage
	groups        []Group
	fields        []string
}

func newConfigMigration(moduleID string, config map[string]codec.RawMessage) configMigration {
	migration := configMigration{moduleID: moduleID, config: config, patch: make(map[string]codec.RawMessage), groups: replyGroups(moduleID)}
	for _, group := range migration.groups {
		migration.fields = append(migration.fields, group.Fields...)
	}
	return migration
}

func replyGroups(moduleID string) []Group {
	for _, module := range Catalog() {
		if module.ID == moduleID {
			return module.Groups
		}
	}
	return nil
}

func (m configMigration) migrateReplyGroups() {
	for _, group := range m.groups {
		if group.MessageKey != "" {
			m.migrateReply(group.MessageKey, group.Fields)
		}
	}
}

func (m configMigration) migrateReply(key string, fields []string) {
	migrateString(m.config, m.patch, key, m.moduleID, fields)
}

func (m configMigration) migrateAdditionalReplies() error {
	switch m.moduleID {
	case "govee":
		m.migrateReply("replyMessage", m.fields)
		return m.migrateReplyArray("bindings", "replyMessage")
	case "channelpoints":
		return m.migrateReplyArray("rewards", "message")
	case "queue":
		m.migrateReply("openedMessage", []string{"user"})
		m.migrateReply("closedMessage", []string{"user"})
	case "songqueue":
		return m.migrateSongqueueReplies()
	case "triggers":
		return m.migrateTriggerReplies()
	}
	return nil
}

func (m configMigration) migrateReplyArray(configKey, replyKey string) error {
	updated, changed, err := migrateObjects(m.config[configKey], replyKey, m.moduleID, m.fields)
	if err != nil {
		return err
	}
	if changed {
		m.patch[configKey] = updated
	}
	return nil
}

func (m configMigration) groupFields(name string) []string {
	for _, group := range m.groups {
		if group.Name == name {
			return group.Fields
		}
	}
	return nil
}

func (m configMigration) migrateSongqueueReplies() error {
	// Legacy chat replies have exact palettes distinct from current/redeem.
	m.migrateReply("addMessage", []string{"user", "title", "artist", "pos"})
	m.migrateReply("playingMessage", []string{"user", "title", "artist", "req"})
	m.migrateReply("retractMessage", []string{"user", "title"})
	m.migrateReply("currentMessage", []string{"user", "title", "artist", "url", "req"})
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
	change := make(map[string]codec.RawMessage)
	migrateString(object, change, "replyMessage", m.moduleID, m.groupFields("redeem"))
	if len(change) == 0 {
		return nil
	}
	for key, value := range change {
		object[key] = value
	}
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
	updated, err := m.rewriteTriggerRules(rules)
	if err != nil {
		return err
	}
	if updated != rules {
		m.patch["rules"], err = codec.Marshal(updated)
	}
	return err
}

func (m configMigration) rewriteTriggerRules(rules string) (string, error) {
	if !strings.HasPrefix(strings.TrimSpace(rules), "[") {
		return m.rewriteLegacyTriggerRules(rules), nil
	}
	encoded, changed, err := migrateObjects([]byte(rules), "response", m.moduleID, m.fields)
	if err != nil {
		return "", err
	}
	if !changed {
		return rules, nil
	}
	return string(encoded), nil
}

func (m configMigration) rewriteLegacyTriggerRules(rules string) string {
	lines := strings.Split(rules, "\n")
	for i, line := range lines {
		lines[i] = m.rewriteLegacyTriggerLine(line)
	}
	return strings.Join(lines, "\n")
}

func (m configMigration) rewriteLegacyTriggerLine(line string) string {
	if strings.HasPrefix(strings.TrimSpace(line), "#") {
		return line
	}
	at := strings.Index(line, "=>")
	if at < 0 {
		return line
	}
	return line[:at+2] + NamespaceTemplate(m.moduleID, m.fields, line[at+2:])
}

func migrateString(config, patch map[string]codec.RawMessage, key, moduleID string, fields []string) {
	var text string
	if codec.Unmarshal(config[key], &text) != nil {
		return
	}
	if converted := NamespaceTemplate(moduleID, fields, text); converted != text {
		// Marshal of a string cannot fail.
		patch[key], _ = codec.Marshal(converted)
	}
}

func migrateObjects(raw []byte, key, moduleID string, fields []string) ([]byte, bool, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return raw, false, nil
	}
	var objects []map[string]codec.RawMessage
	if err := codec.Unmarshal(raw, &objects); err != nil {
		return nil, false, err
	}
	changed := false
	for _, object := range objects {
		patch := make(map[string]codec.RawMessage)
		migrateString(object, patch, key, moduleID, fields)
		for field, value := range patch {
			object[field] = value
			changed = true
		}
	}
	if !changed {
		return raw, false, nil
	}
	encoded, err := codec.Marshal(objects)
	return encoded, true, err
}
