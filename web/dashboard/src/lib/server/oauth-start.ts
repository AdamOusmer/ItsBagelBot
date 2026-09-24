// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Signed-in visitors skip Twitch: persistGrant would rotate the bot refresh token.
export function skipAuthorizeIfSignedIn(opts: {
  hasSession: boolean;
  pendingDelegation: string | undefined;
  reauth: string | null;
}): boolean {
  if (!opts.hasSession) return false;
  if (opts.pendingDelegation) return false;
  if (opts.reauth === '1') return false;
  return true;
}
