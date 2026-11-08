# Safe optimized codec fixture

The committed fixture was generated with protoc v7.35.1, protoc-gen-go v1.36.12,
and protoc-gen-go-vtproto v0.6.0. The generated decoder copies strings/bytes;
no unmarshal_unsafe is enabled. Regeneration is optional: ordinary builds and
tests consume the committed generated files and do not download or run compilers.

If installing the plugins for regeneration, use explicit versions:

```sh
go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.12
go install github.com/planetscale/vtprotobuf/cmd/protoc-gen-go-vtproto@v0.6.0
```

From the repository root, with the pinned plugins and protoc v7.35.1 available
on PATH:

```sh
protoc -I . --go_out=. --go_opt=paths=source_relative \
  --go-vtproto_out=. \
  --go-vtproto_opt=paths=source_relative,features=marshal+unmarshal+size+pool \
  --go-vtproto_opt=pool=ItsBagelBot/pkg/grpc/internal/testpb.Packet \
  --go-vtproto_opt=pool=ItsBagelBot/pkg/grpc/internal/testpb.Child \
  pkg/grpc/internal/testpb/message.proto
```

Only VT-owned empty receive destinations are pooled. ResetVT runs before decode
and again after failure, returning any nested objects before reuse. Standard
protobuf encoding/decoding checks wire interoperability independently of VT.
