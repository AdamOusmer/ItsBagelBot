package engine

import (
	"context"
	"errors"
	"strconv"
	"time"

	modulesrpc "ItsBagelBot/internal/domain/rpc/modules"
	"ItsBagelBot/pkg/bus"

	"github.com/nats-io/nats.go"
)

const quotesRPCTimeout = 2 * time.Second

// QuotesRPC implements QuotesStore by forwarding to the modules service's
// channel-quotes RPC over NATS request/reply. The rows live with the modules
// service (the quotes feature is a module), so sesame reads and writes them
// through bagel.rpc.modules.quote.<verb>.
type QuotesRPC struct {
	nc     *nats.Conn
	prefix string // e.g. "bagel.rpc.modules.quote"
}

// NewQuotesRPC returns a QuotesStore backed by the modules quotes RPC. prefix
// is the modules RPC subject prefix (default "bagel.rpc.modules"); the client
// appends ".quote.<verb>".
func NewQuotesRPC(nc *nats.Conn, modulesPrefix string) *QuotesRPC {
	return &QuotesRPC{nc: nc, prefix: modulesPrefix + ".quote"}
}

// call requests one quote verb and surfaces a reply-envelope error as a plain
// error, mirroring the other RPC clients.
func (c *QuotesRPC) call(ctx context.Context, verb string, req modulesrpc.QuoteRequest) (modulesrpc.QuoteReply, error) {
	reply, err := bus.RequestJSONTimeout[modulesrpc.QuoteReply](ctx, c.nc, c.prefix+"."+verb, req, quotesRPCTimeout)
	if err != nil {
		return modulesrpc.QuoteReply{}, err
	}
	if reply.Error != "" {
		return modulesrpc.QuoteReply{}, errors.New(reply.Error)
	}
	return reply, nil
}

// QuoteAdd saves a new quote and returns it with its assigned number.
func (c *QuotesRPC) QuoteAdd(ctx context.Context, broadcasterID uint64, text, addedBy string) (modulesrpc.Quote, error) {
	reply, err := c.call(ctx, "add", modulesrpc.QuoteRequest{
		UserID:  strconv.FormatUint(broadcasterID, 10),
		Text:    text,
		AddedBy: addedBy,
	})
	if err != nil {
		return modulesrpc.Quote{}, err
	}
	if reply.Quote == nil {
		return modulesrpc.Quote{}, errors.New("quote add: empty reply")
	}
	return *reply.Quote, nil
}

// QuoteGet returns quote #number; found=false when it does not exist.
func (c *QuotesRPC) QuoteGet(ctx context.Context, broadcasterID, number uint64) (modulesrpc.Quote, bool, error) {
	reply, err := c.call(ctx, "get", modulesrpc.QuoteRequest{
		UserID: strconv.FormatUint(broadcasterID, 10),
		Number: number,
	})
	return foundQuote(reply, err)
}

// QuoteRandom returns a random quote; found=false when none are saved.
func (c *QuotesRPC) QuoteRandom(ctx context.Context, broadcasterID uint64) (modulesrpc.Quote, bool, error) {
	reply, err := c.call(ctx, "random", modulesrpc.QuoteRequest{
		UserID: strconv.FormatUint(broadcasterID, 10),
	})
	return foundQuote(reply, err)
}

// foundQuote unwraps a get/random reply: an error, an absent row, or a nil
// payload all collapse to (zero, false, err); a present row is (quote, true).
func foundQuote(reply modulesrpc.QuoteReply, err error) (modulesrpc.Quote, bool, error) {
	if err != nil {
		return modulesrpc.Quote{}, false, err
	}
	if reply.Quote == nil {
		return modulesrpc.Quote{}, false, nil
	}
	return *reply.Quote, reply.Found, nil
}

// QuoteRemove deletes quote #number; found=false when it did not exist.
func (c *QuotesRPC) QuoteRemove(ctx context.Context, broadcasterID, number uint64) (bool, error) {
	reply, err := c.call(ctx, "remove", modulesrpc.QuoteRequest{
		UserID: strconv.FormatUint(broadcasterID, 10),
		Number: number,
	})
	if err != nil {
		return false, err
	}
	return reply.Found, nil
}
