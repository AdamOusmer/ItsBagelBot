// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package network

import (
	"unicode/utf8"

	"ItsBagelBot/app/desktop/internal/bridge/protocol"
	"ItsBagelBot/app/desktop/internal/network/wire"
	"ItsBagelBot/pkg/protobuf"
)

const (
	fID uint32 = 1 << iota
	fStream
	fSlot
	fEpoch
	fOrigin
	fAuth
	fBridge
	fRoots
	fPath
	fToken
	fProof
	fForm
	fRevoke
	fContent
	fBody
	fStatus
	fNonce
	fError
	fIsolated
	fMessage
	fOAuth
)

type frameRule struct{ allowed, required uint32 }

var frameRules = [...]frameRule{
	hello:         {fIsolated, fIsolated},
	bootstrap:     {fID | fAuth | fBridge | fRoots, fID | fAuth},
	oauthRequest:  {fID | fPath | fToken | fProof | fForm | fRevoke, fID | fPath | fProof},
	dialRequest:   {fID | fStream | fPath | fToken | fProof, fID | fStream | fPath | fToken | fProof},
	postRequest:   {fID | fPath | fToken | fProof | fContent | fBody, fID | fPath | fToken | fProof | fContent | fBody},
	closeStream:   {fStream, fStream},
	ackStream:     {fStream, fStream},
	streamFrame:   {fStream | fMessage, fStream | fMessage},
	streamError:   {fStream | fError, fStream | fError},
	openLane:      {fID | fSlot | fEpoch | fOrigin, fID | fEpoch},
	closeLane:     {fSlot | fEpoch, fEpoch},
	response:      {fID | fStatus | fNonce | fError | fOAuth, fID},
	cancelRequest: {fID, fID},
}

const parentReceives = 1<<hello | 1<<openLane | 1<<closeLane | 1<<streamFrame | 1<<streamError | 1<<response | 1<<cancelRequest
const workerReceives = 1<<bootstrap | 1<<oauthRequest | 1<<dialRequest | 1<<postRequest | 1<<closeStream | 1<<ackStream | 1<<response | 1<<cancelRequest

// Every decoded private envelope is checked before response matching or handler
// dispatch. The fixed field set grants no host command or host-information API.
func validFrame(m *wire.Frame) bool {
	if m == nil || m.Kind == 0 || m.Kind >= uint32(len(frameRules)) || protobuf.HasUnknown(m) {
		return false
	}
	rule := frameRules[m.Kind]
	present := frameFields(m)
	if present & ^rule.allowed != 0 || present&rule.required != rule.required {
		return false
	}
	return validFrameBounds(m) && validFrameOperation(m)
}

func frameFields(m *wire.Frame) uint32 {
	values := [...]bool{m.Id != 0, m.Stream != 0, m.Slot != 0, m.Epoch != 0, m.Origin != 0,
		m.AuthEndpoint != "", m.BridgeEndpoint != "", len(m.RootsPem) != 0, m.Path != "",
		m.Token != "", m.Proof != "", m.Form != "", m.Revoke, m.ContentType != "", len(m.Body) != 0,
		m.Status != 0, m.Nonce != "", m.Error != 0, m.Isolated, m.Message != nil, m.Oauth != nil}
	var mask uint32
	for i, present := range values {
		if present {
			mask |= 1 << i
		}
	}
	return mask
}

func validFrameBounds(m *wire.Frame) bool {
	if m.Slot >= laneCount || m.Origin >= 2 || m.Error > closedError || !validStatus(m.Status) {
		return false
	}
	if len(m.RootsPem) > 64<<10 || len(m.Body) > protocol.MaxFrameBytes || len(m.Form) > 4096 {
		return false
	}
	return validFrameText(m) && validOAuthReply(m.Oauth)
}

func validStatus(status int32) bool { return status == 0 || status >= 100 && status <= 599 }

func validFrameText(m *wire.Frame) bool {
	return boundedText(m.AuthEndpoint, 2048) && boundedText(m.BridgeEndpoint, 2048) && boundedText(m.Path, 2048) &&
		boundedHeader(m.Token, 8192) && boundedHeader(m.Proof, 4096) && boundedHeader(m.Nonce, 512)
}

func boundedText(value string, limit int) bool { return len(value) <= limit && utf8.ValidString(value) }
func boundedHeader(value string, limit int) bool {
	if len(value) > limit {
		return false
	}
	for i := range value {
		if value[i] < 33 || value[i] > 126 {
			return false
		}
	}
	return true
}

func validOAuthReply(r *wire.OAuthReply) bool {
	if r == nil {
		return true
	}
	if protobuf.HasUnknown(r) || r.Expires < 0 || r.Expires > 7*24*3600 {
		return false
	}
	return boundedHeader(r.Access, 8192) && boundedHeader(r.Refresh, 1024) && boundedHeader(r.TokenType, 16) &&
		boundedHeader(r.Grant, 128) && boundedHeader(r.Device, 128) && boundedHeader(r.Error, 64)
}

func validFrameOperation(m *wire.Frame) bool {
	switch m.Kind {
	case bootstrap:
		return validBootstrap(m)
	case oauthRequest:
		return !m.Revoke && m.Token == "" || m.Revoke && m.Token != "" && m.Form == ""
	case dialRequest:
		return m.Path == protocol.EventsPath
	case postRequest:
		return validPost(m)
	case streamFrame:
		return protocol.Validate(m.Message) == nil
	default:
		return true
	}
}

// Responses cannot change operation type merely by guessing an outstanding ID.
func validResponseFor(m *wire.Frame, operation uint32) bool {
	if m.Oauth != nil && operation != oauthRequest {
		return false
	}
	if operation == bootstrap || operation == openLane {
		return m.Status == 0 && m.Nonce == ""
	}
	return true
}
