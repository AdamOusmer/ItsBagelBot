package bus

import (
	"errors"
	"reflect"
	"testing"

	"github.com/nats-io/nats.go"
)

func TestRPCSubjectsForNode(t *testing.T) {
	tests := []struct {
		name string
		node string
		want []string
	}{
		{name: "production node", node: "node2", want: []string{"bagel.rpc.users.get", "bagel.rpc.users.get.node.node2"}},
		{name: "worker node", node: "worker-1", want: []string{"bagel.rpc.users.get", "bagel.rpc.users.get.node.worker-1"}},
		{name: "local development", want: []string{"bagel.rpc.users.get"}},
		{name: "multi token rejected", node: "zone.node2", want: []string{"bagel.rpc.users.get"}},
		{name: "wildcard rejected", node: "node*", want: []string{"bagel.rpc.users.get"}},
		{name: "whitespace rejected", node: "node 2", want: []string{"bagel.rpc.users.get"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := rpcSubjectsForNode(rpcSubject("bagel.rpc.users.get"), rpcNodeName(tt.node)); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("rpcSubjectsForNode() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRPCRequestSubjectsAreLocalFirst(t *testing.T) {
	t.Setenv("NODE_NAME", "node2")
	want := []string{"bagel.rpc.users.get.node.node2", "bagel.rpc.users.get"}
	if got := rpcRequestSubjects(rpcSubject("bagel.rpc.users.get")); !reflect.DeepEqual(got, want) {
		t.Fatalf("rpcRequestSubjects() = %v, want %v", got, want)
	}
}

func TestRequestLocalFirstFallsBackOnlyWithoutResponders(t *testing.T) {
	local := "bagel.rpc.users.get.node.node2"
	generic := "bagel.rpc.users.get"
	var called []string
	request := func(subject string) (*nats.Msg, error) {
		called = append(called, subject)
		if subject == local {
			return nil, nats.ErrNoResponders
		}
		return &nats.Msg{Subject: generic}, nil
	}

	msg, err := requestLocalFirst([]string{local, generic}, request)
	if err != nil {
		t.Fatal(err)
	}
	if msg.Subject != generic || !reflect.DeepEqual(called, []string{local, generic}) {
		t.Fatalf("fallback result = (%q, %v)", msg.Subject, called)
	}
}

func TestRequestLocalFirstDoesNotReplayAmbiguousFailure(t *testing.T) {
	want := errors.New("timeout after delivery")
	called := 0
	request := func(string) (*nats.Msg, error) {
		called++
		return nil, want
	}

	_, err := requestLocalFirst([]string{"rpc.write.node.node2", "rpc.write"}, request)
	if !errors.Is(err, want) || called != 1 {
		t.Fatalf("requestLocalFirst() = (%v, %d calls), want original error and one call", err, called)
	}
}
