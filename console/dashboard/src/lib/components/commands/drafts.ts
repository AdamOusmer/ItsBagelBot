// sessionStorage-backed command drafts: the editor mirrors work-in-progress
// here so a stray navigation or refresh can't eat it; the list shows an
// "unsaved" chip for rows with a lingering draft and restores it on reopen.
import type { Perm } from '@bagel/shared';

// The editor's working copy of one command (create + edit share the shape).
export interface CommandDraft {
  edit: boolean;
  name: string;
  originalName: string;
  aliases: string[];
  response: string;
  perm: Perm;
  cooldown: number;
  allowed_user_id: string;
  stream_online_only: boolean;
  is_active: boolean;
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
  } catch {
    /* best-effort */
  }
}

export function hasDraft(originalName: string): boolean {
  try {
    return sessionStorage.getItem(draftKey(originalName, true)) !== null;
  } catch {
    return false;
  }
}
