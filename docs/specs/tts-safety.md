# TTS content safety

Date: 2026-11-22
Status: Draft, revised after review rounds 1 and 2. Nothing in this spec is implemented yet.

## Goal

Every utterance we send to Google for speech synthesis must be one Google's
policies allow, so that:

1. Streams never play hate, sexual, harassing or threatening audio, or a viewer's personal data.
2. Our Google Cloud project never builds the abuse record that leads to suspension.

Google's filters are a backstop, not our gate. Google says flagged requests
are logged and may be reviewed, and that repeated or severe abuse may lead to
suspension. It does not say how many requests one blocked request counts as.
Our working assumption: every request Google flags counts against the project.
So we filter before the request leaves.

### Twitch tone

Twitch chat is loud, crude and full of banter. Swearing, trash talk, game
violence ("I'll kill you", "headshot", "you're trash", "gg ez, uninstall")
and edgy jokes are normal there, and they are what viewers pay to hear in TTS.
A filter that blocks them kills the feature. So the hard floor is narrow:
it covers only what Google prohibits outright and what no streamer wants
read aloud. Everything else is the streamer's call, through the automod
settings they already have:

- **Always blocked:** slurs and hate, explicit sexual acts, telling someone to
  kill themselves, real-world threats, CSAM, personal data.
- **Streamer's choice:** swearing, insults, crude sexual words, "go die".
- **Never blocked:** game violence, trash talk, and roasting the streamer.

Strikes follow the same line. Only floor hits and Google refusals strike, a
first slip is forgiven, and blocks are short.

## Google rules that apply

Sources read on 2026-11-22:

- Generative AI Prohibited Use Policy (policies.google.com/terms/generative-ai/use-policy, last modified 2024-12-17).
- Vertex AI abuse monitoring (docs.cloud.google.com/vertex-ai/generative-ai/docs/learn/abuse-monitoring).
- Gemini API Additional Terms (aistudio.google.com/docs/terms, effective 2026-03-23).
- Vertex AI safety filters (docs.cloud.google.com/vertex-ai/generative-ai/docs/multimodal/configure-safety-filters).
- `GenerateContentResponse` API reference.
- Speech generation guide (aistudio.google.com/docs/speech-generation).

| Rule | Source | What it means for TTS |
| --- | --- | --- |
| No hate speech | Prohibited Use 3 | Slurs and attacks on protected groups never reach Google. |
| No "harassment, bullying, intimidation, abuse, or the insulting of others" | Prohibited Use 3 | Insults aimed at a person are covered, not only slurs. Most Twitch trash talk is not harassment in this sense, so insults are channel tier and Google's own filter is the backstop (see Twitch tone). |
| No sexually explicit content | Prohibited Use 3 | Part of the floor. |
| No violence or incitement, no facilitating self-harm | Prohibited Use 1, 3 | Real-world threats and "kill yourself" variants are part of the floor. Game violence is not. |
| No CSAM | Prohibited Use 1; non-configurable filter | Part of the floor. Google always blocks it (`PROHIBITED_CONTENT`). |
| No privacy violations | Prohibited Use 1; non-configurable `SPII` filter | Emails, phone numbers, IP addresses and card numbers are never spoken. |
| No impersonation of a real person to deceive | Prohibited Use 4 | Prebuilt voices only. Voice cloning stays out of scope. |
| No "circumvention of abuse protections or safety filters" | Prohibited Use 2; Gemini API terms | We never rewrite blocked text so it slips through. A blocked utterance is dropped whole. |
| Classifiers flag, flagged prompts are logged up to 90 days, staff may review; "recurring or severe abuse may result in suspension or termination" | Vertex abuse monitoring | Refusals are an account-level signal. We count them and stop sending at a threshold. |
| 18+ only, and the client must not be "likely to be accessed by individuals under the age of 18"; free-tier prompts may be read by human reviewers | Gemini API terms | The Gemini API backend is unsuitable for Twitch audiences. Prod uses Vertex only. |
| `PROHIBITED_CONTENT` and `SPII` cannot be configured. Configurable harm thresholds have conflicting defaults in the docs: `OFF` for `gemini-3.5-flash` and later in one section, `BLOCK_MEDIUM_AND_ABOVE` in the REST section | Vertex safety filters | We set the thresholds explicitly on every request instead of relying on a default. |
| The TTS model "treats the `text` field strictly as a verbatim transcript" | Speech generation guide (`gemini-3.8-flash-tts`) | Prompt injection is probably not a risk. Phase 1 confirms this with a probe (see Injection). |

