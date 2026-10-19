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
  flushDraft(ref);
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

const WRITE_DELAY_MS = 300;

const pending = new Map<string, { draft: CommandDraft; timer: ReturnType<typeof setTimeout> }>();

let flushHooked = false;

function dropPending(key: string): void {
  const entry = pending.get(key);
  if (!entry) return;
  clearTimeout(entry.timer);
  pending.delete(key);
}

function flushKey(key: string): void {
  const entry = pending.get(key);
  if (!entry) return;
  dropPending(key);
  try {
    sessionStorage.setItem(key, JSON.stringify(entry.draft));
  } catch {}
}

export function flushDraft(ref: DraftRef): void {
  flushKey(draftKey(ref));
}

export function flushDrafts(): void {
  for (const key of Array.from(pending.keys())) flushKey(key);
}

function flushWhenLeaving(): void {
  if (flushHooked || typeof window === 'undefined') return;
  flushHooked = true;
  window.addEventListener('pagehide', flushDrafts);
  document.addEventListener('visibilitychange', () => {
    if (document.hidden) flushDrafts();
  });
}

export function saveDraft(ref: DraftRef, draft: CommandDraft | null): void {
  const key = draftKey(ref);
  dropPending(key);
  try {
    if (draft === null) sessionStorage.removeItem(key);
    else sessionStorage.setItem(key, JSON.stringify(draft));
  } catch {}
}

// Keystrokes would otherwise serialize and write the whole draft each time; the
// latest state is written once typing pauses, on unmount, and on page hide.
export function saveDraftSoon(ref: DraftRef, draft: CommandDraft): void {
  flushWhenLeaving();
  const key = draftKey(ref);
  dropPending(key);
  pending.set(key, { draft, timer: setTimeout(() => flushKey(key), WRITE_DELAY_MS) });
}

export function clearDraft(ref: DraftRef): void {
  saveDraft(ref, null);
}

export function hasDraft(ref: DraftRef): boolean {
  flushDraft(ref);
  try {
    return sessionStorage.getItem(draftKey(ref)) !== null;
  } catch {
    return false;
  }
}

export function draftedNames(board: BoardId): Set<string> {
  flushDrafts();
  const prefix = `${PREFIX}${board}:`;
  const names = new Set<string>();
  try {
    for (let i = 0; i < sessionStorage.length; i++) {
      const key = sessionStorage.key(i);
      if (key?.startsWith(prefix)) names.add(key.slice(prefix.length));
    }
  } catch {}
  return names;
}

const CONTENT_FIELDS = [
  'edit',
  'originalName',
  'name',
  'response',
  'perm',
  'cooldown',
  'user_cooldown',
  'allowed_user_id',
  'bump_counter',
  'stream_online_only',
  'builtin'
] as const;

const sameList = (a: readonly string[], b: readonly string[]): boolean =>
  a.length === b.length && a.every((value, i) => value === b[i]);

export function draftsMatch(a: CommandDraft, b: CommandDraft, ignoreActive: boolean): boolean {
  if (!ignoreActive && !Object.is(a.is_active, b.is_active)) return false;
  return sameList(a.aliases, b.aliases) && CONTENT_FIELDS.every((field) => Object.is(a[field], b[field]));
}
