# Sift

`Platform.Sift` is the JSON package for the Elixir services. It wraps
[Torque](https://github.com/pluralsh/torque), a Rustler NIF over sonic-rs, the
way Go's `pkg/codec` wraps sonic. Services must not call Torque or any other
JSON library directly; `Platform.Boundary` enforces that.

| Go `pkg/codec` | `Platform.Sift` |
| --- | --- |
| `Marshal`, `MarshalIndent`, `Unmarshal`, `Valid` | `encode/2`, `encode!/2`, `encode_to_iodata!/1` (`pretty: true`), `decode/1`, `decode!/1`, `valid?/1` |
| `ExtractValue` and `Kind` | `extract_value/2` returns `{:ok, value, kind}` |
| `ExtractString`, `ExtractInt`, `ExtractFloat`, `ExtractBool` | `extract_string/2`, `extract_int/2`, `extract_float/2`, `extract_bool/2` |
| `ExtractEach`, `ExtractArray` | `extract_each/4`, `extract_array/4` (reduce with `{:cont, acc}` or `{:halt, acc}`) |
| `Pack`, `Unpack` | `pack/1`, `unpack/1` |

`pointers/1` prepares a fixed set of paths once, and `scan/2` reads them from a
document in one pass, building only those values (`nil` for a missing path or
JSON `null`). This is the scanner for hot paths that need a few fields per
document. The prepared set is a NIF resource: keep it in `:persistent_term` or
process state, not a module attribute. It assumes documents have no duplicate
object keys.

Paths are lists of object keys and array indexes, like Go's `codec.Path`. `null`
decodes to `nil`, and the last duplicate key wins on full decode. Decode errors
are `%Platform.Sift.DecodeError{reason}` and encode errors
`%Platform.Sift.EncodeError{reason}`. Structs other than `Date`, `Time`,
`NaiveDateTime` and `DateTime` do not encode.

Torque is pinned to the commit that ships the v0.6.1 NIF checksums, and its
build downloads the precompiled NIF for the platform and verifies it against
them. On Intel/AMD it picks a CPU variant from the build host, so container
builds set `TORQUE_CPU_VARIANT` to what the nodes run.

`pack/1` and `unpack/1` use OTP's zstd with the thresholds, window and frame
magic of the Go package, so frames are readable on both sides. `unpack/1` checks
the frame's declared size and stops at 4 MiB.

Run `mix format --check-formatted` and `mix test` in this directory.
