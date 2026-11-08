# Native credential storage

`pkg/credential` isolates the MIT-licensed `go-keyring` implementation behind a
small project API: `New(service)`, `Read(name)`, `Write(name, bytes)`, and
`Delete(name)`. Desktop code does not import the implementation directly.

The OS backend is macOS Keychain, Windows Credential Manager, or Linux Secret
Service. Backend errors become static `ErrUnavailable`; missing reads return
`ErrNotFound`. There is no plaintext file fallback. A complete installation
private key and rotating refresh credential are saved as one item, avoiding
separate key/token replacement. Access tokens remain in native memory.

Values are bounded to 2048 bytes and base64-encoded for portable binary storage.
Base64 is not encryption; the OS backend supplies storage protection. The bound
and restricted ASCII names also keep Darwin's interactive `security` command
below its 4096-byte limit. Credentials are passed over its stdin rather than
process arguments. Names and errors carry no credential data.

The wrapper does not promise a non-exportable or hardware-backed signing key,
nor protection from an attacker controlling the logged-in account or process.
Tests replace the private backend with in-memory functions and do not touch a
developer's real credential store.

```sh
go test -race ./pkg/credential
go vet ./pkg/credential
```
