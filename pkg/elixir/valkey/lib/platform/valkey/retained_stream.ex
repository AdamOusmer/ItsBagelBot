# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
defmodule Platform.Valkey.RetainedStream do
  @moduledoc "Atomic cross-process retained stream; sequence numbers are decimal strings."
  @script ~S'''
  local stream, seq, leases, cursors, migrations, bytes = unpack(KEYS)
  local op = ARGV[1]
  local t = redis.call('TIME'); local now = t[1]*1000 + math.floor(t[2]/1000)
  local function cmp(a,b)
    a = string.gsub(a, '^0+', ''); b = string.gsub(b, '^0+', '')
    if a == '' then a='0' end; if b == '' then b='0' end
    if #a ~= #b then return #a < #b and -1 or 1 end
    if a == b then return 0 end; return a < b and -1 or 1
  end
  local expired = redis.call('ZRANGEBYSCORE', leases, '-inf', now)
  for _, id in ipairs(expired) do redis.call('ZREM', leases, id); redis.call('HDEL', cursors, id, 'cap:'..id) end
  local function latest()
   local last=redis.call('XREVRANGE',stream,'+','-','COUNT',1)
   if #last == 0 then return '0' end
   return string.match(last[1][1], '^(%d+)%-')
  end
  local function predecessor(n)
   local out={}; local borrow=1
   for i=#n,1,-1 do local d=string.byte(n,i)-48-borrow; borrow=0; if d<0 then d=9; borrow=1 end; table.insert(out,1,string.char(48+d)) end
   local r=string.gsub(table.concat(out),'^0+',''); if r=='' then return '0' else return r end
  end
  local function valid(c)
   if not string.match(c, '^%d+$') or cmp(c, latest()) > 0 then return false end
   local first = redis.call('XRANGE', stream, '-', '+', 'COUNT', 1)
   if #first == 0 then return cmp(c,latest()) == 0 end
   local firstseq = string.match(first[1][1], '^(%d+)%-')
   return cmp(c, predecessor(firstseq)) >= 0
  end
  if op == 'open' then
   local id,cursor,token = ARGV[2],ARGV[3],ARGV[4]
   if not valid(cursor) or redis.call('ZCARD', leases) >= 2 then return {'invalid'} end
   local through = ''
   if token ~= '' then
     local m = redis.call('HGET', migrations, token)
     if not m then return {'invalid'} end
     local deadline, cutoff, oldid = string.match(m, '^(%d+):(%d+):(.+)$')
     if not deadline or tonumber(deadline) <= now then redis.call('HDEL', migrations, token); return {'invalid'} end
     through = cutoff; if redis.call('ZSCORE',leases,oldid) then redis.call('HSET',cursors,'cap:'..oldid,cutoff..':'..deadline..':'..id) end; redis.call('HDEL', migrations, token)
   end
   redis.call('ZADD', leases, now+45000, id); redis.call('HSET', cursors,id,cursor)
   return {'ok',through}
  elseif op == 'read' then
   local id,cursor,limit=ARGV[2],ARGV[3],tonumber(ARGV[4])
   if not redis.call('ZSCORE',leases,id) or not valid(cursor) or not limit or limit < 1 or limit > 16 then return {'invalid'} end
   redis.call('ZADD',leases,now+45000,id)
   local cap=redis.call('HGET',cursors,'cap:'..id)
   if cap then local cutoff, deadline, candidate = string.match(cap, '^(%d+):(%d+):(.+)$'); if candidate then if not redis.call('ZSCORE',leases,candidate) then redis.call('HDEL',cursors,'cap:'..id); cap=nil else cap=cutoff end else cutoff,deadline = string.match(cap, '^(%d+):(%d+)$'); if deadline then if tonumber(deadline) <= now then redis.call('HDEL',cursors,'cap:'..id); cap=nil else cap=cutoff end end end end
   if cap and cmp(cursor,cap)>=0 then return {'ok'} end
   local entries=redis.call('XRANGE',stream,'('..cursor..'-0',cap and (cap..'-0') or '+','COUNT',limit)
   local out={'ok'}; for _,e in ipairs(entries) do table.insert(out,e[2][2]) end; return out
  elseif op == 'checkpoint' then
   local id,cursor=ARGV[2],ARGV[3]
   local previous=redis.call('HGET',cursors,id)
   if not redis.call('ZSCORE',leases,id) or not previous or not valid(cursor) or cmp(cursor,previous)<0 then return {'invalid'} end
   local cap=redis.call('HGET',cursors,'cap:'..id)
   if cap then
     local cutoff,deadline,candidate=string.match(cap,'^(%d+):(%d+):(.+)$')
     if candidate then
       if not redis.call('ZSCORE',leases,candidate) then redis.call('HDEL',cursors,'cap:'..id); cap=nil else cap=cutoff end
     else
       cutoff,deadline=string.match(cap,'^(%d+):(%d+)$')
       if deadline then if tonumber(deadline)<=now then redis.call('HDEL',cursors,'cap:'..id); cap=nil else cap=cutoff end end
     end
   end
   if cap and cmp(cursor,cap)>0 then return {'invalid'} end
   redis.call('HSET',cursors,id,cursor); redis.call('ZADD',leases,now+45000,id)
   return {'ok'}
  elseif op == 'close' then
   redis.call('ZREM',leases,ARGV[2]); redis.call('HDEL',cursors,ARGV[2],'cap:'..ARGV[2]); return {'ok'}
  elseif op == 'migrate' then
   local id,cursor,window,token=ARGV[2],ARGV[3],tonumber(ARGV[4]),ARGV[5]
   if not redis.call('ZSCORE',leases,id) or not valid(cursor) or not window or window<1 or window>120000 then return {'invalid'} end
   -- one pending migration per identity, bounded and scoped by the key
   redis.call('DEL',migrations); local cutoff=latest()
   redis.call('HSET',cursors,'cap:'..id,cutoff..':'..(now+window)); redis.call('HSET',migrations,token,(now+window)..':'..cutoff..':'..id); redis.call('PEXPIRE',migrations,window)
   return {'ok',token}
  elseif op == 'append' then
   local payload,maxcount,maxbytes,expected=ARGV[2],tonumber(ARGV[3]),tonumber(ARGV[4]),ARGV[5]
   if not maxcount or maxcount<1 or not maxbytes or #payload>maxbytes or not string.match(expected,'^[1-9]%d*$') or cmp(expected,'18446744073709551615')>0 then return {'invalid'} end
   local prior=latest()
   if cmp(expected,prior)<=0 then
     local existing=redis.call('XRANGE',stream,expected..'-0',expected..'-0','COUNT',1)
     if #existing==1 and existing[1][2][2]==payload then return {'ok',expected} end
     return {'invalid'}
   end
   local digits={}; local carry=1
   for i=#prior,1,-1 do local d=string.byte(prior,i)-48+carry; carry=0; if d==10 then d=0; carry=1 end; table.insert(digits,1,string.char(48+d)) end
   if carry==1 then table.insert(digits,1,'1') end
   if expected~=table.concat(digits) then return {'invalid'} end
   -- A head-tagged cache is trusted only when it matches the authoritative
   -- stream head. A failed cache write or post-XADD trim is repaired by a scan.
   local count=redis.call('XLEN',stream)
   local cached=redis.call('HMGET',bytes,'head','size')
   local used=tonumber(cached[2])
   if cached[1]~=prior or not used or used<0 then
     used=0
     local retained=redis.call('XRANGE',stream,'-','+')
     for _,e in ipairs(retained) do used=used+#e[2][2] end
   end
   local remove={}
   if count>=maxcount or used+#payload>maxbytes then
     -- At most 128 removals per append; return backpressure for larger work.
     local entries=redis.call('XRANGE',stream,'-','+','COUNT',128)
     local all=redis.call('HGETALL',cursors)
     local pending=redis.call('HLEN',migrations)>0
     local i=1
     while count>=maxcount or used+#payload>maxbytes do
       local e=entries[i]; if not e or pending then return {'full'} end
       local firstseq=string.match(e[1],'^(%d+)%-')
       for j=1,#all,2 do if all[j] ~= '_floor' and string.sub(all[j],1,4) ~= 'cap:' and cmp(all[j+1],firstseq)<0 then return {'full'} end end
       table.insert(remove,e[1]); used=used-#e[2][2]; count=count-1; i=i+1
     end
   end
   -- XADD is the sole head authority. The cache is written last; if a write
   -- fails, its head mismatch causes a rebuild on the next append.
   local added=redis.pcall('XADD',stream,expected..'-0','data',payload)
   if type(added)=='table' and added.err then return {'full'} end
   for _,id in ipairs(remove) do redis.call('XDEL',stream,id) end
   redis.pcall('HSET',bytes,'head',expected,'size',used+#payload)
   return {'ok',expected}
  end
  return {'invalid'}
  '''
  @digest :crypto.hash(:sha, @script) |> Base.encode16(case: :lower)

  def call(conn, key, op, args) do
    keys =
      Enum.map(["stream", "seq", "leases", "cursors", "migrations", "bytes"], &(key <> ":" <> &1))

    case Platform.Valkey.execute(conn, {@digest, @script}, keys, [op | args]) do
      {:ok, ["ok" | rest]} -> {:ok, rest}
      {:ok, [reason | _]} -> {:error, reason}
      _ -> {:error, :unavailable}
    end
  end
end