Exceptions for "educational, documentary, scientific, or artistic" use are
granted at Google's discretion. We plan as if they never apply.

## Current state (origin/main, 2026-11-22)

Speech surfaces and their current checks:

| Surface | Entry | Check today |
| --- | --- | --- |
| Cheer and resub messages | `app/twitch/sesame/modules/alerts_voice.go` `skipReason` | `blockedSpeech` on the message only |
| `!tts` | `app/twitch/sesame/modules/tts.go`, `modules/speech.go:32` | `blockedSpeech` on the message only |
| `speak` channel points reward | `modules/speech.go:32` | same as `!tts` |
| `speak` recipe step | `app/twitch/sesame/engine/automation_action_speak.go` | `blocksSpeech` on the expanded text |
| Dashboard voice test | `modules/voice_test_rpc.go:112` | fixed localized sample plus the request's `DisplayName`, unchecked |

Gaps:

1. **Only the streamer's own list is checked.** `blockedSpeech` calls
   `automod.Config.BlocksText` (`app/twitch/sesame/automod/config.go:201`),
   which returns false whenever the channel has no `block_terms`. The built-in
   lexicon (`internal/moderation/artifact/*.txt`) and `moderation.CheckFloor`
   guard bot chat output (`engine/pipeline_output.go:173`) and several
   validators, but never speech.
2. **Viewer names are not checked.** The name is added after the check:
   `modules/speech.go:37` for chat and rewards, `displayName` in
   `alerts_voice.go` `job` for alerts, `DisplayName` in the voice test.
3. **Recipe text can carry third-party content.** Variable expansion can call
   the Gossip custom-fetch endpoint (`app/twitch/sesame/context.md`), so a
   recipe can speak whatever a remote URL returns.
4. **The lexicon is tiny:** 11 hate, 6 harassment, 7 sexual and 12 profanity terms.
5. **Tags and style are already safe.** `voiceplan.PlainText` replaces `<` and
   `>` with spaces (`internal/domain/voiceplan/speech.go:34,49`), so viewer
   text never reaches the tag parser in `providers/tts/shape.go`. Style is a
   preset id resolved through `voiceplan.Style`
   (`internal/domain/voiceplan/alerts.go:13-21`).
6. **No record of refusals.** A Vertex block maps to `errRefused`
   (`providers/tts/vertex_response.go`) and the voice worker drops the job
   (`app/gossip/internal/voice/handler.go:39`, `jobRefused`). The precise reason
   is discarded, nothing is counted, and the same viewer can keep producing refusals.
7. **Refusal classification is too wide.**
   - `emptyAudioError` (`vertex_response.go:66-71`) treats every non-`STOP`
     finish reason with no audio as a refusal, including `OTHER` and
     `MAX_TOKENS`, which are usually transient.
   - `upstreamFailure` (`tts.go:274`) turns every 4xx into a refusal on both backends.

   Counting these as abuse would produce false strikes.
8. **Gemini API fallback exists.** `Config.backend` (`tts.go:109-113`) uses
   `GEMINI_API_KEY` when Vertex credentials are absent.
9. **No safety settings are sent.** `vertexRequest` has no `safetySettings`
   (`vertex.go:118-121`), so the default applies, and the docs disagree about
   what the default is.
10. **Mods cannot stop a clip.**
    - Ingress forwards `channel.ban` only for the bot's own moderation and
      drops viewer bans (`app/twitch/ingress/lib/ingress/shard_session.ex:531-545`).
    - Outgress subscribes to no `channel.chat.message_delete`,
      `channel.chat.clear_user_messages` or `channel.chat.clear` events
      (`app/twitch/outgress/internal/twitch/eventsub.go`).
    - The gateway TTS modules (`app/controls/gateway/lib/controls_gateway/tts/`) have no skip action.

## Design

### Placement

```
name, message ─> Sesame: CheckSpeech(input, policy) ─> compose utterance ─> publish job ─> Gossip worker
recipe text ──┘      (deny reply, refund, strike)                                              │
                                                                                               v
                       breaker check ─> CheckSpeechFloor(composed text) ─> Vertex ─> classify outcome ─> strike/breaker
```

