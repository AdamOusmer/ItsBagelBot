# Mac worker isolation

The worker is a separately signed `BagelNetworkWorker.app` nested under the
parent app's `Contents/Helpers`. A bare command-line binary does not provide the
bundle metadata required for independent App Sandbox initialization. The helper
uses its own `com.itsbagelbot.desktop.network-worker` identifier and hardened
runtime, without sandbox inheritance, network/file exceptions, debugging, shared
Keychain groups or shared application groups.

App Sandbox alone is insufficient for this worker. A real test found that the
sandboxed helper could launch `/usr/bin/security` and read a synthetic legacy
Keychain item created using the same mechanism as the credential library. A
direct Keychain call was denied, which by itself would have missed the confused
deputy. The fixture queried only its own uniquely named synthetic item and
removed it afterward.

The helper therefore has one fixed, deny-only
`com.apple.security.temporary-exception.sbpl` array in addition to App Sandbox:

```scheme
(deny process-fork)
(deny process-exec)
(deny mach-lookup
  (global-name-prefix "com.apple.securityd")
  (global-name-prefix "com.apple.SecurityServer"))
```

The launcher verifies the signed helper bundle and its exact entitlement
keys/rules before launch. It accepts no caller-provided policy. `ChildIsolated`
then checks actual restrictions before the worker accepts configuration or
credentials: hardened runtime with no debugger/task allowance; denial of a
parent-created synthetic home canary; TCP bind/connect denial; shell/security
execution denial; denial of the parent's task port; and explicit bootstrap
lookup rejection for the Keychain Mach services. A missing or ignored policy
fails closed. The helper receives only explicitly inherited stream pipes; no
credential files, parent environment or parent memory is provided.

The parent retains TCP connection creation. The worker can read/write brokered
pipes and perform TLS and bounded protocol parsing without direct network
creation. The protocol, supervisor, credential ownership and parent broker are
separate from this OS launcher package.

## Platform and distribution limits

The temporary SBPL entitlement is an implementation-dependent Apple policy
mechanism, not a promise of stable public entitlement support. Current Apple
WebKit sources also use the generic entitlement, but that does not certify this
product's release approval. This implementation does not use `sandbox-exec` or a
deprecated sandbox-init fallback. Unknown OS behavior, rejected entitlements or
failed probes leave the worker unavailable instead of running unrestricted.

Host validation used macOS 27.0.1 on arm64 with an ad-hoc signed helper. Intel Mac
code was cross-compiled; Intel runtime behavior was not tested here. Developer ID
signing, notarization and App Store approval were not performed or certified.
They require release validation with the real distribution identity. Retain the
live tests across supported OS releases.

The build signs nested code from the inside out. Recursive `codesign --deep`
signing was tested and found to remove the helper's sandbox entitlements and
hardened-runtime flag. The Taskfile therefore signs the helper with its own
entitlements, then signs the parent without `--deep`; deep verification remains
appropriate. `darwin:sign` takes `DARWIN_SIGN_IDENTITY`, and `darwin:sign:notarize`
also takes `DARWIN_NOTARY_PROFILE`. No distribution operation is run by the tests.

## Verification

```sh
go test -race ./pkg/sandbox
BAGEL_SANDBOX_KEYCHAIN_PROBE=1 go test -race ./pkg/sandbox -count=1 -v
go vet ./pkg/sandbox
```

The opt-in Keychain test creates one uniquely named dummy item in the default
Keychain, verifies the parent can read it, and proves the isolated helper cannot
read it directly or through the system CLI. It disables helper Keychain UI and
removes the item during cleanup. It never searches for or reads real account
credentials. The normal suite exercises signed-process IPC, live isolation,
unsigned/overprivileged rejection, and strict entitlement matching. Adopted raw
pipes are explicitly restored to nonblocking mode before Go registers them with
its poller; exec deliberately clears that flag. The same adoption applies to
owned duplicates of standard control input/output. This is necessary for real
worker cancellation and deadlines, not just in-process fixture behavior.

The actual production `cmd/networkworker` was also built through the Darwin
Taskfile as a signed universal helper and passed local TLS/OAuth parsing and
binary WebSocket/typed-IPC tests under `-race`:

```sh
# From app/desktop, after packaging the helper:
BAGEL_TEST_SANDBOX_WORKER=/absolute/path/to/BagelNetworkWorker.app/Contents/MacOS/BagelNetworkWorker \
  go test -race ./internal/network -run '^TestSandbox' -count=1 -v
```

Those local tests use controlled certificate roots. Public system-root trust and
the existing Cloudflare Tunnel are separate integration checks.

The retained host observations are in
[testdata/darwin-results.txt](testdata/darwin-results.txt). They establish the
finite checks listed above, not immunity to operating-system vulnerabilities or
attacks that already control the trusted parent.

Apple references:

- [App Sandbox](https://developer.apple.com/documentation/security/protecting-user-data-with-app-sandbox)
- [Embedding helper tools](https://developer.apple.com/documentation/xcode/embedding-a-helper-tool-in-a-sandboxed-app)
- [Keychain access groups](https://developer.apple.com/documentation/security/sharing-access-to-keychain-items-among-a-collection-of-apps)
- [Apple WebKit MiniBrowser entitlement source](https://github.com/WebKit/WebKit/blob/main/Tools/MiniBrowser/MiniBrowser.entitlements)
