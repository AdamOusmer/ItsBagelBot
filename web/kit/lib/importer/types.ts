// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { Perm } from '../types';
export type { Perm };

export type ImportSource =
  | 'streamelements'
  | 'fossabot'
  | 'moobot'
  | 'nightbot'
  | 'streamlabs_desktop'
  | 'wizebot';
export const IMPORT_SOURCES: readonly ImportSource[] = [
  'streamelements',
  'fossabot',
  'moobot',
  'nightbot',
  'streamlabs_desktop',
  'wizebot'
];

export interface ImportDiagnostic {
  severity: 'warn' | 'error';
  item_index: number;
  code: string;
  message: string;
}

export interface ManifestCommand {
  name: string;
  aliases?: string[];
  responses?: string[];
  source_responses?: string[];
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

export interface ManifestFetch {
  name: string;
  url?: string;
  json_path?: string[];
  source: ImportSource;
}

export interface AutomodTerms {
  block?: string[];
  allow?: string[];
}

export interface ImportManifest {
  commands?: ManifestCommand[];
  timers?: ManifestTimer[];
  triggers?: ManifestTrigger[];
  quotes?: ManifestQuote[];
  fetches?: ManifestFetch[];
  automod?: AutomodTerms;
}

export interface CollisionRef {
  kind: string;
  name: string;
}

export interface ImportStats {
  commands: number;
  timers: number;
  triggers: number;
  quotes: number;
}

export const IMPORT_ITEM_CAPS = {
  commands: 2000,
  timers: 300,
  triggers: 1000,
  quotes: 5000
} as const;

export interface PreviewResponse {
  manifest?: ImportManifest;
  diagnostics?: ImportDiagnostic[];
  collisions?: CollisionRef[];
  stats: ImportStats;
  error?: string;
}

export interface CommitResponse {
  applied: ImportStats;
  skipped?: CollisionRef[];
  audit_id?: number;
  diagnostics?: ImportDiagnostic[];
  error?: string;
}
