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
	Message string
	Blocked bool
	Field   SpeechField
	Reason  SpeechReason
}

func (r SpeechReason) Floor() bool {
	switch r {
	case SpeechFloorHate, SpeechFloorSexual, SpeechFloorSelfHarm, SpeechFloorThreat:
		return true
	}
	return false
}

func CheckSpeech(in SpeechInput, policy SpeechPolicy) SpeechVerdict {
	message := stripAddresses(in.Message)
	subjects := [2]speechSubject{
		newSpeechSubject(SpeechFieldName, in.Name),
		newSpeechSubject(SpeechFieldMessage, message),
	}
	verdict := SpeechVerdict{Message: message}
	if field, reason := firstFloorHit(subjects); reason != SpeechPass {
		return verdict.blockedBy(field, reason)
	}
	if hasPersonalData(message) {
		return verdict.blockedBy(SpeechFieldMessage, SpeechPII)
	}
	if field, reason := firstChannelReason(subjects, policy); reason != SpeechPass {
		return verdict.blockedBy(field, reason)
	}
	return verdict
}

func firstFloorHit(subjects [2]speechSubject) (SpeechField, SpeechReason) {
	for _, s := range subjects {
		if reason := s.floor(); reason != SpeechPass {
			return s.field, reason
		}
	}
	return SpeechFieldNone, SpeechPass
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
	reason := newSpeechSubject(SpeechFieldMessage, text).floor()
	return reason, reason != SpeechPass
}

func ContainsAnyTerm(skel []byte, terms [][]byte) bool {
	for _, term := range terms {
		if len(term) > 0 && bytes.Contains(skel, term) {
			return true
		}
	}
	return false
}

func (v SpeechVerdict) blockedBy(field SpeechField, reason SpeechReason) SpeechVerdict {
	v.Blocked = true
	v.Field = field
	v.Reason = reason
	return v
}