- **Shared code.** `CheckSpeech` and `CheckSpeechFloor` live in
  `internal/moderation`, beside the existing normalizer and matcher. The
  strike, block and breaker state types live in `pkg/valkey` as one definition
  used by both services.
- **Sesame runs the full check** on the name and the message separately, before
  composing `alerts.voice.said`. It applies the floor, personal data and the
  channel tier. It keeps today's deny reply, reward refund and recipe
  `speech_blocked` behavior, and records the strike itself (see Strikes).
- **Gossip runs the floor only** on the composed text, in
  `synthesizer.synthesize` (`providers/tts/tts.go`) after `Shape` and before
  `validate`. This is the last hop before Google and covers every caller of
  `tts.synthesize`.
  - Gossip never sees streamer `block_terms`. Those often hold a streamer's real
    name or address, and they stay out of the job stream.
  - Personal data is not re-checked in Gossip, because the composed text
    includes the viewer name.
  - A Gossip block returns the existing `Reason: "refused"` path with a local
    reason, so the worker drops the job without retrying.
- **Gossip needs a new `COPY internal/moderation` line** in
  `app/gossip/Containerfile`, per AGENTS.md.

### Policy tiers

| Tier | Contents | Who controls it | Allow terms override |
| --- | --- | --- | --- |
| Floor | slurs and hate, explicit sexual acts, self-harm encouragement ("kys" family), explicit real-world threats ("I know where you live"), CSAM-adjacent terms | nobody, always on | no |
| Personal data | email, phone, card number in the message | nobody, always on | no |
| Channel | the chat lexicon categories `harassment`, `sexual` and `profanity`, each following the streamer's resolved automod section, plus streamer `block_terms` | streamer | yes, `allow_terms` exempt channel-tier hits only |

- **The floor is deliberately narrow** (see Twitch tone). Insults and trash talk
  are channel tier. Google's harassment filter is the backstop for anything
  severe that slips through, and its refusals are counted by the breakers.
- **The chat harassment list** (`artifact/harassment.txt`) is mostly
  self-harm phrases. For speech, those phrases are copied into the floor
  self-harm file, except "go die", which stays channel tier as banter.
- **The chat sexual list** is mostly words, not acts ("porn", "hentai"). It
  stays channel tier. The floor sexual file lists explicit acts and phrases
  only.
- **Game violence never matches.** The threat file has only phrases about the
  real world (a home, a family, finding someone, swatting, doxxing). The
  false-positive corpus pins game phrases as passing.
- **Disabling automod** (`Config.Disabled`) turns off the channel tier only.

### CheckSpeech contract

```go
type SpeechInput struct {
	Name    string // viewer display name from event data; empty when none
	Message string // viewer text, reward text or expanded recipe text
}

type SpeechPolicy struct {
	Profanity  bool
	BlockTerms [][]byte
	AllowTerms [][]byte
}

type SpeechVerdict struct {
	Message  string       // message to compose; differs from input only by removed links
	Blocked  bool
	Field    SpeechField  // name or message
	Reason   SpeechReason // floor_hate, floor_harassment, floor_sexual, floor_threat, pii, channel_profanity, channel_term
}

func CheckSpeech(in SpeechInput, policy SpeechPolicy) SpeechVerdict
func CheckSpeechFloor(text string) (SpeechReason, bool)
```

- Pure functions with no I/O. The returned `Message` must be stable when
  checked again (same `Message`, not blocked), pinned by a fuzz test.
- `Message` is only ever shortened, by removing links. Words are never masked
  or replaced, because rewriting text past a filter is the circumvention Google
  prohibits.
- A block drops the whole utterance, as `blockedSpeech` does today. A blocked
  name blocks the utterance even when the message is clean.
- `automod.Config` gains an exported `SpeechPolicy()` accessor.
- Link removal happens in Sesame before `CheckSpeech`. It uses a new exported
  `automod.StripLinks` built on the link token path (`automod/clips.go:12-45`)
  and `automod/linkcheck` (`NextToken`, `TrimLinkToken`, `HostOf`). Gossip
  runs the floor only and never needs link detection, so `linkcheck` stays
  where it is.
- `containsAny` (`automod/config.go:220`) moves to
  `moderation.ContainsAnyTerm`, and chat automod calls it from there.

### Matching

