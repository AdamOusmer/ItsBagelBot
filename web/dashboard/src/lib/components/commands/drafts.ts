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
    return raw ? (JSON.parse(raw) as CommandDraft) : null;
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
