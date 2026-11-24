// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { safeReturnPath } from '@bagel/kit/return-path';
import { bestEffort } from '@bagel/kit/server/best-effort';
import { currentAccount, needsTerms } from '#lib/server/consent';
import { freshAccountState, type AccountState } from '#lib/server/services';
import type { Session } from '#lib/server/session';

const SKIP_REASONS = new Set(['no_email', 'unavailable']);

export function returnTo(url: URL): string {
  const next = safeReturnPath(url.searchParams.get('next')) ?? '/';
  const path = next.split(/[?#]/, 1)[0]?.replace(/\/$/, '');
  return path === '/consent' ? '/' : next;
}

export function loginPath(next: string): string {
  return `/login?next=${encodeURIComponent(`/consent?next=${encodeURIComponent(next)}`)}`;
}

export function skippedReason(url: URL): 'no_email' | 'unavailable' | null {
  const skipped = url.searchParams.get('skipped') ?? '';
  return SKIP_REASONS.has(skipped) ? (skipped as 'no_email' | 'unavailable') : null;
}

export async function termsAccepted(locals: App.Locals, session: Session, checkFresh: boolean): Promise<boolean> {
  if (!needsTerms(currentAccount(locals))) return true;
  if (!checkFresh) return false;
  const account = await bestEffort<AccountState | null>(freshAccountState(session.user_id), null);
  return !needsTerms(account);
}
