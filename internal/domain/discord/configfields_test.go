// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discord

import (
	"os"
	"reflect"
	"slices"
	"testing"

	"ItsBagelBot/pkg/codec"
)

func TestConfigFieldsMatchTheSharedFixture(t *testing.T) {
	want := readFieldFixture(t)
	got := configJSONTags(t)
	if !slices.Equal(got, want) {
		t.Fatalf("Config json tags drifted from testdata/config_fields.json\n only in Go:      %v\n only in fixture: %v",
			missing(got, want), missing(want, got))
	}
}

func configJSONTags(t *testing.T) []string {
	t.Helper()
	rt := reflect.TypeOf(Config{})
	out := make([]string, 0, rt.NumField())
	for i := range rt.NumField() {
		field := rt.Field(i)
		if !field.IsExported() {
			continue
		}
		name, _, _ := cutComma(field.Tag.Get("json"))
		if name == "" {
			t.Fatalf("Config.%s is exported with no json tag; it would ship as %q and the console would never see it",
				field.Name, field.Name)
		}
		if name == "-" {
			continue
		}
		out = append(out, name)
	}
	slices.Sort(out)
	return out
}

func cutComma(tag string) (string, string, bool) {
	for i := range len(tag) {
		if tag[i] == ',' {
			return tag[:i], tag[i+1:], true
		}
	}
	return tag, "", false
}

func readFieldFixture(t *testing.T) []string {
	t.Helper()
	raw, err := os.ReadFile("testdata/config_fields.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var doc struct {
		Fields []string `json:"fields"`
	}
	if err := codec.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	return doc.Fields
}

func missing(from, other []string) []string {
	out := []string{}
	for _, v := range from {
		if !slices.Contains(other, v) {
			out = append(out, v)
		}
	}
	return out
}
