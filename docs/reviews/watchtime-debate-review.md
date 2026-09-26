# Watchtime review and debate

Reviewed September 26, 2026 by two independent GPT-6 Sol subagents. Reviewer A focused on scheduler, replay, lifecycle and retention invariants. Reviewer B independently reviewed request admission, cancellation, provider feedback and shared quotas, then challenged the storage and projection findings. Both read the current implementation and exchanged concrete counterexamples. The primary agent implemented the agreed corrections; reviewers rechecked them.

## Confirmed finding and debate

**P1: settings expiry could erase loyalty's module revision fence.** A newer disable or payout configuration was protected only by `module:loyalty:revision` in the expiring settings hash. After that hash expired, an older stamped enabled/config event could pass the revision gate. Equal-revision canonical user hydration restored active user fields, and direct watchtime admission accepted the stale loyalty configuration.

Reviewer A reproduced this using the original application Lua on isolated real Valkey, including an old revision-zero event. Reviewer B challenged whether projected markers, user revisions or generation changes prevented it. They agreed none did: scheduler/outgress admission calls `Capture` directly, and changing the epoch fenced old work while permitting newly generated work with the stale configuration.

The correction stores a permanent, account-scoped `loyalty_revision` in the existing primary-Valkey admission record. Both event and full-snapshot gates compare the maximum known/warm revision, preserve it before rejecting older writes, and accept equal canonical revisions to rebuild expired settings. Only strictly newer account restoration resets the fence. Existing clients and service wiring are reused.

The debate uncovered two follow-up cases in that correction:

| Counterexample | Final behavior |
| --- | --- |
| An old full snapshot skips or omits a known loyalty row after settings expiry, then incorrectly marks the empty projection complete. | The snapshot leaves hydration incomplete while a known loyalty row is missing, allowing canonical refetch. Other module rows can still merge. |
| A warm projection from an older writer lacks the permanent revision; the first new full snapshot omits loyalty, and settings later expires. | Full hydration bootstraps the trusted warm revision even when the incoming snapshot omits loyalty. It adopts only a warm row stamped for the known current account, after lifecycle checks. |

Both reviewers accepted the final implementation after challenging these cases. No disagreement or other high-confidence code finding remained in the audited paths.

## Verification and limits

Real-Valkey regressions cover old event and hydration revisions zero/one, disabled and changed-payout settings, equal canonical revision recovery, warm bootstrap before rejection, warm snapshots omitting loyalty, empty cold snapshots, unrelated module merging, deletion tombstones and account recreation. Targeted race checks and affected projection/projector/request/repository tests pass. Reviewer A also exercised the corrected Lua directly on real Valkey; Reviewer B's final review was static.

Their independent traces found the existing owned-outbox acceptance, immutable saved-page replay, SQL transaction/barrier ordering, per-attempt credential/admission sequence, header-time 429 publication and shared app/bot quota profiles consistent with the stated invariants. This is a code review conclusion, not production reliability evidence.

Real MySQL concurrency/performance tests remain unexecuted without a server. Sustained fleet earnings coverage, Valkey failover and retained-storage restore still require the deployment environment. See the [rollout guide](../../docs/operations/watchtime-rollout.md).

## Port to current main

The PR was assembled on current `main` in an isolated checkout. Sol agents preserved its newer counter-batch receipt transactions and trial handling, and added a regression that discarded retired-account batches retain their receipt across recreation. The original dirty workspace and its unrelated changes were left intact.

Luna's additional crosscheck identified stale-delete side effects introduced by newer live totals and existing module/credential cleanup. Ignored deletions now also skip live-counter cleanup. Modules resolve a correlated canonical incarnation before any cleanup, distinguish explicit source absence from failure, and compare captured row identities/content/versions when deleting modules, credentials and quotes. Both Sol reviewers challenged the final cleanup behavior. Commands/fetches still need the separate broader cleanup audit noted in the rollout guide.

Integration checks also preserve viewer lookup wiring: the legacy chatter request receives a bounded complete-list fallback using the existing quota authority and primary account eligibility. Incomplete lists return an error without partial viewers; watch pages warm the existing viewer cache only for a complete first-page listing.
