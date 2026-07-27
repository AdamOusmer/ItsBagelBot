package main

import (
	"testing"
	"time"

	"github.com/bytedance/sonic"
)

func TestPadForLandsNearTheRequestedSize(t *testing.T) {
	for _, target := range []int{256, 512, 1024, 4096} {
		event := envelope{
			EventID: eventID("stress-publisher-00", 3, 1234),
			Pub:     "stress-publisher-00", Lane: 3, Seq: 1234,
			Class: classTreatment, SentAt: time.Now().UnixNano(),
			Pad: padFor(target),
		}
		body, err := sonic.ConfigFastest.Marshal(&event)
		if err != nil {
			t.Fatal(err)
		}
		// The probe is sized for the widest field values, so a real event lands at
		// or just under the target; it must never overshoot, which would silently
		// change the byte rate the run is reported against.
		if len(body) > target || len(body) < target-64 {
			t.Fatalf("target %d produced %d bytes", target, len(body))
		}
	}
}

func TestPadForRefusesImpossibleTargets(t *testing.T) {
	if got := padFor(0); got != "" {
		t.Fatalf("a zero target must not pad, got %d bytes", len(got))
	}
	if got := padFor(4); got != "" {
		t.Fatalf("a target under the envelope overhead must not pad, got %d bytes", len(got))
	}
}

func TestEventIDIsStableAndLaneScoped(t *testing.T) {
	if eventID("p0", 1, 7) != eventID("p0", 1, 7) {
		t.Fatal("the same event must produce the same id, or a duplicate is unrecognisable")
	}
	if eventID("p0", 1, 7) == eventID("p0", 2, 7) {
		t.Fatal("two lanes at the same sequence are two events")
	}
	if eventID("p0", 1, 7) == eventID("p1", 1, 7) {
		t.Fatal("two publisher replicas at the same sequence are two events")
	}
}

func TestLaneKeySeparatesReplicas(t *testing.T) {
	if laneKey("p0", 1) == laneKey("p1", 1) {
		t.Fatal("replicas must not share a sequence cursor")
	}
}

func TestEnvelopeRoundTrips(t *testing.T) {
	want := envelope{EventID: "p0-1-7", Pub: "p0", Lane: 1, Seq: 7, Class: classControl, SentAt: 42, Pad: "xx"}
	body, err := sonic.ConfigFastest.Marshal(&want)
	if err != nil {
		t.Fatal(err)
	}
	var got envelope
	if err := sonic.ConfigFastest.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("round trip lost fields: %+v want %+v", got, want)
	}
}
