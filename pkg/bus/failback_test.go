package bus

import "testing"

func TestIsLocalLeaf(t *testing.T) {
	tests := []struct {
		name   string
		server string
		node   string
		want   bool
	}{
		{name: "same node", server: "node1--nats-leaf-abc", node: "node1", want: true},
		{name: "other node", server: "node2--nats-leaf-def", node: "node1", want: false},
		{name: "prefix collision", server: "node10--nats-leaf-def", node: "node1", want: false},
		{name: "disabled without node", server: "node1--nats-leaf-abc", node: "", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isLocalLeaf(tt.server, tt.node); got != tt.want {
				t.Fatalf("isLocalLeaf(%q, %q) = %v, want %v", tt.server, tt.node, got, tt.want)
			}
		})
	}
}
