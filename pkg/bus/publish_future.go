// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"context"
	"errors"
)

type PublishFuture struct{ request publishRequest }

func (f PublishFuture) Await(ctx context.Context) error {
	return awaitPublishConfirmation(ctx, f.request)
}

type AsyncPublisher interface {
	BeginConfirmed(context.Context, Publication) (PublishFuture, error)
}

func BeginConfirmed(ctx context.Context, pub Publisher, publication Publication) (PublishFuture, error) {
	if publication.ID == "" {
		return PublishFuture{}, errors.New("bus: confirmed publish requires a message ID")
	}
	if async, ok := pub.(AsyncPublisher); ok {
		return async.BeginConfirmed(ctx, publication)
	}
	request := publishRequest{confirmed: getConfirmChan()}
	request.confirmed <- PublishConfirmed(ctx, pub, publication)
	return PublishFuture{request: request}, nil
}

func (p *publisherPool) BeginConfirmed(ctx context.Context, publication Publication) (PublishFuture, error) {
	stream, err := p.streamFor(publication.Subject)
	if err != nil {
		return PublishFuture{}, err
	}
	command := publishCommand{ctx: ctx, stream: stream, topic: publication.Subject, msgID: publication.ID,
		payload: append([]byte(nil), publication.Payload...), confirmed: true}
	request := newPublishRequest(command, publishMessage(command))
	if err := p.connectionFor(command).admit(ctx, stream, request); err != nil {
		releaseUnadmittedRequest(request)
		return PublishFuture{}, err
	}
	return PublishFuture{request: request}, nil
}
