// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"fmt"
	"reflect"

	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/event/lane"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/codec"
)

// PrepareJSON compiles the hot envelope, output and cold projection codecs
// before the consumer starts. Nested RawMessage module/event bodies still
// compile when their particular handler first uses them.
func PrepareJSON() error {
	if err := codec.Pretouch(
		reflect.TypeOf(lane.Envelope{}),
		reflect.TypeOf(outgress.Message{}),
		reflect.TypeOf(outgress.Batch{}),
	); err != nil {
		return err
	}
	if err := projection.PrepareJSON(); err != nil {
		return err
	}
	// Builders own several anonymous payload types. Exercise their pure
	// construction once so the exact types used at runtime compile too.
	for typ, build := range outgressBuilders {
		if _, err := build(&module.Output{Type: typ}); err != nil {
			return fmt.Errorf("prepare %s output JSON: %w", typ, err)
		}
	}
	return nil
}
