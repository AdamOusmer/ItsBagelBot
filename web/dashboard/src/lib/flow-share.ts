// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { FlowImportEntryView, FlowImportResultView, Issue, ScrubNote } from '@bagel/kit/automations';
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
  scrubbed?: { name: string; notes: ScrubNote[] }[];
  failed?: string[];
};

export type ScrubbedPlace = { name: string; step: number | null; type: string };

const STEP_OF_PATH = /^actions\[(\d+)\]/;

export function scrubbedPlaces(scrubbed: NonNullable<FlowExportReply['scrubbed']>): ScrubbedPlace[] {
  const places = new Map<string, ScrubbedPlace>();
  for (const { name, notes } of scrubbed) {
    for (const note of notes) {
      const index = STEP_OF_PATH.exec(note.path)?.[1];
      const step = index === undefined ? null : Number(index) + 1;
      places.set(`${name}\u0000${step}\u0000${step === null ? '' : note.type}`, { name, step, type: note.type });
    }
  }
  return [...places.values()];
}

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