Every floor scan works on two copies of the text: a **literal copy** (NFKC
and lowercase only) and a **folded copy** (built by steps 2 to 4 below).

1. **Mask stage (literal copy).** A token of letters plus `*` or `#`, with at
   least one letter, matches a floor term of the same length when each masked
   position is a wildcard and every letter agrees ("f*ck", "n****r"). At least
   two letters must agree, so "s***" never matches a four-letter hate term.
   Mask entries come from the uncollapsed lexicon. A mask-only hit blocks the
   utterance but carries strike weight 0, because self-censored profanity is
   usually not abuse. This stage runs before leet folding, so `@` and `$`
   keep their leet meaning in the folded copy.
2. **Normalize** with the existing `moderation.Normalize` (NFKC, confusables,
   leet folding gated by token quorum, invisible-character removal;
   `internal/moderation/skeleton.go:143`).
3. **Speech folds,** applied to the folded copy only:
   - **Single-letter runs:** three or more one-letter tokens in a row are
     joined ("n i g g a"). A run of two is left alone.
   - **Separators inside a word:** any punctuation between two letters is
     removed (`. - _ * , / | \\ + ' :`), as in "f.u.c.k" and "f/u/c/k".
   - **Repeats:** runs of three or more of the same letter collapse to two.
     Lexicon entries keep their own spelling, so double letters in real terms
     still match. Every token with a doubled letter (a run of two after the
     collapse) is also tried with that run reduced to one, so a doubled-letter
     spelling of a single-letter term still matches. Keys that land on an
     innocent word from the collision list in the false-positive corpus
     ("niger", "fagot") are skipped.
4. **Phonetic key** (floor hate file only, where misspelling to evade is common and the terms are specific enough to keep false positives low):
   - A small fixed table (`ph`→`f`, `ck`/`q`→`k`, trailing `z`→`s`, `x`→`ks`)
     is applied to both the folded input and the floor lexicon. Vowels are
     not collapsed, to keep innocent words apart.
   - The table lives in one file as one list.
   - Every key that collides with a word in the false-positive corpus is
     rejected by a test.
5. **Word-bounded scan** of the folded copy with the Aho-Corasick matcher
   (`internal/moderation/ahocorasick.go`). Lexicon entries may be phrases, so
   multi-word homophones are listed as phrases.
6. **Joined scan** for chunked splits ("nig ger", "fu ck"). Only runs of
   adjacent tokens of three letters or fewer are joined, so ordinary words
   never fuse across a boundary ("brag headline" stays apart). Only floor terms
   marked `*` are scanned on the joined runs. Only terms of five or more
   letters with no innocent superstring in the corpus may carry the mark. Each
   one needs corpus entries.
7. **Channel tier.**
   - Profanity uses the word-bounded lexicon scan from step 5, so "class" does
     not match.
   - Streamer `block_terms` and `allow_terms` use the shared `containsAny`
     substring match on the output of step 2, as in chat automod today
     (`automod/config.go:220`).

All six site locales are scanned on every utterance (en, fr, de, es, pt-br, ru;
`locales/`), because viewers write in any language regardless of the
channel's locale.

### Accepted residual risk

- **Languages:** the model supports far more languages than our six. Gaps in
  other languages fall to Google's filter, and they are counted by our breakers.
  Decision 2 (classifier) is the lever if telemetry shows this happening.
- **Split across messages:** one slur split across two `!tts` messages becomes
  two separate clips, at least the user cooldown apart (default 30 s,
  `voiceplan.ChatDefaultUserCooldown`).
- **Street addresses** are not pattern-matched, because false positives would
  be too common. They fall to moderator controls.

### Lexicon

- **New speech floor files** in `internal/moderation/artifact/speech/`:
  - `hate.txt`, seeded from the chat `hate.txt`.
  - `sexual.txt`, explicit acts and phrases only.
  - `selfharm.txt`, the "kys" family, seeded from the chat `harassment.txt`
    without "go die".
  - `threat.txt`, real-world threat phrases only.

  The floor reads only these files. The channel tier reads the existing chat
  files. Those chat files, chat automod, `CheckFloor`, Projector and the other
  validators that read them (`app/projector/main.go:81`,
  `engine/pipeline_output.go:173`) are unchanged.
- **Format:** one term or phrase per line, `#` comments, a `*` prefix for
  joined-scan terms, any of the six languages. The speech loader parses the
  prefix. The chat `parseTerms` (`internal/moderation/lexicon.go:113`) never
  sees speech files.
