// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { FlowImportEntryView, FlowImportResultView, Issue } from '@bagel/kit/automations';
import type { FlowRef } from '#lib/server/flow-store';

export type ShareableFlow = { ref: FlowRef; name: string };

export function flowKey(ref: FlowRef): string {
  if (ref.layer === 'automation') return `automation:${ref.id}`;
  if (ref.layer === 'command') return `command:${ref.name}`;
  return `alert:${ref.alert}`;
}

export type FlowExportReply = {
  ok: boolean;
  error?: string;
  file?: string;
  exported?: number;
  needSetup?: number;
  missing?: number;
  failed?: string[];
};

export type FlowImportPreviewReply = {
  ok: boolean;
  error?: string;
  code?: string;
  issues?: Issue[];
  entries?: FlowImportEntryView[];
  slots?: number;
};

export type FlowImportCommitReply = {
  ok: boolean;
  error?: string;
  code?: string;
  issues?: Issue[];
  results?: FlowImportResultView[];
};
