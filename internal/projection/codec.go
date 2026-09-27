// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package projection

import (
	"reflect"

	"ItsBagelBot/pkg/codec"
)

// PrepareJSON compiles the exact cold-load RPC request/reply and projected-row
// codecs before a worker starts consuming messages.
func PrepareJSON() error {
	return codec.Pretouch(
		reflect.TypeFor[map[string]string](),
		reflect.TypeFor[User](),
		reflect.TypeFor[modulesReply](),
		reflect.TypeFor[commandsReply](),
		reflect.TypeFor[fetchesReply](),
		reflect.TypeFor[CommandView](),
		reflect.TypeFor[FetchView](),
	)
}
