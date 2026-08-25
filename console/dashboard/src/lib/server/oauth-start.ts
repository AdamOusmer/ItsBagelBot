// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Marketing "Add to Twitch" now hits GET /auth/login instead of the dashboard
// origin. A signed-in visitor must not be sent through Twitch: persistGrant
// would rotate the bot refresh token. Skipping whenever a session exists was
// tried first and is wrong — settings reconnect and /delegate/accept both
// arrive here with a live session and must re-run authorize. Those paths opt
// in: reconnect passes ?reauth=1; accept sets pending_delegation before the hop.
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
