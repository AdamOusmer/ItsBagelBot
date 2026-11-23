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

func maskedVerdict(reason SpeechReason) SpeechVerdict {
	return SpeechVerdict{Blocked: true, Reason: reason, Field: SpeechFieldMessage, MaskOnly: true}
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
		{"dotted letters", messageOnly("f.a.g.g.o.t"), SpeechPolicy{}, hate},
		{"slashed letters", messageOnly("n/i/g/g/e/r"), SpeechPolicy{}, hate},
		{"repeated letters", messageOnly("nigggger"), SpeechPolicy{}, hate},
		{"phonetic trailing z", messageOnly("niggaz"), SpeechPolicy{}, hate},
		{"phonetic ph", messageOnly("phaggot"), SpeechPolicy{}, hate},
		{"phonetic is hate only", messageOnly("suq my dick"), SpeechPolicy{}, SpeechVerdict{}},
		{"joined chunks", messageOnly("nig ger"), SpeechPolicy{}, hate},
		{"single letter run", messageOnly("n i g g a"), SpeechPolicy{}, hate},
		{"run of two stays apart", messageOnly("n i"), SpeechPolicy{}, SpeechVerdict{}},
		{"long words do not fuse", messageOnly("brag headline"), SpeechPolicy{}, SpeechVerdict{}},
		{"mask with two letters", messageOnly("f*ggot"), SpeechPolicy{}, maskedVerdict(SpeechFloorHate)},
		{"mask run", messageOnly("n****r"), SpeechPolicy{}, maskedVerdict(SpeechFloorHate)},
		{"mask with one letter", messageOnly("s***"), SpeechPolicy{}, SpeechVerdict{}},
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
		{"phone digit run", messageOnly("my number is 5551234567"), SpeechPolicy{}, pii},
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

func TestSpeechStrikeWeight(t *testing.T) {
	cases := []struct {
		name string
		text string
		want int64
	}{
		{"floor hit", "nigger", 1},
		{"mask only", "n****r", 0},
		{"personal data", "foo@bar.com", 0},
		{"channel tier", "what the fuck", 0},
		{"clean", "hello", 0},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			verdict := CheckSpeech(messageOnly(tt.text), SpeechPolicy{Profanity: true})
			assert.Equal(t, tt.want, verdict.StrikeWeight())
		})
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
