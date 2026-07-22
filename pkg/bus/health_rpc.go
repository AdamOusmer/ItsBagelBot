package bus

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/nats-io/nats.go"
)

// RPCHealthPrefix is a fleet-wide, side-effect-free request/reply surface used
// by the admin Analytics page. Keeping it separate from business RPCs means a
// latency sample never reads a database, calls Twitch, or mutates state.
const RPCHealthPrefix = "bagel.rpc.health"

// RPCHealthReply is intentionally tiny: the useful measurement is the NATS
// round trip at the caller, while the body confirms which service answered.
type RPCHealthReply struct {
	Service string `json:"service"`
	OK      bool   `json:"ok"`
}

func RPCHealthSubject(service string) string {
	return RPCHealthPrefix + "." + service
}

// SubscribeRPCHealth registers one queue-balanced no-op responder for a
// service. It uses the service's existing RPC connection, so a successful
// response covers the same leaf route, account import/export, connection and
// subscriber dispatch path as its real RPC handlers.
func SubscribeRPCHealth(nc *nats.Conn, service, queueGroup string) error {
	if service == "" || strings.ContainsAny(service, ".*> \t\r\n") {
		return fmt.Errorf("invalid rpc health service token %q", service)
	}
	if queueGroup == "" {
		return fmt.Errorf("rpc health queue group is required")
	}

	reply, err := json.Marshal(RPCHealthReply{Service: service, OK: true})
	if err != nil {
		return fmt.Errorf("marshal rpc health reply: %w", err)
	}

	subject := RPCHealthSubject(service)
	if err := QueueSubscribeRPC(nc, subject, queueGroup, func(msg *nats.Msg) {
		_ = msg.Respond(reply)
	}); err != nil {
		return fmt.Errorf("subscribe %s: %w", subject, err)
	}
	if err := nc.Flush(); err != nil {
		return fmt.Errorf("flush subscription %s: %w", subject, err)
	}
	return nil
}