- **Embedded only.** The speech lexicon is compiled in so Sesame and Gossip
  always agree. `SESAME_AUTOMOD_LEXICON_DIR` (`app/twitch/sesame/main.go:161,206`)
  keeps overriding the chat lexicon only.
- **Size target:** hundreds of terms per floor category across the six
  languages. Built from a permissively licensed public list plus our own
  additions (Decision 1).

### Personal data

Checked on the message only (never the name). Checks run on the literal copy
from Matching step 1, because `Normalize` folds `@` to `a`
(`internal/moderation/charset.go:41-43`).

| Pattern | Rule | Action |
| --- | --- | --- |
| URL or bare domain | `automod.StripLinks` in Sesame, before `CheckSpeech` | removed from the message |
| Email | `local@domain.tld` shape | block, `pii` |
| Phone | a leading `+` followed by 8 or more digits, or 10 or more digits in one run, or a grouped shape (`3-3-4`, `(3) 3-4`) with spaces, dots or dashes | block, `pii` |
| IPv4 or IPv6 | dotted quad with each part 255 or less; IPv6 colon groups | removed from `Message` (version strings such as "1.2.3.4" share the shape, so a block would be too costly) |
| Card number | 13 to 19 digits passing Luhn | block, `pii` |

These must not block (false-positive corpus): countdowns ("10 9 8 7 6 5 4 3"),
number lists ("10 20 30 40 50"), years and dates, "1000000", scores, "1v1",
bit amounts, version strings.

### Injection

Google's guide says the TTS model treats `text` as a verbatim transcript.
Our style already travels outside the transcript on both backends (Vertex
`speechMetadata.style`, Gemini API `speech_metadata` annotation).

- **Phase 1 probe.** Run once against the prod model with benign inputs:
  - "Say cheerfully: hello"
  - "Speaker 1: hi Speaker 2: bye"
  - "*laughs* hi"

  Record whether the audio reads the words literally. The probe sends `!tts`
  on a test channel with Premium voice enabled. The dashboard voice test speaks
  a fixed sample (`modules/voice_test_rpc.go:112`), so it cannot carry these
  inputs.
- **If the reading is verbatim,** no injection rule ships.
- **If it is not verbatim,** a directive rule ships in phase 2:
  - Scope: a direction verb at the start of the message followed within 60
    characters by `:`, and speaker labels at the start of a sentence.
  - Verbs: one table per locale.
  - Field: the message only, never the name, so a viewer named Lee or Sing
    is unaffected.
  - Action: the utterance is blocked with reason `injection`. It counts as no
    strike, because the probe found that the model can be steered, not that
    anyone meant harm.

### Job contract

`voiceplan.AlertJob` gains:

```go
ViewerID        string       `json:"viewer_id,omitempty"`         // Twitch user id from event data; empty when none
Source          SpeechSource `json:"source,omitempty"`            // viewer or channel
SourceMessageID string       `json:"source_message_id,omitempty"` // chat message id for !tts; empty otherwise
NotBefore       time.Time    `json:"not_before,omitzero"`         // hold end; zero means no hold
```

- `ViewerID` comes from event data only: chatter, cheerer, resub user or
  redeemer id. There is no login resolution.
- `Source` is `viewer` for alerts, `!tts` and rewards, and `channel` for
  recipes and the voice test. Recipe text is written by the streamer, so it
  never strikes a viewer. Otherwise a recipe could speak a slur on viewer X's
  trigger and get X blocked everywhere.
- Older jobs without the new fields decode to empty values. They get the floor
  in Gossip and no viewer strike, so a rolling deploy blocks no less than
  phase 1 does.
- `gossiprpc.Request` is unchanged apart from the new `RefusalClass` reply
  field. Strikes are recorded by the voice worker (`voice.Handler`), which
  holds the job and its `ViewerID` and `Source`.
- Deploy order: Gossip first (reads the new fields), then Sesame (writes them).

### Safety settings

Every Vertex request sends `safetySettings` explicitly, so behavior does not
depend on a default the docs disagree about:

