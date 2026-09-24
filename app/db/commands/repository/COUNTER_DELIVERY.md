# Command-use delivery

The producer still groups command uses into five-second batches. Each batch has a stable ID. The commands consumer uses the existing bus consumer acknowledgment and retry behavior; it returns success only after the database transaction commits. The transaction writes the batch receipt and increments the matching command together. Replaying a committed batch has no additional effect. Legacy events use the bus message UUID when no batch ID is supplied.

The command `uses` column is signed BIGINT with a nonnegative database check and Ent validation allowing 0 through 9,223,372,036,854,775,807. An atomic update predicate guards addition before it occurs. Overflow rolls back the receipt and increment so the failed event is not silently marked committed. A deleted or missing command consumes the receipt without recreating a command, and replay after recreation cannot affect the replacement.

Deploy the generated schema migration before the new consumer. Check existing `commands.uses` rows for values above the ceiling before changing the unsigned column to signed or enabling the check. The existing automatic Ent migration creates `command_use_batches` and applies the schema change; a database user must have the corresponding migration privileges.

Receipts are retained permanently because arbitrary historical event replay is supported. Storage grows by one receipt per producer batch, not per command execution. Do not prune receipts independently of event replay eligibility: pruning permits a previously committed event to increment again. A bounded retention policy requires a matching enforced maximum replay age and accounting for broker backups and restored producers.

This protects database increments after a batch reaches the broker. It does not recover unpersisted producer memory after a process crash. Command-change notifications remain best effort; the database counter is authoritative. Transactions rely on normal database durability and preservation of the receipt table alongside counter backups.

Command-use deltas are signed int64. Command uses in change events and projections are decimal JSON strings to preserve every digit for JavaScript clients.

New Go readers accept both historical numeric Uses JSON and decimal strings. For an uninterrupted mixed-version rollout, deploy compatible readers before enabling string-writing versions: old binaries cannot decode the new string representation.
