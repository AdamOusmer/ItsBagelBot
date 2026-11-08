# Safe optimized protobuf

`pkg/protobuf` owns transport-independent serialization for generated safe VT
messages. Applications import this package instead of protobuf vendors. The
WebSocket package owns frames and native connections; supervisors own accounts,
binding, retries, handoff and event routing. The same serializer is used by the
shared gRPC codec without creating a dependency on gRPC types.

The vendor-free `Message` interface requires `SizeVT`, `MarshalToSizedBufferVT`,
`UnmarshalVT` and `ResetVT`. Generate safe marshal/unmarshal/size/pool helpers;
unsafe input-aliasing decoders do not satisfy the ownership contract.

- `Check(value)` validates optimized type and a non-nil pointer.
- `Size(message)` validates size in the range 0–4 MiB and contains size panics.
- `MarshalTo(dst, message)` writes the exact protobuf wire bytes to `dst[:n]`.
  Callers provide sufficient `len(dst)` and may reuse the buffer. It does not
  grow or retain the buffer. On error, n is zero and partial bytes must not be sent.
- `Unmarshal(src, message)` replaces rather than merges the destination. It resets
  before decode and again after failure, bounds input at 4 MiB and copies strings
  and bytes through the safe generated decoder. It does not retain src.
- `Reset(message)` clears a valid VT-owned message and returns nested objects.
- `HasUnknown(message)` reports retained fields outside the generated schema at
  that message level. Strict application contracts check each bounded nested
  type as well; generic decoding retains normal protobuf unknown-field behavior.
- `Empty` is compatible with google.protobuf.Empty. Valid unknown wire fields
  are consumed without retention; malformed data is rejected.

Typed nils, unsupported implementations, invalid sizes, short output buffers and
panics return static project errors. There is no implicit fallback to reflection,
JSON or an unsafe decoder. A smaller transport/application limit must be enforced
by its owner in addition to the generic 4 MiB serialization ceiling.

Receive destinations start empty or are VT-pool owned, including nested messages.
Each message and output buffer has one owner during a call. Do not reset or return
a message while another consumer still reads its fields. Detach nested messages
before transferring their ownership to another consumer.

Tests cover standard protobuf wire interoperability, exact uint64 values, owned
strings/bytes/unknown fields after input mutation, replacing mixed messages,
failed-decode cleanup, malformed wire data, bounds and panic containment.
Warmed reusable-buffer marshaling and scalar-only decoding have zero-allocation
regression tests. Safe decoding of strings/bytes can allocate; transport and TLS
costs are outside this package's allocation budget.

```sh
go test -race ./pkg/protobuf
go test ./pkg/protobuf -run '^$' -bench . -benchmem
```

Committed test fixtures use protoc v7.35.1, protoc-gen-go v1.36.12 and
protoc-gen-go-vtproto v0.6.0. Ordinary builds/tests do not regenerate code.
To regenerate with those tools on PATH, run from the repository root:

```sh
protoc -I . --go_out=. --go_opt=paths=source_relative \
  --go-vtproto_out=. \
  --go-vtproto_opt=paths=source_relative,features=marshal+unmarshal+size+pool \
  --go-vtproto_opt=pool=ItsBagelBot/pkg/protobuf/internal/testpb.Packet \
  --go-vtproto_opt=pool=ItsBagelBot/pkg/protobuf/internal/testpb.Child \
  pkg/protobuf/internal/testpb/message.proto
```
