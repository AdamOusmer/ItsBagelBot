# Migrate saved module reply templates

`module-template-migrate` converts recognized module reply variables from `{var}` to
`{module:var}` in saved configuration. It uses the shared module variable catalog,
changes only known reply fields, and preserves existing namespaced variables,
generic command variables, and unrelated configuration. Existing bare variables
continue to render during rollout, so the migration can follow the code deploy.
The conversion also covers saved legacy songqueue chat reply keys and queue
opened/closed replies that are absent from the editable reply catalogue, using
the exact fields available to each reply.

The default run is a dry run. It selects module rows in ID order with bounded
pages and prints row IDs, module names, revision numbers, changed key names, and
totals. It prints neither connection secrets nor configuration contents, creates
no backup, makes no RPC connection, and performs no database writes.

## Prepare access

Deploy the updated modules service before applying the migration. The CLI uses
the new `patch-existing` RPC verb: an older service has no handler for that verb,
so it cannot apply a patch without the row identity guard. Existing bare reply
variables continue to render while the deployment and migration are staged.

Provide a MySQL DSN in `MODULES_MIGRATION_DSN` using your normal secret injection.
Use a SELECT-only database account for the modules database and include
`parseTime=true` in the DSN. `--dsn-env` selects another environment variable.
The tool does not run schema migrations or Ent mutations.

For apply, supply the normal modules-service RPC environment: `NATS_URL`, or
`NATS_RPC_URL` / `NATS_LEAF_URL` in split-plane deployments, and
`NATS_RPC_USER` / `NATS_RPC_PASSWORD` plus any configured TLS environment.
`--nats-url-env` selects a different fallback URL variable. The subject prefix is
`NATS_MODULES_SUBJECT_PREFIX`, defaults to `bagel.rpc.modules`, and can be set
with `--subject-prefix`.

## Preview and apply

From the repository root, preview a single channel first:

```sh
go run ./cmd/module-template-migrate --user-id 123456
```

Omit `--user-id` to scan all stored rows. `--page-size` defaults to 200 and is
bounded at 1000; `--timeout` bounds each read and RPC separately.

Apply after reviewing the dry-run output:

```sh
go run ./cmd/module-template-migrate \
  --user-id 123456 --apply --backup /secure/path/module-templates-2026-09-27.jsonl
```

The backup must be a new file. The tool opens it exclusively with mode `0600`;
it never appends to or overwrites an existing backup. Each prepared record stores
the complete original and intended config, the changed before/after fields,
enable state, and expected revisions. It writes and fsyncs that record before
sending any mutation. An outcome record is also written and fsynced afterward.
Backups contain private module configuration; store them with the same controls
as database backups.

Changes go through `modules.patch-existing` with `ExpectedID` and `ExpectedRev`,
preserving the row's enable state and letting the service publish normal projection/cache updates.
A conflict is skipped, refetched, and reported; the tool never overwrites the
concurrent edit or recreate a deleted/replaced row. Loyalty rows whose saved
account ownership differs from the service's current ownership also conflict;
the migration preserves their configuration for operator review. Rerun the dry run and use a new backup file for a later apply.
An RPC error stops the run because its outcome may be uncertain. Inspect the
affected row before retrying; an `uncertain` or unconfirmed operation is not
automatically restored. Invalid configurations are reported and skipped.

## Restore migrated fields

Preview rollback against the same environment:

```sh
go run ./cmd/module-template-migrate \
  --restore /secure/path/module-templates-2026-09-27.jsonl
```

Apply rollback with a separate new journal:

```sh
go run ./cmd/module-template-migrate \
  --restore /secure/path/module-templates-2026-09-27.jsonl \
  --apply --backup /secure/path/module-templates-rollback-2026-09-27.jsonl
```

Restore processes only confirmed successful mutations. It compares the current
values of the changed fields with the migration's recorded values, skips fields
edited afterward, and patches the original values under the current revision.
It preserves unrelated subsequent edits and the current enable state. The
original full config in the journal is retained for operator-assisted recovery;
the tool does not force a full configuration replacement.

After apply, rerun the dry run to confirm that no recognized legacy reply
variables remain. No production migration is performed just by building the CLI.
