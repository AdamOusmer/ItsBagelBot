# Native device proofs

This package owns the RFC 9449 proof format and P-256/ES256 cryptography through
Go's standard library. Applications use project-owned `Key`, `Expected`, and
`Claims` types. JSON serialization uses `pkg/codec`; this is OAuth/JWS metadata,
independent of the binary protobuf event contract.

`NewKey` generates an installation key. `Bytes` and `ParseKey` serialize PKCS#8
for native protected storage; they are not hardware-backed key handles.
`Thumbprint` uses the canonical RFC 7638 public JWK members. `Proof(method, url,
token, nonce)` creates a fresh random identifier and timestamp for each HTTP
request. Token-endpoint requests pass an empty token; protected requests pass
their access token so `ath` binds the proof to its exact bytes.

`Verify` checks a strict ES256 public JWK, JOSE signature, method, HTTPS URL,
issued-at window, and token hash. It rejects private-key fields, algorithm
substitution and malformed/bounded inputs. URI query components are excluded as
required by DPoP. Server and client use configured canonical public URLs; proxy
headers do not select the proof destination.

Verification is only one stage. The service must validate the server-issued
nonce and atomically claim `(thumbprint, jti)` across replicas before accepting a
request. An empty `Expected.Nonce` returns the signed nonce for that service-side
validation; it does not authorize the request. Proof verification does not check
grant revocation or access-token authenticity. Users owns those checks, nonce
issuance, and durable proof replay receipts.

Proof creation and verification occur on infrequent auth/control requests, not
per server event. Cryptography, encoding and persistence allocate; no zero-alloc
claim applies to this path. Stolen tokens alone lack the installation proof;
compromise of both token and usable private key is outside that protection.

Tests include the official RFC 9449 Figure 13 proof, canonical thumbprints,
request/token/nonce binding, key persistence, tampering and malformed proofs:

```sh
go test -race ./pkg/dpop
go vet ./pkg/dpop
```

References: [DPoP](https://www.rfc-editor.org/rfc/rfc9449.html),
[JWK thumbprints](https://www.rfc-editor.org/rfc/rfc7638.html).
