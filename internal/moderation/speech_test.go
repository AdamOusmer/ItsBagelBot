// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package moderation

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func blockedVerdict(reason SpeechReason, field SpeechField) SpeechVerdict {
	return SpeechVerdict{Blocked: true, Reason: reason, Field: field}
}

func messageOnly(text string) SpeechInput { return SpeechInput{Message: text} }

func terms(words ...string) [][]byte {
	out := make([][]byte, len(words))
	for i, w := range words {
		out[i] = []byte(w)
	}
	return out
}

func TestCheckSpeech(t *testing.T) {
	hate := blockedVerdict(SpeechFloorHate, SpeechFieldMessage)
	selfHarm := blockedVerdict(SpeechFloorSelfHarm, SpeechFieldMessage)
	threat := blockedVerdict(SpeechFloorThreat, SpeechFieldMessage)
	sexual := blockedVerdict(SpeechFloorSexual, SpeechFieldMessage)
	pii := blockedVerdict(SpeechPII, SpeechFieldMessage)
	allOn := SpeechPolicy{Harassment: true, Sexual: true, Profanity: true}

	cases := []struct {
		name   string
		in     SpeechInput
		policy SpeechPolicy
		want   SpeechVerdict
	}{
		{"leet fold", messageOnly("n1gger"), SpeechPolicy{}, hate},
		{"cyrillic lookalike", messageOnly("nіgger"), SpeechPolicy{}, hate},
		{"fullwidth letters", messageOnly("ｎｉｇｇｅｒ"), SpeechPolicy{}, hate},
		{"dotted letters pass", messageOnly("f.a.g.g.o.t"), SpeechPolicy{}, SpeechVerdict{}},
		{"dotted acronym usa", messageOnly("u.s.a"), SpeechPolicy{}, SpeechVerdict{}},
		{"dotted acronym faq", messageOnly("f.a.q"), SpeechPolicy{}, SpeechVerdict{}},
		{"spaced letters", messageOnly("a b c d e"), SpeechPolicy{}, SpeechVerdict{}},
		{"spaced slur letters pass", messageOnly("n i g g a"), SpeechPolicy{}, SpeechVerdict{}},
		{"split slur pass", messageOnly("nig ger"), SpeechPolicy{}, SpeechVerdict{}},
		{"repeated letters", messageOnly("nigggger"), SpeechPolicy{}, hate},
		{"masked slur passes", messageOnly("n****r"), SpeechPolicy{}, SpeechVerdict{}},
		{"masked swear passes", messageOnly("f*ck"), SpeechPolicy{}, SpeechVerdict{}},
		{"masked word passes", messageOnly("s***"), SpeechPolicy{}, SpeechVerdict{}},
		{"masked two letters passes", messageOnly("f*ggot"), SpeechPolicy{}, SpeechVerdict{}},
		{"fr hate", messageOnly("bougnoule"), SpeechPolicy{}, hate},
		{"de hate", messageOnly("kanake"), SpeechPolicy{}, hate},
		{"es hate", messageOnly("sudaca"), SpeechPolicy{}, hate},
		{"pt-br hate", messageOnly("viado"), SpeechPolicy{}, hate},
		{"ru hate", messageOnly("хач"), SpeechPolicy{}, hate},
		{"en self harm", messageOnly("kill yourself"), SpeechPolicy{}, selfHarm},
		{"fr self harm", messageOnly("tue-toi"), SpeechPolicy{}, selfHarm},
		{"de self harm", messageOnly("bring dich um"), SpeechPolicy{}, selfHarm},
		{"es self harm", messageOnly("ahórcate"), SpeechPolicy{}, selfHarm},
		{"pt-br self harm", messageOnly("vai se matar"), SpeechPolicy{}, selfHarm},
		{"ru self harm", messageOnly("убей себя"), SpeechPolicy{}, selfHarm},
		{"en threat", messageOnly("i know where you live"), SpeechPolicy{}, threat},
		{"de threat", messageOnly("ich weiß wo du wohnst"), SpeechPolicy{}, threat},
		{"en sexual", messageOnly("suck my dick"), SpeechPolicy{}, sexual},
		{"ru sexual", messageOnly("соси мой хуй"), SpeechPolicy{}, sexual},
		{"email", messageOnly("mail me at foo@bar.com"), SpeechPolicy{}, pii},
		{"phone with plus", messageOnly("call +1 555 123 4567"), SpeechPolicy{}, pii},
		{"digit run passes", messageOnly("1234567890"), SpeechPolicy{}, SpeechVerdict{}},
		{"order id passes", messageOnly("order 20261122123456 shipped"), SpeechPolicy{}, SpeechVerdict{}},
		{"phone plus eight digits", messageOnly("+44 20 7946 0958"), SpeechPolicy{}, pii},
		{"phone parenthesised", messageOnly("(555) 123-4567"), SpeechPolicy{}, pii},
		{"phone dashed", messageOnly("555-123-4567"), SpeechPolicy{}, pii},
		{"card passing luhn", messageOnly("4111 1111 1111 1111"), SpeechPolicy{}, pii},
		{"card failing luhn", messageOnly("4111 1111 1111 1112"), SpeechPolicy{}, SpeechVerdict{}},
		{"ipv4 removed", messageOnly("join 192.168.1.1 now"), SpeechPolicy{}, SpeechVerdict{Message: "join  now"}},
		{"ipv6 removed", messageOnly("host fe80::1 ok"), SpeechPolicy{}, SpeechVerdict{Message: "host  ok"}},
		{"name is not scanned for pii", SpeechInput{Name: "foo@bar.com", Message: "hi"}, SpeechPolicy{}, SpeechVerdict{}},
		{"blocked name", SpeechInput{Name: "nigger", Message: "hello"}, SpeechPolicy{}, blockedVerdict(SpeechFloorHate, SpeechFieldName)},
		{"blocked message", SpeechInput{Name: "Lee", Message: "kill yourself"}, SpeechPolicy{}, selfHarm},
		{"clean name and message", SpeechInput{Name: "fine", Message: "fine"}, SpeechPolicy{}, SpeechVerdict{}},
		{"profanity on", messageOnly("what the fuck"), allOn, blockedVerdict(SpeechChannelProfanity, SpeechFieldMessage)},
		{"profanity off", messageOnly("what the fuck"), SpeechPolicy{}, SpeechVerdict{}},
		{"short entries stay bounded", messageOnly("class"), allOn, SpeechVerdict{}},
		{"harassment on", messageOnly("go die"), SpeechPolicy{Harassment: true}, blockedVerdict(SpeechChannelHarassment, SpeechFieldMessage)},
		{"harassment off", messageOnly("go die"), SpeechPolicy{}, SpeechVerdict{}},
		{"sexual on", messageOnly("porn"), SpeechPolicy{Sexual: true}, blockedVerdict(SpeechChannelSexual, SpeechFieldMessage)},
		{"block term", messageOnly("my secret name"), SpeechPolicy{BlockTerms: terms("secret")}, blockedVerdict(SpeechChannelTerm, SpeechFieldMessage)},
		{"allow term exempts channel", messageOnly("fuck"), SpeechPolicy{Profanity: true, AllowTerms: terms("fuck")}, SpeechVerdict{}},
		{"allow term cannot unblock floor", messageOnly("nigger"), SpeechPolicy{AllowTerms: terms("nigger")}, hate},
		{"channel tier on name", SpeechInput{Name: "fuck", Message: "hi"}, allOn, blockedVerdict(SpeechChannelProfanity, SpeechFieldName)},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			want := tt.want
			if want.Message == "" {
				want.Message = tt.in.Message
			}
			assert.Equal(t, want, CheckSpeech(tt.in, tt.policy))
		})
	}
}

func TestCheckSpeechFloor(t *testing.T) {
	cases := []struct {
		text   string
		reason SpeechReason
	}{
		{"n1gger", SpeechFloorHate},
		{"Bob says kill yourself", SpeechFloorSelfHarm},
		{"hello there", SpeechPass},
		{"what the fuck", SpeechPass},
	}
	for _, tt := range cases {
		reason, blocked := CheckSpeechFloor(tt.text)
		assert.Equal(t, tt.reason, reason, tt.text)
		assert.Equal(t, tt.reason != SpeechPass, blocked, tt.text)
	}
}

func TestCheckSpeechBenignCorpus(t *testing.T) {
	data, err := os.ReadFile("testdata/speech/benign.txt")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range parseTerms(data) {
		verdict := CheckSpeech(SpeechInput{Name: line, Message: line}, SpeechPolicy{})
		if verdict.Blocked || verdict.Message != line {
			t.Errorf("benign %q: %+v", line, verdict)
		}
		if reason, blocked := CheckSpeechFloor(line); blocked {
			t.Errorf("benign %q: floor %q", line, reason)
		}
	}
}
