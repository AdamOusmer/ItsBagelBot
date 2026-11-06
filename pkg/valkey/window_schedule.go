// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

const clearCursorSeen = `for _,field in ipairs(redis.call('HKEYS',KEYS[1])) do if string.sub(field,1,12)=='cursor_seen:' then redis.call('HDEL',KEYS[1],field) end end`

// SaveQueuePage persists a SCAN cursor and appends discovered members to a work queue.
var SaveQueuePage = NewScript(`
redis.call('SET',KEYS[1],ARGV[1])
for i=2,#ARGV do redis.call('RPUSH',KEYS[2],ARGV[i]) end
return 1`)

// PopQueueFront removes the queue's front member if it still matches the one named, so a retried pop is harmless.
var PopQueueFront = NewScript(`
if redis.call('LINDEX',KEYS[1],0)~=ARGV[1] then return 0 end
redis.call('LPOP',KEYS[1]);return 1`)

// ArmWindow starts or restarts a scheduled window. ARGV[4] is the fence value read before capture; other writers
// may restamp it with their own clock, so only an already-active schedule still needs it to match the event.
var ArmWindow = NewScript(`
local live=redis.call('GET',KEYS[4]); if live~=ARGV[4] then return 0 end
local active=redis.call('HGET',KEYS[1],'active')=='1'
if active and live~=ARGV[2] then return 0 end
local current=tonumber(redis.call('HGET',KEYS[1],'version') or '-1')
local incoming=tonumber(ARGV[2]); if incoming<current then return 0 end
local generation=redis.call('HGET',KEYS[1],'generation')
if active and generation==ARGV[3] then
 if ARGV[6]=='recover' or incoming==current then return 0 end
end
if not active and incoming==current and generation==ARGV[3] and redis.call('HGET',KEYS[1],'stop_reason')=='offline' then return 0 end
redis.call('DEL',KEYS[3])
redis.call('PERSIST',KEYS[1])
redis.call('HSET',KEYS[1],'version',ARGV[2],'session',ARGV[2],'active','1','generation',ARGV[3],'live_session',ARGV[2],'due',ARGV[5],'chunk','0','failures','0','confirmed_at','0')
redis.call('HDEL',KEYS[1],'window','window_started_at','cursor','pending','pending_cursor','pending_complete','last_error','retry_at','stop_reason','provider_stream_id','provider_started_at')
redis.call('ZADD',KEYS[2],ARGV[5],ARGV[1]);return 1`)

// DisarmWindow stops a scheduled window.
var DisarmWindow = NewScript(`
local current=tonumber(redis.call('HGET',KEYS[1],'version') or '-1')
if tonumber(ARGV[2])<current then return 0 end
redis.call('HSET',KEYS[1],'version',ARGV[2],'active','0','stop_reason','offline')
redis.call('HDEL',KEYS[1],'window','cursor','pending','pending_cursor','pending_complete')
redis.call('PEXPIRE',KEYS[1],ARGV[3]);redis.call('ZREM',KEYS[2],ARGV[1]);redis.call('DEL',KEYS[3],KEYS[4]);return 1`)

// DueWindows lists members due at or before ARGV[1], up to ARGV[2].
var DueWindows = NewScript(`return redis.call('ZRANGEBYSCORE',KEYS[1],'-inf',ARGV[1],'LIMIT',0,ARGV[2])`)

// ClaimWindow claims an owner's exclusive turn to process a due window.
var ClaimWindow = NewScript(`
if redis.call('HGET',KEYS[1],'active')~='1' then redis.call('ZREM',KEYS[2],ARGV[1]);return 0 end
local due=tonumber(redis.call('ZSCORE',KEYS[2],ARGV[1]) or '')
if not due or due>tonumber(ARGV[3]) then return 0 end
if not redis.call('SET',KEYS[3],ARGV[2],'NX','PX',ARGV[4]) then return 0 end
if not redis.call('HGET',KEYS[1],'window') then
` + clearCursorSeen + `
 local original=redis.call('HGET',KEYS[1],'due') or ARGV[3]
 local session=redis.call('HGET',KEYS[1],'session')
 local generation=redis.call('HGET',KEYS[1],'generation')
 redis.call('HSET',KEYS[1],'window',ARGV[1]..':'..generation..':'..session..':'..original,'window_started_at',ARGV[3],'cursor','','chunk','0')
end
redis.call('ZADD',KEYS[2],tonumber(ARGV[3])+tonumber(ARGV[4]),ARGV[1]);return 1`)

