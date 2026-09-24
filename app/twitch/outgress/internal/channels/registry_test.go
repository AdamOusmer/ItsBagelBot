// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package channels

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"ItsBagelBot/internal/domain/rpc/manage"
	pkg_valkey "ItsBagelBot/pkg/valkey"
)

func TestPauseSnapshotOrdering(t *testing.T) {
	tests := []struct {
		name    string
		first   pauseSnapshot
		second  pauseSnapshot
		wantErr bool
	}{
		{
			name:   "older version does not revert paused state",
			first:  pauseSnapshot{paused: true, version: 4},
			second: pauseSnapshot{paused: false, version: 3},
		},
		{
			name:   "same version repairs a legacy writer",
			first:  pauseSnapshot{paused: false, version: 2},
			second: pauseSnapshot{paused: true, version: 2},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := &Registry{}
			r.applyPauseSnapshot(stamp(tc.first))
			r.applyPauseSnapshot(stamp(tc.second))

			paused, err := r.Paused(context.Background())
			if err != nil {
				t.Fatalf("Paused() error = %v", err)
			}
			if !paused {
				t.Fatal("paused = false, want true")
			}
		})
	}
}

func stamp(s pauseSnapshot) pauseSnapshot {
	s.observedAt = time.Now()
	return s
}

func TestPausedFailsClosedWhenSnapshotIsStale(t *testing.T) {
	r := &Registry{}
	r.applyPauseSnapshot(pauseSnapshot{observedAt: time.Now().Add(-pauseMaxAge - time.Second)})

	_, err := r.Paused(context.Background())
	if !errors.Is(err, ErrPauseStateUnavailable) {
		t.Fatalf("error = %v, want ErrPauseStateUnavailable", err)
	}
}

func TestPauseReconcileDelayIsJitteredWithinBounds(t *testing.T) {
	for range 100 {
		delay := nextPauseReconcileDelay()
		if delay < pauseReconcileInterval-pauseReconcileJitter/2 ||
			delay >= pauseReconcileInterval+pauseReconcileJitter/2 {
			t.Fatalf("delay %s is outside jitter bounds", delay)
		}
	}
}

func TestNewPinsRegistryReadsToThePrimary(t *testing.T) {
	if !pkg_valkey.IsPrimary(New(nil).client) {
		t.Fatal("registry reads are served by the node-local replica; they read back its own writes")
	}
}

func TestSaveCoversEveryChannelField(t *testing.T) {
	notPersisted := map[string]bool{"": true, "-": true, "broadcaster_id": true}

	written := savedFields(manage.Channel{})

	typ := reflect.TypeOf(manage.Channel{})
	for i := range typ.NumField() {
		tag := typ.Field(i).Tag.Get("json")
		name, _, _ := strings.Cut(tag, ",")
		if notPersisted[name] {
			continue
		}
		if _, ok := written[name]; !ok {
			t.Errorf("manage.Channel field %q is never written by Save, so it silently "+
				"reverts to empty on every full overwrite", name)
		}
	}
}

func TestSaveRoundTripsGrantState(t *testing.T) {
	tests := []struct {
		name    string
		channel manage.Channel
		want    string
	}{
		{"dead", manage.Channel{GrantState: manage.GrantDead}, "dead"},
		{"healthy", manage.Channel{}, ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := savedFields(tc.channel)["grant_state"]; got != tc.want {
				t.Errorf("grant_state = %q, want %q", got, tc.want)
			}
		})
	}
}
