// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package moderation

import "bytes"

type SpeechReason string

const (
	SpeechPass              SpeechReason = ""
	SpeechFloorHate         SpeechReason = "floor_hate"
	SpeechFloorSexual       SpeechReason = "floor_sexual"
	SpeechFloorSelfHarm     SpeechReason = "floor_selfharm"
	SpeechFloorThreat       SpeechReason = "floor_threat"
	SpeechPII               SpeechReason = "pii"
	SpeechChannelHarassment SpeechReason = "channel_harassment"
	SpeechChannelSexual     SpeechReason = "channel_sexual"
	SpeechChannelProfanity  SpeechReason = "channel_profanity"
	SpeechChannelTerm       SpeechReason = "channel_term"
)

type SpeechField uint8

const (
	SpeechFieldNone SpeechField = iota
	SpeechFieldName
	SpeechFieldMessage
)

type SpeechInput struct {
	Name    string
	Message string
}

type SpeechPolicy struct {
	Harassment bool
	Sexual     bool
	Profanity  bool
	BlockTerms [][]byte
	AllowTerms [][]byte
}

type SpeechVerdict struct {
	Message  string
	Blocked  bool
	Field    SpeechField
	Reason   SpeechReason
	MaskOnly bool
}

func (r SpeechReason) Floor() bool {
	switch r {
	case SpeechFloorHate, SpeechFloorSexual, SpeechFloorSelfHarm, SpeechFloorThreat:
		return true
	}
	return false
}

func (v SpeechVerdict) StrikeWeight() int64 {
	if v.countsAsStrike() && v.Reason.Floor() {
		return 1
	}
	return 0
}

func (v SpeechVerdict) countsAsStrike() bool { return v.Blocked && !v.MaskOnly }

func CheckSpeech(in SpeechInput, policy SpeechPolicy) SpeechVerdict {
	message := stripAddresses(in.Message)
	subjects := [2]speechSubject{
		newSpeechSubject(SpeechFieldName, in.Name),
		newSpeechSubject(SpeechFieldMessage, message),
	}
	verdict := SpeechVerdict{Message: message}
	if field, hit := firstFloorHit(subjects); hit.reason != SpeechPass {
		return verdict.blockedBy(field, hit)
	}
	if hasPersonalData(message) {
		return verdict.blockedBy(SpeechFieldMessage, speechHit{reason: SpeechPII})
	}
	if field, reason := firstChannelReason(subjects, policy); reason != SpeechPass {
		return verdict.blockedBy(field, speechHit{reason: reason})
	}
	return verdict
}

func firstFloorHit(subjects [2]speechSubject) (SpeechField, speechHit) {
	for _, s := range subjects {
		if hit := s.floor(); hit.reason != SpeechPass {
			return s.field, hit
		}
	}
	return SpeechFieldNone, speechHit{}
}

func firstChannelReason(subjects [2]speechSubject, policy SpeechPolicy) (SpeechField, SpeechReason) {
	for _, s := range subjects {
		if reason := s.channel(policy); reason != SpeechPass {
			return s.field, reason
		}
	}
	return SpeechFieldNone, SpeechPass
}

func CheckSpeechFloor(text string) (SpeechReason, bool) {
	hit := newSpeechSubject(SpeechFieldMessage, text).floor()
	return hit.reason, hit.reason != SpeechPass
}

func ContainsAnyTerm(skel []byte, terms [][]byte) bool {
	for _, term := range terms {
		if len(term) > 0 && bytes.Contains(skel, term) {
			return true
		}
	}
	return false
}

func (v SpeechVerdict) blockedBy(field SpeechField, hit speechHit) SpeechVerdict {
	v.Blocked = true
	v.Field = field
	v.Reason = hit.reason
	v.MaskOnly = hit.maskOnly
	return v
}
