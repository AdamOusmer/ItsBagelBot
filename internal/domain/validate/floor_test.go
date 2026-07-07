package validate

import (
	"errors"
	"testing"

	"ItsBagelBot/internal/moderation"
)

// floorSlur pulls a term from the embedded hate artifact so no slur sits in
// test source.
func floorSlur(t *testing.T) string {
	t.Helper()
	terms := moderation.EmbeddedLexicon().Terms(moderation.CatHate)
	if len(terms) == 0 {
		t.Fatal("embedded hate list empty")
	}
	return terms[0]
}

// The bot posts command responses as itself: the immovable floor is enforced at
// save time, while everything milder stays allowed (people say what they want).
func TestCommandResponseFloor(t *testing.T) {
	slur := floorSlur(t)

	if err := CommandResponse("welcome to the stream " + slur); !errors.Is(err, ErrContentFloor) {
		t.Fatalf("slur in a command response must be refused, got %v", err)
	}
	// Obfuscation folds onto the plain spelling.
	leet := leetify(slur)
	if err := CommandResponse("hello " + leet + " world"); !errors.Is(err, ErrContentFloor) {
		t.Fatalf("obfuscated slur must be refused, got %v", err)
	}
	if err := CommandResponse("check my setup at grabify.link/pc"); !errors.Is(err, ErrContentFloor) {
		t.Fatalf("IP-grabber host must be refused, got %v", err)
	}

	// Milder-but-legal stays allowed: profanity, scam-sounding giveaway copy.
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

// leetify obfuscates a term (a->4, e->3, i->1, o->0, s->5).
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

// Module config blobs feed bot-emitted templates: every string value is held to
// the floor, nested or not; keys and non-strings are not free text.
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
