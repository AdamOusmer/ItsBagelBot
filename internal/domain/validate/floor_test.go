// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package validate

import (
	"errors"
	"testing"

	"ItsBagelBot/internal/moderation"
)

func init() {
	CheckFloor = moderation.CheckFloor
}

func floorSlur(t *testing.T) string {
	t.Helper()
	terms := moderation.EmbeddedLexicon().Terms(moderation.CatHate)
	if len(terms) == 0 {
		t.Fatal("embedded hate list empty")
	}
	return terms[0]
}

func TestCommandResponseFloor(t *testing.T) {
	slur := floorSlur(t)

	if err := CommandResponse("welcome to the stream " + slur); !errors.Is(err, ErrContentFloor) {
		t.Fatalf("slur in a command response must be refused, got %v", err)
	}
	leet := leetify(slur)
	if err := CommandResponse("hello " + leet + " world"); !errors.Is(err, ErrContentFloor) {
		t.Fatalf("obfuscated slur must be refused, got %v", err)
	}
	if err := CommandResponse("check my setup at grabify.link/pc"); !errors.Is(err, ErrContentFloor) {
		t.Fatalf("IP-grabber host must be refused, got %v", err)
	}

	for _, ok := range []string{
		"that was some bullshit, hell of a play though",
		"type !prize to claim your prize in tonight's giveaway",
		"damn this fucking game is hard",
	} {
		if err := CommandResponse(ok); err != nil {
			t.Fatalf("%q must be allowed (floor only refuses hate/abuse infra): %v", ok, err)
		}
	}
}

func leetify(term string) string {
	out := make([]rune, 0, len(term))
	for _, r := range term {
		switch r {
		case 'a':
			r = '4'
		case 'e':
			r = '3'
		case 'i':
			r = '1'
		case 'o':
			r = '0'
		case 's':
			r = '5'
		}
		out = append(out, r)
	}
	return string(out)
}

func TestConfigsJSONFloor(t *testing.T) {
	slur := floorSlur(t)

	bad := []byte(`{"message":"raid hype ` + slur + ` welcome"}`)
	if err := ConfigsJSON(bad); !errors.Is(err, ErrContentFloor) {
		t.Fatalf("floor term in a config string must be refused, got %v", err)
	}
	nested := []byte(`{"a":{"b":["fine","also fine","` + slur + `"]}}`)
	if err := ConfigsJSON(nested); !errors.Is(err, ErrContentFloor) {
		t.Fatalf("nested floor term must be refused, got %v", err)
	}
	ok := []byte(`{"message":"huge shoutout to {raider}, damn what a raid!","count":3}`)
	if err := ConfigsJSON(ok); err != nil {
		t.Fatalf("clean config refused: %v", err)
	}
}
