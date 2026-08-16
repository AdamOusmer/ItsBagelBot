package bus

import (
	"bytes"
	"context"
	"fmt"
	"time"

	"ItsBagelBot/pkg/codec"
	"ItsBagelBot/pkg/monitor"

	"github.com/nats-io/nats.go"
	"github.com/newrelic/go-agent/v3/newrelic"
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

	encodeSegment := startMessagingSegment(ctx, messagingSpan{
		name: "rpc.request.encode", operation: "request", destination: subject,
	})
	body, err := codec.Marshal(request)
	endMessagingSegment(encodeSegment, err)
	if err != nil {
		return zero, fmt.Errorf("rpc %s marshal request: %w", subject, err)
	}

	requestMsg := nats.NewMsg(subject)
	requestMsg.Data = body
	insertTraceHeaders(ctx, requestMsg)

	segment := startMessagingSegment(ctx, messagingSpan{
		name: "nats.request", operation: "request", destination: subject,
	})
	msg, err := RequestMsgWithContext(ctx, nc, requestMsg)
	endMessagingSegment(segment, err)
	if err != nil {
		return zero, fmt.Errorf("rpc %s request: %w", subject, err)
	}

	if errorMessage := rpcErrorMessage(msg.Data); errorMessage != "" {
		return zero, RPCReplyError{Subject: subject, Message: errorMessage}
	}

	decodeSegment := startMessagingSegment(ctx, messagingSpan{
		name: "rpc.response.decode", operation: "request", destination: subject,
	})
	var reply T
	if err := codec.Unmarshal(msg.Data, &reply); err != nil {
		endMessagingSegment(decodeSegment, err)
		return zero, fmt.Errorf("rpc %s unmarshal reply: %w", subject, err)
	}
	endMessagingSegment(decodeSegment, nil)
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
//
// handle runs inline on the subscription's delivery goroutine, so requests on a
// subscription are answered one at a time. Moving this path onto an RPCPool
// waits on the read-modify-write handlers behind it (outgress channel.set, users
// ApplyBilling) and on the DB_MAX_OPEN_CONNS=4 budget every database service
// ships, either of which would turn added concurrency into lost updates or
// gate-blocked handlers rather than throughput.
func QueueSubscribeJSON[Req any, Resp any](
	nc *nats.Conn,
	subject string,
	queueGroup string,
	timeout time.Duration,
	app *newrelic.Application,
	log *zap.Logger,
	handle func(context.Context, Req) Resp,
) error {
	if timeout <= 0 {
		timeout = defaultRPCTimeout
	}

	err := QueueSubscribeRPC(nc, subject, queueGroup, func(msg *nats.Msg) {
		start := time.Now()

		txn := app.StartTransaction("rpc " + normalizedDestination(subject))
		defer txn.End()
		acceptTraceHeaders(txn, msg.Header)
		addMessagingTransactionAttributes(txn, messagingAttributes{operation: "process", destination: subject})
		log := monitor.TraceLogger(txn, log)

		var req Req
		// Empty bodies are allowed for no-argument RPCs; handlers validate any
		// required fields on the zero-value request.
		if len(msg.Data) > 0 {
			decodeSegment := txn.StartSegment("rpc.decode")
			if err := codec.Unmarshal(msg.Data, &req); err != nil {
				decodeSegment.AddAttribute(resultAttribute, "invalid")
				decodeSegment.End()
				txn.NoticeError(err)
				txn.AddAttribute(resultAttribute, "invalid")
				respondAndLog(msg, subject, start, log, txn, map[string]string{"error": "bad request"})
				return
			}
			decodeSegment.AddAttribute(resultAttribute, "ok")
			decodeSegment.End()
		}

		ctx, cancel := context.WithTimeout(newrelic.NewContext(context.Background(), txn), timeout)
		defer cancel()

		handleSegment := txn.StartSegment("rpc.handler")
		reply := handle(ctx, req)
		handleSegment.End()
		respondAndLog(msg, subject, start, log, txn, reply)
	})
	if err != nil {
		return fmt.Errorf("subscribe %s: %w", subject, err)
	}
	if err := nc.Flush(); err != nil {
		return fmt.Errorf("flush subscription %s: %w", subject, err)
	}
	return nil
}

func respondAndLog(msg *nats.Msg, subject string, start time.Time, log *zap.Logger, txn *newrelic.Transaction, reply any) {
	elapsed := time.Since(start)
	encodeSegment := txn.StartSegment("rpc.reply.encode")
	body, err := marshalResponse(reply)
	encodeSegment.AddAttribute(resultAttribute, messagingResult(err))
	encodeSegment.End()
	if err != nil {
		txn.AddAttribute(resultAttribute, "error")
		txn.NoticeError(err)
		if log != nil {
			log.Warn("rpc encode reply failed", zap.String("subject", subject), zap.Duration("elapsed", elapsed), zap.Error(err))
		}
		return
	}
	segment := txn.StartSegment("nats.reply")
	segment.AddAttribute(messagingSystemAttribute, "nats")
	segment.AddAttribute(messagingOperationAttribute, "reply")
	segment.AddAttribute(messagingDestinationAttribute, normalizedDestination(subject))
	err = sendResponse(msg, body)
	segment.AddAttribute(resultAttribute, messagingResult(err))
	segment.End()
	txn.AddAttribute(resultAttribute, messagingResult(err))
	if err != nil && log != nil {
		txn.NoticeError(err)
		log.Warn("rpc respond failed", zap.String("subject", subject), zap.Duration("elapsed", elapsed), zap.Error(err))
		return
	}
	if elapsed > 250*time.Millisecond && log != nil {
		log.Debug("slow rpc handler", zap.String("subject", subject), zap.Duration("elapsed", elapsed))
	}
}

// errorFieldProbe is the shortest byte sequence any {"error": "..."} reply must
// contain. A reply without it cannot be an error envelope, whatever its shape.
var errorFieldProbe = []byte(`"error"`)

// ReplyErrorMessage reports the message carried by the fleet's conventional
// {"error": "..."} reply envelope, or "" when the reply is an ordinary payload.
// Callers that own their own request/reply loop use it instead of decoding an
// error envelope themselves.
func ReplyErrorMessage(data []byte) string { return rpcErrorMessage(data) }

// rpcErrorMessage answers the same question without parsing the common case.
// Replies are overwhelmingly successes, and a success carries no "error" key at
// all, so a byte scan rejects it outright. Only a reply that could hold the key
// pays a decode. Without the scan every reply in the fleet was parsed twice:
// once into this envelope and once into the caller's own type.
func rpcErrorMessage(data []byte) string {
	if !bytes.Contains(data, errorFieldProbe) {
		return ""
	}
	var envelope struct {
		Error string `json:"error"`
	}
	if err := codec.Unmarshal(data, &envelope); err != nil {
		return ""
	}
	return envelope.Error
}
