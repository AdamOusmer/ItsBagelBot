// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bridge

import (
	"context"
	"errors"
	"testing"
)

func TestMigrationTargetBoundary(t *testing.T) {
	r := Runtime{config: Config{Endpoint: "https://bridge.example"}}
	for _, target := range []string{"http://bridge.example", "https://bridge.example?", "https://bridge.example/path", "https://evil.example", "https://bridge.example:444", "https://user@bridge.example", "https://bridge.example?token=secret", "https://bridge.example#secret"} {
		if r.setTarget(target) {
			t.Fatalf("accepted untrusted migration target %q", target)
		}
	}
	if !r.setTarget("https://bridge.example") {
		t.Fatal("trusted target rejected")
	}
	if !r.setTarget("") || r.target != r.config.Endpoint {
		t.Fatal("default target did not restore configured endpoint")
	}
}

func TestNativeTokenErrorClassification(t *testing.T) {
	for _, tc := range []struct {
		err            error
		authentication bool
	}{
		{ErrAuthentication, true}, {context.DeadlineExceeded, false}, {errors.New("temporary outage"), false},
	} {
		r := Runtime{config: Config{Tokens: runtimeTokenFunc(func(context.Context) (string, error) { return "", tc.err })}}
		_, err := r.nativeToken(context.Background())
		authentication := errors.Is(err, ErrAuthentication)
		if err == nil || authentication != tc.authentication {
			t.Fatal("transient token failures must not force sign-in")
		}
	}
}

func TestAdvancingReceiveAllocations(t *testing.T) {
	r := preparedRuntime(t)
	id := r.machine.Snapshot().Primary
	m := runtimeEvent(1)
	defer m.ReturnToVTPool()
	allocations := testing.AllocsPerRun(1000, func() {
		m.Event.Sequence = r.Status().Cursor + 1
		r.receive(context.Background(), notice{id: id, kind: frame, message: m})
	})
	if allocations != 0 {
		t.Fatalf("typedreceive allocated %g times", allocations)
	}
	if r.Status().Cursor < 1000 {
		t.Fatal("receive did not advance")
	}
}
