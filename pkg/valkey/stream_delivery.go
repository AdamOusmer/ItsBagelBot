// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"context"

	valkey_go "github.com/valkey-io/valkey-go"
)

var refreshStreamDeliveryScript = valkey_go.NewLuaScript(`
local pending=redis.call('XPENDING',KEYS[1],ARGV[1],ARGV[3],ARGV[3],1)
if #pending==0 then return 0 end
if pending[1][2]~=ARGV[2] then return 0 end
local refreshed=redis.call('XCLAIM',KEYS[1],ARGV[1],ARGV[2],0,ARGV[3],'JUSTID')
return #refreshed`)

type StreamDelivery struct {
	Stream   string
	Group    string
	Consumer string
	ID       string
}

func (d StreamDelivery) Refresh(ctx context.Context, client valkey_go.Client) (bool, error) {
	return refreshStreamDeliveryScript.Exec(ctx, client, []string{d.Stream}, []string{d.Group, d.Consumer, d.ID}).AsBool()
}