| Category | Threshold | Why |
| --- | --- | --- |
| `HARM_CATEGORY_HATE_SPEECH` | `BLOCK_MEDIUM_AND_ABOVE` | matches our floor |
| `HARM_CATEGORY_SEXUALLY_EXPLICIT` | `BLOCK_MEDIUM_AND_ABOVE` | matches our floor |
| `HARM_CATEGORY_HARASSMENT` | `BLOCK_ONLY_HIGH` | banter and trash talk would otherwise refuse and strike |
| `HARM_CATEGORY_DANGEROUS_CONTENT` | `BLOCK_ONLY_HIGH` | game violence would otherwise refuse and strike |

The Gemini API backend (non-production only) sends the same four thresholds
through the Interactions API `safety_settings` field. These are configurable
thresholds Google offers. Choosing them is
not circumventing a filter. The prohibited-use rules still apply to what we
send, which the floor covers.

### Refusal classification

Each Google outcome is classified before any counting:

| Google outcome | Class | Retried | Viewer strike | Breaker refusal |
| --- | --- | --- | --- | --- |
| `blockReason` `SAFETY`, `PROHIBITED_CONTENT`, `BLOCKLIST`, `JAILBREAK`, `MODEL_ARMOR` | `policy` | no | 2 | yes |
| `finishReason` `SAFETY`, `PROHIBITED_CONTENT`, `SPII`, `BLOCKLIST`, `MODEL_ARMOR` with no audio | `policy` | no | 2 | yes |
| `blockReason` `OTHER` or `BLOCKED_REASON_UNSPECIFIED` | `policy_unspecified` | no | 1, this channel only | yes |
| `finishReason` `OTHER`, `MAX_TOKENS`, `STOP`, `MALFORMED_RESPONSE`, `FINISH_REASON_UNSPECIFIED` with no audio | `empty` | once | 0 | no |
| `finishReason` `RECITATION` | `invalid` | no | 0 | no |
| Any enum value not listed above | `unknown` | no | 1, this channel only | yes, and logged at warn |
| 4xx other than 429, without a safety block in the body | `invalid` | no | 0 | no |
| 429, 5xx, timeout | existing rate-limit and unavailable paths | existing | 0 | no |

- `blockReason OTHER` is documented as covering unsupported languages too, so
  it never blocks a viewer across channels. It still counts once in that
  channel, so one viewer cannot open the channel breaker repeatedly without
  being blocked there first.
- `errRefused` splits into these classes on both backends. The reply
  `Reason` stays `"refused"` for every non-retried class, so
  `voice/handler.go` keeps dropping the job. A new `RefusalClass` reply field
  carries the detail.
- For `empty`, the worker returns a `bus.JobRetryError` with a two-second
  delay on first delivery (`GossipJobMessage.Delivered`). A second `empty`
  drops the job.

### Strikes, blocks and breakers

State lives in Valkey, through one definition in `pkg/valkey` (a new
`SpeechSafety` type) used by both services. No service issues raw commands
(AGENTS.md). Sesame and Gossip already share the Valkey deployment
(`deploy/k8s/sesame.yaml:115`, `deploy/k8s/gossip.yaml:199`).

**Strike sources:**

| Source | Writer | Weight |
| --- | --- | --- |
| Sesame floor block, `Source` viewer | Sesame | 1 (0 for a mask-only hit) |
| Sesame channel-tier block (swearing, insults, streamer terms) | Sesame | 0, the streamer's own rule, not abuse |
| Sesame personal data block | Sesame | 0 (often accidental) |
| Gossip floor block, `Source` viewer | Gossip | 1 |
| Google `policy` refusal, `Source` viewer | Gossip | 1 |
| Google `policy_unspecified` or `unknown` refusal, `Source` viewer | Gossip | 1, this channel only |
| Any block or refusal, `Source` channel | Gossip | 0, counted toward the channel breaker only |

- **Idempotent.** A strike is recorded at most once per job or event id, using a
  `FreshMemberSet` marker with the window TTL, so redelivery never double counts.
- **Small overshoot under concurrency** (several jobs in flight when a threshold
  is crossed) is accepted. Thresholds are soft.

**Effects:**

| Breaker | Trigger | Effect | Ends |
| --- | --- | --- | --- |
| Viewer, per channel | strike count in a rolling window | Sesame denies, Gossip drops that viewer's jobs in that channel | after a fixed period, or a moderator unblocks |
| Viewer, all channels | strike count across channels in a rolling window | same, in every channel | after a fixed period |
| Channel | Google refusals in a rolling window | channel TTS paused | auto-resume after a fixed period |
| Global | Google refusals across all channels in a rolling window | all synthesis paused, critical alert | half-open after a cool-down: one job in ten is let through, one refusal re-opens, a clean window closes |

