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
	known := make(map[string]bool, len(fields))
	for _, field := range fields {
		known[strings.ToLower(field)] = true
	}
	var out strings.Builder
	for _, tok := range tmpl.Lex(text) {
		if tok.Kind == tmpl.KindLiteral {
			out.WriteString(tok.Text)
			continue
		}
		if cond, ok := tok.Cond(); ok && !tok.HasInnerBrace() && !cond.Ref.HasPayload && known[cond.Ref.Name] {
			start := strings.IndexByte(tok.Raw, ':') + 1
			end := start + len(cond.Ref.Name)
			converted := tok.Raw[:start] + moduleID + ":" + cond.Ref.Name + tok.Raw[end:]
			// Namespaced references consume an extra ':' segment. A conditional
			// with only a then branch needs an explicit empty else to retain it.
			if strings.Count(tok.Payload, ":") == 1 {
				at := len(converted) - 1
				if tok.HasFallback {
					at = strings.LastIndexByte(converted, '|')
				}
				converted = converted[:at] + ":" + converted[at:]
			}
			out.WriteString(converted)
		} else if !tok.HasPayload && known[tok.Name] {
			out.WriteString("{" + moduleID + ":" + tok.Name)
			if tok.HasFallback {
				out.WriteString("|" + tok.Fallback)
			}
			out.WriteByte('}')
		} else {
			out.WriteString(tok.Raw)
		}
	}
	return out.String()
}

// MigrateConfig returns only changed top-level config keys, suitable for the
// modules.patch-existing RPC. Ownership comes from the public reply catalogue; arbitrary
// custom-command responses deliberately have no inferred module ownership.
func MigrateConfig(moduleID string, raw []byte) (map[string]codec.RawMessage, error) {
	if len(strings.TrimSpace(string(raw))) == 0 {
		return nil, nil
	}
	var config map[string]codec.RawMessage
	if err := codec.Unmarshal(raw, &config); err != nil {
		return nil, err
	}
	patch := make(map[string]codec.RawMessage)
	var fields []string
	var redeemFields []string
	for _, mod := range Catalog() {
		if mod.ID != moduleID {
			continue
		}
		for _, group := range mod.Groups {
			fields = append(fields, group.Fields...)
			if group.Name == "redeem" {
				redeemFields = group.Fields
			}
			if group.MessageKey != "" {
				migrateString(config, patch, group.MessageKey, moduleID, group.Fields)
			}
		}
	}
	if len(fields) == 0 {
		return nil, nil
	}
	switch moduleID {
	case "govee":
		migrateString(config, patch, "replyMessage", moduleID, fields)
		if updated, changed, err := migrateObjects(config["bindings"], "replyMessage", moduleID, fields); err != nil {
			return nil, err
		} else if changed {
			patch["bindings"] = updated
		}
	case "channelpoints":
		if updated, changed, err := migrateObjects(config["rewards"], "message", moduleID, fields); err != nil {
			return nil, err
		} else if changed {
			patch["rewards"] = updated
		}
	case "queue":
		// These saved templates predate the editable public reply catalogue.
		migrateString(config, patch, "openedMessage", moduleID, []string{"user"})
		migrateString(config, patch, "closedMessage", moduleID, []string{"user"})
	case "songqueue":
		// Legacy chat replies have their own exact palettes. Do not infer their
		// fields from the current-song or redeem public group.
		migrateString(config, patch, "addMessage", moduleID, []string{"user", "title", "artist", "pos"})
		migrateString(config, patch, "playingMessage", moduleID, []string{"user", "title", "artist", "req"})
		migrateString(config, patch, "retractMessage", moduleID, []string{"user", "title"})
		migrateString(config, patch, "currentMessage", moduleID, []string{"user", "title", "artist", "url", "req"})
		if len(config["redeem"]) > 0 && string(config["redeem"]) != "null" {
			var object map[string]codec.RawMessage
			if err := codec.Unmarshal(config["redeem"], &object); err != nil {
				return nil, err
			}
			change := make(map[string]codec.RawMessage)
			migrateString(object, change, "replyMessage", moduleID, redeemFields)
			if len(change) > 0 {
				for key, value := range change {
					object[key] = value
				}
				encoded, err := codec.Marshal(object)
				if err != nil {
					return nil, err
				}
				patch["redeem"] = encoded
			}
		}
	case "triggers":
		var rules string
		if codec.Unmarshal(config["rules"], &rules) == nil {
			updated := rules
			if strings.HasPrefix(strings.TrimSpace(rules), "[") {
				encoded, changed, err := migrateObjects([]byte(rules), "response", moduleID, fields)
				if err != nil {
					return nil, err
				}
				if changed {
					updated = string(encoded)
				}
			} else {
				lines := strings.Split(rules, "\n")
				for i, line := range lines {
					if strings.HasPrefix(strings.TrimSpace(line), "#") {
						continue
					}
					if at := strings.Index(line, "=>"); at >= 0 {
						lines[i] = line[:at+2] + NamespaceTemplate(moduleID, fields, line[at+2:])
					}
				}
				updated = strings.Join(lines, "\n")
			}
			if updated != rules {
				encoded, err := codec.Marshal(updated)
				if err != nil {
					return nil, err
				}
				patch["rules"] = encoded
			}
		}
	}
	if len(patch) == 0 {
		return nil, nil
	}
	return patch, nil
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
