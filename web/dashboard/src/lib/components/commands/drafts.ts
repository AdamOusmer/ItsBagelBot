// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { Perm } from '@bagel/kit';

export interface CommandDraft {
  edit: boolean;
  name: string;
  originalName: string;
  aliases: string[];
  response: string;
  perm: Perm;
  cooldown: number;
  user_cooldown: number;
  allowed_user_id: string;
  bump_counter: string;
  stream_online_only: boolean;
  is_active: boolean;
  builtin?: boolean;
}

export type BoardId = string;

export interface DraftRef {
  board: BoardId;
  name: string;
  edit: boolean;
}

const PREFIX = 'bb-cmd-draft@';
const LEGACY_PREFIX = 'bb-cmd-draft:';

export function draftRef(board: BoardId, draft: Pick<CommandDraft, 'originalName' | 'edit'>): DraftRef {
  return { board, name: draft.edit ? draft.originalName : '', edit: draft.edit };
}

export function draftKey(ref: DraftRef): string {
  return `${PREFIX}${ref.board}:${ref.edit ? ref.name : 'new'}`;
}

export function discardLegacyDrafts(): void {
  try {
    const keys = Array.from({ length: sessionStorage.length }, (_, i) => sessionStorage.key(i));
    const legacy = keys.filter((key): key is string => key?.startsWith(LEGACY_PREFIX) === true);
    legacy.forEach((key) => sessionStorage.removeItem(key));
  } catch {}
}

type StoredDraft = Partial<Omit<CommandDraft, 'edit' | 'originalName' | 'builtin'>>;

const isString = (value: unknown): value is string => typeof value === 'string';
const isBoolean = (value: unknown): value is boolean => typeof value === 'boolean';
const isFiniteNumber = (value: unknown): value is number => typeof value === 'number' && Number.isFinite(value);
const isStringArray = (value: unknown): value is string[] => Array.isArray(value) && value.every(isString);
const isRecord = (value: unknown): value is Record<string, unknown> =>
  value !== null && typeof value === 'object' && !Array.isArray(value);

const STORED_FIELDS: Record<string, (value: unknown) => boolean> = {
  name: isString,
  aliases: isStringArray,
  response: isString,
  perm: isString,
  cooldown: isFiniteNumber,
  user_cooldown: isFiniteNumber,
  allowed_user_id: isString,
  bump_counter: isString,
  stream_online_only: isBoolean,
  is_active: isBoolean
};

function parseStoredDraft(raw: string): StoredDraft | null {
  const decoded: unknown = JSON.parse(raw);
  if (!isRecord(decoded)) return null;
  // Ignore stale metadata and removed fields, but refuse incompatible editor
  // values. Invalid command text remains editable; this only checks its shape.
  const fields = Object.entries(decoded).filter(([key]) => Object.hasOwn(STORED_FIELDS, key));
  if (!fields.every(([key, value]) => STORED_FIELDS[key](value))) return null;
  return Object.fromEntries(fields) as StoredDraft;
}

export function loadDraft(ref: DraftRef): CommandDraft | null {
  try {
    const raw = sessionStorage.getItem(draftKey(ref));
    if (!raw) return null;
    const stored = parseStoredDraft(raw);
    if (!stored) return null;
    // Old content-only snapshots omitted Active. Complete every bound field
    // before mounting: an undefined Checkbox binding throws in production.
    return {
      edit: ref.edit,
      originalName: ref.name,
      name: ref.name,
      aliases: [],
      response: '',
      perm: 'everyone',
      cooldown: 0,
      user_cooldown: 0,
      allowed_user_id: '',
      bump_counter: '',
      stream_online_only: false,
      is_active: true,
      ...stored
    };
  } catch {
    return null;
  }
}

export function saveDraft(ref: DraftRef, draft: CommandDraft | null): void {
  try {
    if (draft === null) sessionStorage.removeItem(draftKey(ref));
    else sessionStorage.setItem(draftKey(ref), JSON.stringify(draft));
  } catch {}
}

export function clearDraft(ref: DraftRef): void {
  saveDraft(ref, null);
}

export function hasDraft(ref: DraftRef): boolean {
  try {
    return sessionStorage.getItem(draftKey(ref)) !== null;
  } catch {
    return false;
  }
}