- **Thresholds.** The values live in Gossip and Sesame config from Doppler, not
  in the repo, so trolls cannot read the exact limits. The viewer thresholds
  are set below the channel breaker, so a single viewer is blocked before
  they can pause a channel. Starting values are agreed outside this spec
  (Decision 4).
- **Where checks run.** Sesame checks the viewer state before composing, so the
  viewer gets the normal deny reply. Gossip checks every state again before
  calling Google, and drops the job with reason `paused` (no retry, since a
  clip that plays late is worse than none).
- **Operator pause flag.** A `pkg/valkey` `VersionedFlag` pauses all synthesis
  without a deploy.

### Moderator controls

1. **Viewer block list** (phase 4).
   - **Manual blocks** are durable streamer configuration. They live in a new
     table in the overlays schema (`app/db/overlays`), beside the TTS settings
     it already owns. Sesame reads them through Projector with the rest of
     `rpc.internal.projector.overlays.tts.settings` and enforces them before
     composing. Gossip does not read them, because a blocked viewer's job is
     never published.
   - **Strike blocks** stay in Valkey (`SpeechSafety`) with their TTL. Valkey
     holds no durable state in this design.
   - The dashboard voice page lists manual blocks and active strike blocks with
     their end times, and offers Unblock for both. It may search viewers by
     name, as AGENTS.md allows. Manual blocks are written through the overlays
     RPC. Clearing a strike block goes through the same RPC, which calls
     `SpeechSafety` in `pkg/valkey`.
   - Chat has no block command, because `@mention` would need login resolution.
   - The paused notice needs the channel breaker state. `dashboard.voice_get`
     is answered by the users service (`app/db/users/rpc/voice.go:58`), which
     gains a read-only `SpeechSafety` call. Writers stay Sesame and Gossip.
     Copy goes in all six locales and passes `check-i18n.mjs`.
2. **Hold and cancel** (phase 5, Decision 3).
   - `!tts` and reward jobs carry `NotBefore`. The worker delays the job until
     then, then synthesizes only if no cancel marker exists for
     `SourceMessageID` or for the viewer in that channel.
   - The cancel marker is part of `SpeechSafety`. Sesame writes it on deletion
     of the source message, a clear of that user, a chat clear, or a ban or
     timeout of that user.
   - This needs `channel.chat.message_delete`,
     `channel.chat.clear_user_messages` and `channel.chat.clear` subscriptions
     in outgress `eventsub.go`, and Ingress forwarding viewer `channel.ban` to
     Sesame instead of dropping it (`shard_session.ex:531-545`). Ingress still
     publishes bot moderation to its control subject unchanged.
   - Scopes for each subscription must be confirmed against live grants first.
     Grants older than 2026-10-12 lack moderation scopes, so on those channels
     the hold still runs but nothing can cancel.
   - A cancelled clip never reaches Google.
3. **Overlay skip** (Decision 5, not designed here).

### Provider configuration

- Prod runs Vertex only. When `APP_ENV` resolves to `production` (the
  default when unset, `pkg/svcboot/svcboot.go:26,31-36`), the TTS provider
  never builds the Gemini API backend. Without Vertex credentials it disables
  itself, as it does today when both keys are absent (`providers/all.go:184`),
  and the rest of Gossip starts normally. Non-production environments may
  keep the Gemini API path.
- **No per-end-user label.** The Vertex `generateContent` reference documents
  `labels` as "used for billing and reporting only" and names no
  `safety_identifier` field (checked 2026-11-23), so no label is sent.

### Telemetry and privacy

- New Relic attributes:
  - Sesame, on the message transaction that handled the speech surface:
    `tts.gate` (`pass` or a `SpeechReason`) and `tts.breaker` when a viewer
    block denied the request.
  - Gossip, on the voice job and synthesize transactions: `tts.gate`,
    `tts.refusal` (class and raw enum), and `tts.breaker` (`viewer`,
    `channel` or `global`) when one fired.
- Never record the utterance, the matched term or the viewer name in logs or
  telemetry. Record category and reason only.
- Strike state holds ids and counts only, with TTLs equal to their windows.
- Alert conditions (global breaker open, Google refusal rate) are configured
  outside git.