// SaveWindowPage stages one page of work under the current claim, refusing a page already seen when cursor retries are disabled.
var SaveWindowPage = NewScript(`
if redis.call('GET',KEYS[2])~=ARGV[1] or redis.call('HGET',KEYS[1],'active')~='1' or redis.call('HGET',KEYS[1],'window')~=ARGV[2] then return 0 end
if redis.call('HGET',KEYS[1],'pending') then return 0 end
if ARGV[5]=='0' and redis.call('HEXISTS',KEYS[1],'cursor_seen:'..ARGV[4])==1 then return -1 end
redis.call('HSET',KEYS[1],'pending',ARGV[3],'pending_cursor',ARGV[4],'pending_complete',ARGV[5]);return 1`)

// CommitWindowPage commits a staged page: it either advances to the next window or records the cursor and keeps collecting.
var CommitWindowPage = NewScript(`
if redis.call('GET',KEYS[3])~=ARGV[1] or redis.call('HGET',KEYS[1],'active')~='1' or redis.call('HGET',KEYS[1],'window')~=ARGV[3] or redis.call('HGET',KEYS[1],'pending')~=ARGV[4] then return 0 end
local now=tonumber(ARGV[6])
if ARGV[5]=='1' then
 local interval=tonumber(ARGV[7])
 local original=tonumber(redis.call('HGET',KEYS[1],'due') or ARGV[6])
 local nextdue=original+interval
 if nextdue<=now then nextdue=nextdue+(math.floor((now-nextdue)/interval)+1)*interval end
 redis.call('HSET',KEYS[1],'due',nextdue,'chunk','0')
 redis.call('HDEL',KEYS[1],'window','window_started_at','cursor')
` + clearCursorSeen + `
 redis.call('ZADD',KEYS[2],nextdue,ARGV[2])
else
 local cursor=redis.call('HGET',KEYS[1],'pending_cursor')
 redis.call('HSET',KEYS[1],'cursor',cursor,'cursor_seen:'..cursor,'1')
 redis.call('HINCRBY',KEYS[1],'chunk',1)
 redis.call('ZADD',KEYS[2],now+250,ARGV[2])
end
redis.call('HSET',KEYS[1],'failures','0')
redis.call('HDEL',KEYS[1],'pending','pending_cursor','pending_complete','last_error','retry_at')
return 1`)

// ConfirmWindow records confirmation of the live stream backing a window, restarting the window when the provider's stream identity changed underneath it.
var ConfirmWindow = NewScript(`
if redis.call('GET',KEYS[2])~=ARGV[1] or redis.call('HGET',KEYS[1],'active')~='1' then return 0 end
local prior=redis.call('HGET',KEYS[1],'provider_stream_id')
redis.call('HSET',KEYS[1],'confirmed_at',ARGV[2],'provider_stream_id',ARGV[3],'provider_started_at',ARGV[4])
if prior and prior~=ARGV[3] then
 redis.call('HSET',KEYS[1],'due',ARGV[6],'chunk','0','failures','0','last_error','provider stream changed')
 redis.call('HDEL',KEYS[1],'window','window_started_at','cursor','pending','pending_cursor','pending_complete','retry_at')
 redis.call('ZADD',KEYS[3],ARGV[6],ARGV[5]);return 2
end
return 1`)

// StopWindow deactivates a schedule under the current claim.
var StopWindow = NewScript(`
if redis.call('GET',KEYS[3])~=ARGV[1] then return 0 end
redis.call('HSET',KEYS[1],'active','0','stop_reason',ARGV[3])
redis.call('HDEL',KEYS[1],'window','cursor','pending','pending_cursor','pending_complete')
redis.call('ZREM',KEYS[2],ARGV[2]);redis.call('PEXPIRE',KEYS[1],ARGV[4]);return 1`)

// RetryWindow records a failed attempt and reschedules it, returning the new consecutive-failure count.
var RetryWindow = NewScript(`
if redis.call('GET',KEYS[3])~=ARGV[1] or redis.call('HGET',KEYS[1],'active')~='1' then return 0 end
local n=redis.call('HINCRBY',KEYS[1],'failures',1)
local delay=tonumber(ARGV[6]);if n>tonumber(ARGV[8]) then delay=tonumber(ARGV[7]) end
local retry=math.max(tonumber(ARGV[3])+delay,tonumber(ARGV[4]))
redis.call('HSET',KEYS[1],'retry_at',retry,'last_error',ARGV[5]);redis.call('ZADD',KEYS[2],retry,ARGV[2]);return n`)

// AbandonWindow drops an unrepairable window, keeping the schedule active at normal cadence.
// Definitive cursor/payload errors cannot be repaired by repeating the same request.
var AbandonWindow = NewScript(`
if redis.call('GET',KEYS[3])~=ARGV[1] or redis.call('HGET',KEYS[1],'active')~='1' then return 0 end
redis.call('HSET',KEYS[1],'due',ARGV[3],'chunk','0','failures','0','last_error',ARGV[4])
redis.call('HDEL',KEYS[1],'window','window_started_at','cursor','pending','pending_cursor','pending_complete','retry_at')
redis.call('ZADD',KEYS[2],ARGV[3],ARGV[2]);return 1`)
