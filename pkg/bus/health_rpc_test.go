package bus

import "testing"

func TestRPCHealthSubject(t *testing.T) {
	if got := RPCHealthSubject("projector"); got != "bagel.rpc.health.projector" {
		t.Fatalf("RPCHealthSubject() = %q", got)
	}
}

func TestSubscribeRPCHealthRejectsInvalidTokensBeforeDial(t *testing.T) {
	for _, service := range []string{"", "two.tokens", "wildcard.*", "has space"} {
		if err := SubscribeRPCHealth(nil, service, "health"); err == nil {
			t.Fatalf("SubscribeRPCHealth accepted invalid service %q", service)
		}
	}
	if err := SubscribeRPCHealth(nil, "users", ""); err == nil {
		t.Fatal("SubscribeRPCHealth accepted an empty queue group")
	}
}