### Streamer and viewer copy

- The deny reply (`tts.blocked`, `replyDenied`) stays generic and never names
  the category or term, so probing gets no signal.
- Terms of service and privacy policy state that viewer TTS text is processed
  by Google and that streamers choose to enable TTS. The wording belongs to the
  legal pages owner and is out of this spec.

## Tests

All tests go through exported seams, with policy pins as single tables.

| Seam | Test |
| --- | --- |
| `moderation.CheckSpeech`, `CheckSpeechFloor` | One table of input, policy and expected verdict. Covers every mask, fold, repeat, phonetic, joined-scan and personal-data rule, in every locale, for both the name and the message fields. |
| same | False-positive corpus table, run with every channel section off: "niger", "fagot", "Scunthorpe", "assassin", "class", place names, chemistry and medical terms, countdowns, years, dates, scores, emote names, common names (Lee, Di, Sing), and Twitch banter ("I'll kill you", "headshot", "you're trash", "gg ez", "uninstall", "go die", "this streamer sucks", swearing). Every entry must pass the floor. |
| same | `FuzzCheckSpeech`: message stability and no panics, seeded from the existing `FuzzNormalize` corpus. |
| Sesame pipeline `Process` | A blocked name blocks the utterance. The floor applies with automod disabled. An allow term cannot unblock a floor hit. Reward refund on block. Recipe reports `speech_blocked`. A Sesame strike is written once per event. A blocked viewer is denied. |
| Gossip `tts.synthesize` handler | A fake backend returns each Google outcome in the classification table, and the test checks class, retry and drop. An old job without new fields gets the floor and no strike. |
| Gossip voice `Handler.Handle` | Strike weights by `Source`, idempotence on redelivery, every breaker opening and closing, global half-open, paused drop, cancel-marker drop, with the local Valkey test server (`VALKEY_TEST_ADDR`). |
| Gossip config | Prod start fails without Vertex credentials. |

The corpus contains offensive terms by necessity. It lives under
`internal/moderation/testdata/`, like the existing lexicon artifacts.

## Rollout

Each phase is a separate PR based on `main`, merged and released in order.

1. **Floor.**
   - `CheckSpeech` and `CheckSpeechFloor` with today's chat lexicon plus the
     first speech files.
   - Matching steps 1 to 7, personal data, the false-positive corpus, link removal.
   - Sesame and Gossip call sites, the name check, the voice test name check.
   - New job fields, the Gossip Containerfile line, explicit safety settings,
     the prod Vertex-only check.
   - The injection probe is run and its result recorded in this spec.
   - Enforced from release, with no shadow mode. Each day without it sends
     unfiltered text to Google.
2. **Lexicon expansion** (Decision 1), with corpus additions, and the
   directive rule only if the probe showed the model can be steered.
3. **Refusal classification, strikes, breakers, operator pause flag,
   telemetry.**
4. **Viewer block list and paused notice** in the dashboard.
5. **Hold and cancel** (Decision 3, after scope checks on live grants).
6. **Overlay skip** (Decision 5).

## Decisions needed

1. **Lexicon source.** Recommendation: a permissively licensed public
   multilingual list as a vendored data file (attribution kept in the file
   header), plus our own terms. This adds third-party data to the tree.
2. **Pre-send classifier.** Optionally call a moderation classifier after the
   lexicon passes: Google Cloud Natural Language text moderation, or the Rust
   automod lane. It catches insults no word list covers, and languages beyond
   our six, at the cost of one more call per clip and a new dependency.
   Recommendation: defer until phase 3 telemetry shows the refusal rate.
3. **Hold length** for `!tts` and rewards: off, 3 s or 5 s. Recommendation:
   5 s for `!tts` and rewards, none for cheers and resubs.
4. **Starting thresholds** for the four breakers. They are stored in Doppler,
   and the proposed values are kept out of this public file.
5. **Overlay skip:** whether it ships in this program or separately.
6. **Floor scope:** decided 2026-11-22. The floor is narrow for Twitch tone,
   and insults and crude words are channel tier (see Policy tiers).

## Out of scope

- Chat automod behavior and the chat lexicon files.
- The free embedded TTS model planned for the free tier. It sends nothing to
  Google, but it should reuse `CheckSpeech` when built, because stream safety still applies.
- Voice cloning.
