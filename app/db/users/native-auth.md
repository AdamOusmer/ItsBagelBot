# Native device authentication

The dashboard remains the browser login and new-device consent surface. Consent
requires a dashboard session, PKCE S256 and a canonical `dpop_jkt` installation
key thumbprint. It asks Users to create a 60-second authorization code. It does
not exchange codes or refresh credentials. The retired dashboard machine token
endpoint never redirects or forwards a token-bearing request.

The native app exchanges and refreshes directly at the independent Elixir
`POST /oauth/desktop/token` endpoint. It sends form-encoded OAuth fields,
`client_id=itsbagelbot-desktop` and an ES256 DPoP proof header. Elixir supplies its
configured public URL to Users over scoped RPC; neither request Host nor
forwarding headers select that proof audience. No dashboard cookie is required.

Users verifies the proof, key binding, nonce and global proof receipt before
redeeming/rotating a grant. An absent/expired nonce produces HTTP 400 JSON
`error=use_dpop_nonce` and `DPoP-Nonce`. The native client retries with a newly
signed proof. Nonces are purpose-bound, encrypted, URL/key-bound and valid for
two minutes with 30 seconds of clock skew. Accepted proof IDs are stored as
unique SHA-256 receipts for five minutes in `DeviceProof`; this rejects replay
across Users replicas and restarts. The existing expiry sweep removes receipts.

A bound access token lasts 24 hours and has `token_type=DPoP`. It is an opaque
`v2.<base64url ciphertext>` string: Users seals a separately signed v2 payload
with its existing Tink AEAD packer and fixed purpose binding. The inner signature
contains `cnf.jkt`, user/device/grant IDs, issuance/expiry, audience and signing
key ID. Only Users holds the encryption key and private signing key. Gateway
pods receive neither, and the dashboard session key is never reused. Encrypted
claims alone do not prevent theft; the installation key proof provides that
additional constraint. Old unbound v1 tokens keep their 15-minute TTL and cannot
be exchanged through the new machine endpoint.

Refresh tokens remain random secrets stored only as hashes. Both code redemption
and refresh are key-bound; old bearer-only entry points cannot redeem bound
records. Refresh rotates with a conditional hash update. The previous token may
recover the identical encrypted result for two minutes with a fresh proof from
the same installation key, preventing a lost HTTP response from stranding the
device. Concurrent retries cannot rotate twice. Authorization-code response
recovery additionally requires valid PKCE and an unexpired code. Legacy unbound
refresh reuse keeps its existing 10-second behavior.

`POST /oauth/desktop/revoke` accepts `Authorization: DPoP <access token>` plus a
DPoP proof, and returns 204 after revoking that grant. Its nonce challenge also
carries `WWW-Authenticate: DPoP error="use_dpop_nonce"`. Local native credential
removal must still work when networking is unavailable.

## Binding authority and current server limit

`bagel.rpc.overlay.device.authorize` takes the access token, proof, method and a
configured public resource URL. Users verifies the inner signature, expiration,
proof key, owner and durable grant state. A revoked/banned/deleted/expired grant
cannot reconnect merely because its access token has time remaining. This check
belongs at new binding and periodic HTTPS reauthorization, outside per-event
processing. A database outage is an unavailable authority, not a valid grant.

This RPC is a server integration boundary, not a completed protobuf event server.
The old `/v1/connect` handler still implements legacy JSON duplex connections,
15-minute bearer tokens and best-effort core-NATS revocation. It cannot accept
v2 sealed tokens or satisfy the new make-before-break protobuf contract. A future
bridge server must use authoritative binding and periodic renewal and respond
correctly when authorization becomes unavailable. Core NATS alone is not durable
active-socket invalidation. No existing production listener has been repurposed.

## Configuration and rollout

Users:

- `DEVICE_AUTH_TOKEN_URL`: exact public HTTPS token URL, at most 180 bytes.
- `DEVICE_AUTH_RESOURCE_URLS`: whitespace-separated exact public HTTPS URLs for
  binding, upgrade, authorization renewal and self-revocation (at most eight).
- `DEVICE_AUTH_SIGNING_KEY` / `DEVICE_AUTH_SIGNING_KID`: existing active key.
- `DEVICE_AUTH_VERIFY_PUBLIC_KEYS`: optional JSON object of retained signing key
  IDs to base64 Ed25519 public keys. Retain old public keys for the full token
  lifetime during rotation. Tink keyset rotation must likewise retain decrypting
  keys until outstanding tokens/nonces/recovery results expire.
- `NATS_OVERLAY_AUTH_SUBJECT_PREFIX`: optional override of
  `bagel.rpc.overlay.device`.

Elixir uses the same `DEVICE_AUTH_TOKEN_URL`,
`NATS_OVERLAY_AUTH_SUBJECT_PREFIX`, and `DEVICE_AUTH_REVOKE_URL` for its trusted
public self-revocation URL. The token listener fails closed when unconfigured.
Users subscribes the new native prefix only when its token URL is configured.
The Elixir native identity needs only the `token`, `authorize` and `revoke`
request subjects; it must not reuse the dashboard NATS identity.

The additive schema adds key binding on authorization codes/device grants,
encrypted refresh recovery data, and short-lived proof receipts. Generated Ent
files are committed. No live database or infrastructure was changed by this
implementation. Stage schema, the separate NATS identity, private service routing
and deployment configuration before enabling native pairing. Origin exposure,
DNS, Tunnel and firewall changes are not part of these code changes.

## Checks

```sh
go test -race ./app/db/users/... ./internal/domain/validate
# app/controls/gateway
mix format --check-formatted
mix compile --warnings-as-errors
mix test
```

Tests cover sealed access, wrong-key/bearer downgrade rejection, durable revoked
reconnect rejection, fresh owner state, nonce/proof replay, code/refresh response
recovery, key rotation, independent RPC registration, and real HTTP/NATS adapter
behavior. Existing MySQL concurrency tests require `MYSQL_TEST_DSN`; SQLite
security tests run without it. These tests do not certify a production deployment.
