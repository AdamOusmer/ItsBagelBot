package bus

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

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
	body, err := json.Marshal(request)
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
	msg, err := nc.RequestMsgWithContext(ctx, requestMsg)
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
	if err := json.Unmarshal(msg.Data, &reply); err != nil {
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

	_, err := nc.QueueSubscribe(subject, queueGroup, func(msg *nats.Msg) {
		start := time.Now()

		txn := app.StartTransaction("rpc " + normalizedDestination(subject))
		defer txn.End()
		acceptTraceHeaders(txn, msg.Header)
		addMessagingTransactionAttributes(txn, messagingAttributes{operation: "process", destination: subject})

		var req Req
		// Empty bodies are allowed for no-argument RPCs; handlers validate any
		// required fields on the zero-value request.
		if len(msg.Data) > 0 {
			decodeSegment := txn.StartSegment("rpc.decode")
			if err := json.Unmarshal(msg.Data, &req); err != nil {
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

func rpcErrorMessage(data []byte) string {
	var envelope struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return ""
	}
	return envelope.Error
}
