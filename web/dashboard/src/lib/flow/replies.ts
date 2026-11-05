// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import type { Issue } from '@bagel/kit/automations';
import type { FlowWarning, UpgradeReport } from '#lib/server/flow-store';
import type { FlowRef } from './ref';

export type SaveReply =
  | { ok: true; id: string; name: string; enabled: boolean; revision: string; warnings: FlowWarning[]; upgrade?: UpgradeReport }
  | {
      ok: false;
      reason: 'invalid' | 'conflict' | 'needs_upgrade' | 'not_found' | 'failed' | 'denied';
      issues?: Issue[];
      upgrade?: UpgradeReport;
      error?: string;
      latestRevision?: string;
    };

export type ToggleReply =
  | { ok: true; enabled: boolean; revision: string; warnings: FlowWarning[] }
  | { ok: false; reason: 'conflict' | 'not_found' | 'failed' | 'denied'; error?: string };

export type DeleteReply = { ok: true } | { ok: false; reason: 'not_found' | 'not_deletable' | 'failed' | 'denied'; error?: string };

export type SaveRequest = {
  flow: { name: string; enabled: boolean; nodes: unknown[]; edges: unknown[]; queue?: string };
  ref: FlowRef | null;
  expectedRevision?: string;
  confirmUpgrade?: boolean;
};
