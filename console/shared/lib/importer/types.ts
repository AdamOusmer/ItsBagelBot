// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Canonical config-import types — the single source of truth since the
// standalone importer service was folded into the dashboard (2026-08-23).
// Previously these mirrored internal/domain/rpc/importer/importer.go one-for-one
// as a NATS wire contract; that Go file is gone, so this module now DEFINES the
// shapes and console/shared/lib/types.ts re-exports them for import stability.
//
// ImportManifest is the stable intermediate representation every source parser
// produces and the commit path consumes. Field names are snake_case JSON,
// carried over unchanged from the old wire format so previews/commits already
// stored client-side keep deserializing.

// Type-only back-reference (erased at runtime, no import cycle): permission is
// the dashboard-wide domain enum. Re-exported so consumers of this module can
// name the tier type without reaching into the root barrel.
import type { Perm } from '../types';
export type { Perm };

// ImportSource names the supported import origins. The spelled-out values
// travel on the wire (form posts carry `source`), so they must never change.
export type ImportSource = 'streamelements' | 'fossabot' | 'moobot' | 'streamlabs_desktop';
export const IMPORT_SOURCES: readonly ImportSource[] = [
  'streamelements',
  'fossabot',
  'moobot',
  'streamlabs_desktop'
];

// ImportDiagnostic is one translation/validation finding. item_index addresses
// the manifest array the code's prefix names (command_/timer_/trigger_/
// quote_/counter_); -1 means manifest-level. Severity 'error' marks an item
// commit will skip, 'warn' a lossy-but-applied translation.
export interface ImportDiagnostic {
  severity: 'warn' | 'error';
  item_index: number;
  code: string;
  message: string;
}

// ManifestCommand is one canonical custom command in ImportManifest.
// responses is already split into chat-ready lines by the parser.
export interface ManifestCommand {
  name: string;
  aliases?: string[];
  responses?: string[];
  permission?: Perm;
  cooldown_seconds?: number;
  online_only?: boolean;
  warnings?: string[];
}

export interface ManifestTimer {
  message: string;
  interval_seconds: number;
  online_only?: boolean;
}

export interface ManifestTrigger {
  phrase: string;
  response: string;
}

export interface ManifestQuote {
  text: string;
  added_by?: string;
  created_at?: string;
}

export interface ManifestCounter {
  name: string;
  value: number;
}

export interface AutomodTerms {
  block?: string[];
  allow?: string[];
}

// ImportManifest is the canonical intermediate representation every parser
// produces and commit consumes. Every collection is whole: filtering happens
// client-side before commit (unchecked items are removed), never server-side.
export interface ImportManifest {
  commands?: ManifestCommand[];
  timers?: ManifestTimer[];
  triggers?: ManifestTrigger[];
  quotes?: ManifestQuote[];
  counters?: ManifestCounter[];
  automod?: AutomodTerms;
}

// CollisionRef names one existing channel item a manifest item would collide
// with; kind is 'command' | 'timer' | 'trigger' | 'quote' | 'counter'.
export interface CollisionRef {
  kind: string;
  name: string;
}

export interface ImportStats {
  commands: number;
  timers: number;
  triggers: number;
  quotes: number;
  counters: number;
}

// IMPORT_ITEM_CAPS bounds one import per collection. Raised only deliberately
// (2026-08-23, initial values chosen as ~10x the largest observed community
// exports and a latency ceiling for the commit write fan-out): the browser
// truncates to these before POSTing so a parsed manifest is bounded on the wire
// and in the review DOM, while the server re-enforces them against untrusted
// callers of lib/server/importer.
export const IMPORT_ITEM_CAPS = {
  commands: 2000,
  timers: 300,
  triggers: 1000,
  quotes: 5000,
  counters: 500
} as const;

// PreviewResponse renders the review screen. manifest is undefined when the
// source could not be fetched/parsed at all; error then explains why.
export interface PreviewResponse {
  manifest?: ImportManifest;
  diagnostics?: ImportDiagnostic[];
  collisions?: CollisionRef[];
  stats: ImportStats;
  error?: string;
}

// CommitResponse reports what landed. skipped lists collision-skipped items.
// audit_id is optional since the standalone service died: there is no
// import_audits table anymore (one structured log line per commit instead), so
// the local commit never sets it and the done screen omits the line. The DEMO
// fixture still mints one to exercise the summary markup.
export interface CommitResponse {
  applied: ImportStats;
  skipped?: CollisionRef[];
  audit_id?: number;
  diagnostics?: ImportDiagnostic[];
  error?: string;
}
