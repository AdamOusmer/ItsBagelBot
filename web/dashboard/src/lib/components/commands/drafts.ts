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
  allowed_user_id: string;
  bump_counter: string;
  stream_online_only: boolean;
  is_active: boolean;
  builtin?: boolean;
}

const PREFIX = 'bb-cmd-draft:';

export function draftKey(originalName: string, edit: boolean): string {
  return `${PREFIX}${edit ? originalName : 'new'}`;
}

export function loadDraft(originalName: string, edit: boolean): CommandDraft | null {
  try {
    const raw = sessionStorage.getItem(draftKey(originalName, edit));
    if (!raw) return null;
    const stored: unknown = JSON.parse(raw);
    if (!stored || typeof stored !== 'object' || Array.isArray(stored)) return null;
    const d = stored as Record<string, unknown>;
    // Storage is untrusted, and older content-only snapshots omitted Active.
    // Every bound editor field must have a value before the component mounts:
    // binding undefined to Checkbox's fallback throws in production too.
    for (const field of ['name', 'response', 'perm', 'allowed_user_id', 'bump_counter']) {
      if (d[field] !== undefined && typeof d[field] !== 'string') return null;
    }
    if (d.aliases !== undefined && (!Array.isArray(d.aliases) || d.aliases.some((a) => typeof a !== 'string'))) return null;
    if (d.cooldown !== undefined && (typeof d.cooldown !== 'number' || !Number.isFinite(d.cooldown))) return null;
    for (const field of ['is_active', 'stream_online_only']) {
      if (d[field] !== undefined && typeof d[field] !== 'boolean') return null;
    }
    return {
      edit,
      originalName,
      name: (d.name as string | undefined) ?? originalName,
      aliases: (d.aliases as string[] | undefined) ?? [],
      response: (d.response as string | undefined) ?? '',
      perm: (d.perm as Perm | undefined) ?? 'everyone',
      cooldown: (d.cooldown as number | undefined) ?? 0,
      allowed_user_id: (d.allowed_user_id as string | undefined) ?? '',
      bump_counter: (d.bump_counter as string | undefined) ?? '',
      stream_online_only: (d.stream_online_only as boolean | undefined) ?? false,
      is_active: (d.is_active as boolean | undefined) ?? true
    };
  } catch {
    return null;
  }
}

export function clearDraft(originalName: string, edit: boolean): void {
  try {
    sessionStorage.removeItem(draftKey(originalName, edit));
  } catch {}
}

export function hasDraft(originalName: string): boolean {
  try {
    return sessionStorage.getItem(draftKey(originalName, true)) !== null;
  } catch {
    return false;
  }
}
