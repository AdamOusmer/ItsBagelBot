// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package natsacl

import (
	"fmt"

	"github.com/nats-io/jwt/v2"
)

const hubDomainMapping = "hub-domain"

// Mirrors generateJSMappingTable in nats-server for domain "hub": without it
// a reload refuses hub-domain JetStream calls until the server re-adds its own.
func hubDomainMappings() jwt.Mapping {
	const prefix = "$JS.hub.API."
	entries := map[string]string{
		prefix + "INFO":       "$JS.API.INFO",
		prefix + "STREAM.>":   "$JS.API.STREAM.>",
		prefix + "CONSUMER.>": "$JS.API.CONSUMER.>",
		prefix + "DIRECT.>":   "$JS.API.DIRECT.>",
		prefix + "META.>":     "$JS.API.META.>",
		prefix + "SERVER.>":   "$JS.API.SERVER.>",
		prefix + "ACCOUNT.>":  "$JS.API.ACCOUNT.>",
		prefix + "$KV.>":      "$KV.>",
		prefix + "$OBJ.>":     "$OBJ.>",
	}
	out := make(jwt.Mapping, len(entries))
	for from, to := range entries {
		out[jwt.Subject(from)] = []jwt.WeightedMapping{{Subject: jwt.Subject(to)}}
	}
	return out
}

func resolveMappings(preset string) (jwt.Mapping, error) {
	if preset == "" {
		return nil, nil
	}
	if preset == hubDomainMapping {
		return hubDomainMappings(), nil
	}
	return nil, fmt.Errorf("natsacl: unknown mappings preset %q", preset)
}
