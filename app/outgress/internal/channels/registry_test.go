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

func TestPauseSnapshotRejectsOlderVersion(t *testing.T) {
	r := &Registry{}
	r.applyPauseSnapshot(pauseSnapshot{paused: true, version: 4, observedAt: time.Now()})
	r.applyPauseSnapshot(pauseSnapshot{paused: false, version: 3, observedAt: time.Now()})

	paused, err := r.Paused(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !paused {
		t.Fatal("older snapshot reverted paused state")
	}
}

func TestPauseSnapshotSameVersionRepairsLegacyWriter(t *testing.T) {
	r := &Registry{}
	r.applyPauseSnapshot(pauseSnapshot{paused: false, version: 2, observedAt: time.Now()})
	r.applyPauseSnapshot(pauseSnapshot{paused: true, version: 2, observedAt: time.Now()})

	paused, err := r.Paused(context.Background())
	if err != nil || !paused {
		t.Fatalf("paused/error = %v/%v, want true/nil", paused, err)
	}
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

// The registry only ever reads state it just wrote: Get reloads the hash whose
// cache applyChannelUpdate invalidated, List reads the index set the same
// pipeline SADDed, and EnrollCooldownActive checks the key ArmEnrollCooldown
// set. Served by a lagging node-local replica those reads re-cache the
// pre-write value and bypass the cooldown, so the constructor must pin them.
func TestNewPinsRegistryReadsToThePrimary(t *testing.T) {
	if !pkg_valkey.IsPrimary(New(nil).client) {
		t.Fatal("registry reads are served by the node-local replica; they read back its own writes")
	}
}

// TestSaveCoversEveryChannelField is the guard against the silent-revert bug
// class: Save is a full overwrite, so any persisted field of manage.Channel it
// forgets is quietly reset to empty on the next Save. That is how a grant-death
// marker would disappear when an operator toggles "enabled" in the admin
// console, with nothing logged and no test failing.
//
// broadcaster_id is excluded because it is the Valkey key, not a hash field.
func TestSaveCoversEveryChannelField(t *testing.T) {
	written := savedFields(manage.Channel{})

	typ := reflect.TypeOf(manage.Channel{})
	for i := range typ.NumField() {
		tag := typ.Field(i).Tag.Get("json")
		name, _, _ := strings.Cut(tag, ",")
		if name == "" || name == "-" || name == "broadcaster_id" {
			continue
		}
		if _, ok := written[name]; !ok {
			t.Errorf("manage.Channel field %q is never written by Save, so it silently "+
				"reverts to empty on every full overwrite", name)
		}
	}
}

// TestSaveRoundTripsGrantState pins the field the beacon reads.
func TestSaveRoundTripsGrantState(t *testing.T) {
	if got := savedFields(manage.Channel{GrantState: manage.GrantDead})["grant_state"]; got != "dead" {
		t.Errorf("grant_state = %q, want %q", got, "dead")
	}
	if got := savedFields(manage.Channel{})["grant_state"]; got != "" {
		t.Errorf("healthy grant_state = %q, want empty", got)
	}
}
