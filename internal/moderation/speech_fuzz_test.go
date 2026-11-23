// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package moderation

import "testing"

func fuzzSpeechSeeds() []string {
	return []string{
		"n1gger", "n i g g a", "f.a.g.g.o.t", "n****r", "kill yourself",
		"join 192.168.1.1 now", "host fe80::1 ok", "1.1.1.1.1.1.1.1",
		"call +1 555 123 4567", "4111 1111 1111 1111", "mail me at foo@bar.com",
		"10 9 8 7 6 5 4 3", "убей себя", "ich weiß wo du wohnst", "sudaca",
	}
}

func fuzzStable(first, again SpeechVerdict) bool {
	return !again.Blocked && again.Message == first.Message
}

func FuzzCheckSpeech(f *testing.F) {
	for _, s := range fuzzNormalizeSeeds() {
		f.Add("viewer", s)
	}
	for _, s := range fuzzSpeechSeeds() {
		f.Add(s, s)
	}
	policy := SpeechPolicy{Harassment: true, Sexual: true, Profanity: true}

	f.Fuzz(func(t *testing.T, name, message string) {
		first := CheckSpeech(SpeechInput{Name: name, Message: message}, policy)
		if len(first.Message) > len(message) {
			t.Fatalf("message grew: %q -> %q", message, first.Message)
		}
		again := CheckSpeech(SpeechInput{Name: name, Message: first.Message}, policy)
		if !first.Blocked && !fuzzStable(first, again) {
			t.Fatalf("unstable: %q -> %q -> %+v", message, first.Message, again)
		}
		CheckSpeechFloor(message)
	})
}
