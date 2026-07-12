package worker

import "testing"

// The three lane workers are built from one Config with a shared UserIDs cache
// (see newLaneWorkers). They must reuse that single instance so the login->id
// keyspace is not duplicated once per lane in the pod.
func TestLaneWorkersShareInjectedUserIDCache(t *testing.T) {
	shared := NewUserIDCache()
	base := Config{UserIDs: shared}

	lanes := map[string]*Worker{
		"premium":  New(base),
		"standard": New(base),
		"system":   New(base),
	}
	for lane, w := range lanes {
		if w.userIDs != shared {
			t.Fatalf("%s worker did not reuse the injected login->id cache", lane)
		}
	}
}

// A Config without an injected cache still yields a usable cache so a standalone
// worker never nil-derefs on the shoutout resolution path.
func TestNewFallsBackToPrivateUserIDCache(t *testing.T) {
	w := New(Config{})
	if w.userIDs == nil {
		t.Fatal("worker without an injected cache must still have a usable login->id cache")
	}
}
