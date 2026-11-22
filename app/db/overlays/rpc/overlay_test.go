// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc_test

import (
	"testing"

	domainrpc "ItsBagelBot/internal/domain/rpc"
	overlaysrpc "ItsBagelBot/internal/domain/rpc/overlays"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOverlayRPCLifecycle(t *testing.T) {
	nc := serve(t)
	user := overlaysrpc.OverlayUserRequest{UserID: "1001"}
	added := request[overlaysrpc.OverlayView](t, nc, overlaysrpc.OverlayAddTemplateSubject, overlaysrpc.OverlayAddTemplateRequest{OverlayUserRequest: user, Template: "powered"})
	require.Equal(t, domainrpc.CodeOK, added.Code, added.Error)
	require.Contains(t, added.URL, overlaysrpc.OverlayURLPrefix)
	ref := overlaysrpc.OverlayRefRequest{OverlayUserRequest: user, ID: added.ID}

	list := request[overlaysrpc.OverlayListReply](t, nc, overlaysrpc.OverlayListSubject, user)
	require.Len(t, list.Overlays, 1)
	assert.Equal(t, [2]string{"system", "powered"}, [2]string{list.Overlays[0].Kind, list.Overlays[0].Template})

	rotated := request[overlaysrpc.OverlayView](t, nc, overlaysrpc.OverlayRotateSubject, ref)
	require.Equal(t, domainrpc.CodeOK, rotated.Code, rotated.Error)
	assert.NotEqual(t, added.URL, rotated.URL)

	deleted := request[overlaysrpc.OverlayView](t, nc, overlaysrpc.OverlayDeleteSubject, ref)
	require.Equal(t, domainrpc.CodeOK, deleted.Code, deleted.Error)
	after := request[overlaysrpc.OverlayView](t, nc, overlaysrpc.OverlayGetSubject, ref)
	assert.Equal(t, domainrpc.CodeNotFound, after.Code)
}

func TestOverlayRPCRefusals(t *testing.T) {
	nc := serve(t)
	created := request[overlaysrpc.OverlayView](t, nc, overlaysrpc.OverlayAddTemplateSubject,
		overlaysrpc.OverlayAddTemplateRequest{OverlayUserRequest: overlaysrpc.OverlayUserRequest{UserID: "1001"}, Template: "powered"})
	owned := overlaysrpc.OverlayRefRequest{OverlayUserRequest: overlaysrpc.OverlayUserRequest{UserID: "1001"}, ID: created.ID}
	foreign := overlaysrpc.OverlayRefRequest{OverlayUserRequest: overlaysrpc.OverlayUserRequest{UserID: "1002"}, ID: created.ID}
	invalid := overlaysrpc.DefaultPoweredConfig()
	invalid.Directions = nil

	cases := []struct {
		name    string
		subject string
		req     any
		want    domainrpc.Code
	}{
		{"missing user", overlaysrpc.OverlayListSubject, overlaysrpc.OverlayUserRequest{}, domainrpc.CodeInvalid},
		{"unknown template", overlaysrpc.OverlayAddTemplateSubject, overlaysrpc.OverlayAddTemplateRequest{OverlayUserRequest: owned.OverlayUserRequest, Template: "nope"}, domainrpc.CodeInvalid},
		{"another account", overlaysrpc.OverlayGetSubject, foreign, domainrpc.CodeNotFound},
		{"stale revision", overlaysrpc.OverlaySaveSubject, overlaysrpc.OverlaySaveRequest{OverlayRefRequest: owned, Config: overlaysrpc.DefaultPoweredConfig(), Revision: 9}, domainrpc.CodeConflict},
		{"invalid config", overlaysrpc.OverlaySaveSubject, overlaysrpc.OverlaySaveRequest{OverlayRefRequest: owned, Config: invalid, Revision: created.Revision}, domainrpc.CodeInvalid},
		{"saved", overlaysrpc.OverlaySaveSubject, overlaysrpc.OverlaySaveRequest{OverlayRefRequest: owned, Config: overlaysrpc.DefaultPoweredConfig(), Revision: created.Revision}, domainrpc.CodeOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reply := request[overlaysrpc.OverlayView](t, nc, tc.subject, tc.req)
			require.Equal(t, tc.want, reply.Code, reply.Error)
		})
	}
}

func TestOwnerOverlayLookupFollowsTheCurrentLink(t *testing.T) {
	nc := serve(t)
	created := request[overlaysrpc.OverlayView](t, nc, overlaysrpc.OverlayAddTemplateSubject,
		overlaysrpc.OverlayAddTemplateRequest{OverlayUserRequest: overlaysrpc.OverlayUserRequest{UserID: "1001"}, Template: "powered"})
	hash := func(v overlaysrpc.OverlayView) overlaysrpc.TokenHash {
		return overlaysrpc.HashToken(v.URL[len(overlaysrpc.OverlayURLPrefix):])
	}
	rotated := request[overlaysrpc.OverlayView](t, nc, overlaysrpc.OverlayRotateSubject,
		overlaysrpc.OverlayRefRequest{OverlayUserRequest: overlaysrpc.OverlayUserRequest{UserID: "1001"}, ID: created.ID})

	cases := []struct {
		name string
		hash overlaysrpc.TokenHash
		want domainrpc.Code
	}{
		{"current link", hash(rotated), domainrpc.CodeOK},
		{"replaced link", hash(created), domainrpc.CodeNotFound},
		{"malformed hash", "abc", domainrpc.CodeNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reply := request[overlaysrpc.OwnerPoweredReply](t, nc, overlaysrpc.OwnerPoweredByTokenSubject, overlaysrpc.OwnerPoweredByTokenRequest{TokenHash: tc.hash})
			require.Equal(t, tc.want, reply.Code, reply.Error)
			require.Equal(t, tc.want == domainrpc.CodeOK, reply.Unified != nil)
		})
	}
}
