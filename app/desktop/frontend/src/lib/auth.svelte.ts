// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Frontend-only session flag: signOut() clears it so App.svelte falls back to
// Login. There is no device grant or keychain credential yet (BagelBridge
// pairing is unbuilt), so this does not call a server or revoke anything real;
// wire that here once device authorization exists.
export const authState = $state({ signedOut: false });

export function signOut(): void {
  authState.signedOut = true;
}
