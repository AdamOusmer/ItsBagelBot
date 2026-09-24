# Counter integrity and rollout

## Range and representation

Message, command-use, loyalty custom, and feed counter totals and increments
use signed `int64`. The allowed stored range is `0..9223372036854775807`.
IDs remain unsigned where their domain requires it. At one increment for each of 60 messages per second, a
counter starting at zero reaches the limit after approximately 4.87 billion
Gregorian years.

MySQL `BIGINT` is exact in this range. Database checks and guarded writes reject
an increment that would exceed the limit; they do not wrap or silently cap it.
JavaScript `number` cannot represent every integer in this range, so counter
values cross browser-facing JSON as decimal strings. Dashboard comparison and
formatting use `bigint`; the public rate is an approximate number calculated
from exact differences. The projector's `ctr:board:v2:` members encode a
19-digit padded count and channel ID, avoiding floating-point sorted-set scores.

## Delivery path

- Sesame publishes decoded message and event totals with the existing
  `bus.PublishConfirmed` helper and a stable ID derived from the source message.
  Source processing returns an error until the broker confirms publication.
- Loyalty applies each counter batch and its receipt in one SQL transaction.
  The existing `bus.Consume` handler returns only after commit, so a broker
  redelivery cannot increment a committed batch twice.
- Command-use batches retain their IDs on retry. The commands service likewise
  commits a receipt and increment together before acknowledging the event.
- Feed lifetime and channel totals commit together in modules SQL, with an
  event receipt. The transient feed-today Valkey value is updated afterward.
- The projector validates the full signed range before changing live hashes or
  the versioned leaderboard. The dashboard rejects malformed values rather
  than rendering a rounded total.

Permanent feed totals and decoded message/event totals have a durable path.
Broker storage for these events is bounded: `BagelDataStream` currently retains
them for five minutes and at most 512 MiB. A consumer outage beyond either
limit can expire an event before its receipt is committed. Monitor consumer
lag and increase retention with capacity planning before relying on longer
outage recovery.
Other producer windows, including custom counter bumps, command uses, answered
counts, and moderation counts, remain in process memory until their flush.
A crash before publication can lose such a window. Command-use execution also
has cooldown and output effects that require a durable execution record to
replay independently after a publish failure. The feed-today value can miss a
committed feed if the process stops between SQL commit and its Valkey update;
the lifetime total remains correct.

## Capacity and retention

At 60 source messages per second, decoded bot and channel totals can produce
up to 120 confirmed events and SQL transactions per second. That is up to
10,368,000 receipt rows per day. The receipts are deliberately retained for
replay safety. Before production rollout, benchmark the loyalty write path at
the expected traffic and budget receipt storage, index growth, and backups.
Do not delete receipts by age without an enforced maximum replay age covering
the source, broker, publisher retries, and restored backups. A bounded replay
policy and receipt cleanup are separate capacity work.

## Deployment

1. Check existing rows before converting unsigned columns to signed or adding
   checks. In the commands database, inspect `commands.uses`; in loyalty,
   `counters.value` and `counter_entries.value`; in modules,
   `feed_counters.count` and `channel_feed_counters.count`. Any value above
   `9223372036854775807` needs an explicit correction or migration plan.
2. Apply the generated Ent schema changes and create the receipt tables before
   starting the new consumers. Keep receipt tables with counter backups and
   restores.
3. Roll out services that can read both legacy numeric and new string `uses`
   values before enabling the new string writer. Update the projector and
   dashboard together for `ctr:board:v2:`. The new board seed key causes an
   exact rebuild from stored counters.
4. Watch consumer retry rates, range errors, receipt table growth, projection
   seed lag, and the stats page's degraded state. A range rejection is a
   blocked event requiring investigation, not a successful count.

This uses established patterns: [MySQL signed BIGINT and overflow behavior](https://dev.mysql.com/doc/refman/8.4/en/out-of-range-and-overflow.html),
[CHECK constraints](https://dev.mysql.com/doc/refman/8.4/en/create-table-check-constraints.html),
and [JavaScript's exact-integer limit](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Number/MAX_SAFE_INTEGER).
