package bus

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
	"go.uber.org/zap"
)

const defaultRPCTimeout = 5 * time.Second

// RPCReplyError is returned when the peer answers with a JSON {"error": "..."}
// payload. Most existing RPC contracts use that shape, including the TS client.
type RPCReplyError struct {
	Subject string
	Message string
}

func (e RPCReplyError) Error() string {
	if e.Subject == "" {
		return e.Message
	}
	return fmt.Sprintf("rpc %s: %s", e.Subject, e.Message)
}

// RequestJSON performs a core NATS request/reply using a JSON request body and
// JSON response body. It also normalizes the fleet's conventional {"error": ""}
// reply into a Go error so callers do not accidentally treat failed replies as
// zero-valued success.
func RequestJSON[T any](ctx context.Context, nc *nats.Conn, subject string, request any) (T, error) {
	var zero T

	body, err := json.Marshal(request)
	if err != nil {
		return zero, fmt.Errorf("rpc %s marshal request: %w", subject, err)
	}

	msg, err := nc.RequestWithContext(ctx, subject, body)
	if err != nil {
		return zero, fmt.Errorf("rpc %s request: %w", subject, err)
	}

	if msg := rpcErrorMessage(msg.Data); msg != "" {
		return zero, RPCReplyError{Subject: subject, Message: msg}
	}

	var reply T
	if err := json.Unmarshal(msg.Data, &reply); err != nil {
		return zero, fmt.Errorf("rpc %s unmarshal reply: %w", subject, err)
	}
	return reply, nil
}

// RequestJSONTimeout is RequestJSON with a local timeout layered onto ctx.
func RequestJSONTimeout[T any](ctx context.Context, nc *nats.Conn, subject string, request any, timeout time.Duration) (T, error) {
	if timeout <= 0 {
		timeout = defaultRPCTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return RequestJSON[T](ctx, nc, subject, request)
}

// QueueSubscribeJSON registers a queue RPC handler with common JSON decode,
// timeout, response, slow-call logging and subscription flushing behavior.
func QueueSubscribeJSON[Req any, Resp any](
	nc *nats.Conn,
	subject string,
	queueGroup string,
	timeout time.Duration,
	log *zap.Logger,
	handle func(context.Context, Req) Resp,
) error {
	if timeout <= 0 {
		timeout = defaultRPCTimeout
	}

	_, err := nc.QueueSubscribe(subject, queueGroup, func(msg *nats.Msg) {
		start := time.Now()

		var req Req
		// Empty bodies are allowed for no-argument RPCs; handlers validate any
		// required fields on the zero-value request.
		if len(msg.Data) > 0 {
			if err := json.Unmarshal(msg.Data, &req); err != nil {
				respondAndLog(msg, subject, start, log, map[string]string{"error": "bad request"})
				return
			}
		}

		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()

		respondAndLog(msg, subject, start, log, handle(ctx, req))
	})
	if err != nil {
		return fmt.Errorf("subscribe %s: %w", subject, err)
	}
	if err := nc.Flush(); err != nil {
		return fmt.Errorf("flush subscription %s: %w", subject, err)
	}
	return nil
}

func respondAndLog(msg *nats.Msg, subject string, start time.Time, log *zap.Logger, reply any) {
	elapsed := time.Since(start)
	if err := Respond(msg, reply); err != nil && log != nil {
		log.Warn("rpc respond failed", zap.String("subject", subject), zap.Duration("elapsed", elapsed), zap.Error(err))
		return
	}
	if elapsed > 250*time.Millisecond && log != nil {
		log.Debug("slow rpc handler", zap.String("subject", subject), zap.Duration("elapsed", elapsed))
	}
}

func rpcErrorMessage(data []byte) string {
	var envelope struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return ""
	}
	return envelope.Error
}
