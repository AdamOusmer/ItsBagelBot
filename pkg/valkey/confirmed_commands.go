// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"context"
	"strconv"

	valkeygo "github.com/valkey-io/valkey-go"
)

var confirmedCommands = NewScript(`
local offset = tonumber(ARGV[1])
local pos = 2
redis.call('HSET', KEYS[1], 'started', '1')
redis.call('EXPIRE', KEYS[1], 7200)
while pos <= #ARGV do
  local size = tonumber(ARGV[pos])
  local command = {}
  for i = 1, size do command[i] = ARGV[pos+i] end
  if redis.call('HEXISTS', KEYS[1], tostring(offset)) == 0 then
    redis.call(unpack(command))
    redis.call('HSET', KEYS[1], tostring(offset), '1')
  end
  pos = pos + size + 1
  offset = offset + 1
end
return 1
`)

type ConfirmedBatch struct {
	ID       string
	Commands [][]string
}

func ConfirmCommands(ctx context.Context, client valkeygo.Client, batch ConfirmedBatch) error {
	for start := 0; start < len(batch.Commands); {
		end := confirmedEnd(batch.Commands, start)
		args := []string{strconv.Itoa(start)}
		keys := []string{batch.ID}
		for _, command := range batch.Commands[start:end] {
			keys = append(keys, command[1])
			args = append(args, strconv.Itoa(len(command)))
			args = append(args, command...)
		}
		if err := confirmedCommands.Exec(ctx, Primary(client), keys, args).Error(); err != nil {
			return err
		}
		start = end
	}
	_ = client.Do(ctx, client.B().Del().Key(batch.ID).Build()).Error()
	return nil
}

func confirmedEnd(commands [][]string, start int) int {
	end, bytes := start, 0
	for end < len(commands) && end-start < 256 {
		size := 0
		for _, token := range commands[end] {
			size += len(token) + 16
		}
		if end > start && bytes+size > 256*1024 {
			break
		}
		bytes += size
		end++
	}
	return end
}
