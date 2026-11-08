// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { Call } from '@wailsio/runtime';

type Status = { configured: boolean; signedIn: boolean; paired: boolean; pending: boolean; error: string };
export const authState = $state({ configured: false, signedIn: false, paired: false, pending: false, working: false, error: '', signedOut: false });

async function refresh(): Promise<void> {
  try {
    const status = await Call.ByName('main.AuthService.Status') as Status;
    Object.assign(authState, status);
  } catch { authState.error = 'Native authorization is unavailable.'; }
}

export function observeAuth(): () => void {
  void refresh();
  const timer = setInterval(() => { void refresh(); }, 1000);
  return () => clearInterval(timer);
}

export async function startPairing(): Promise<void> {
  authState.error = '';
  authState.working = true;
  try {
    await Call.ByName('main.AuthService.StartPairing');
    authState.signedOut = false;
    await refresh();
  } catch { authState.error = 'Could not start device approval. Try again.'; }
  finally { authState.working = false; }
}

export async function retryAuthorization(): Promise<void> {
  authState.working = true;
  try {
    await Call.ByName('main.AuthService.Retry');
    await refresh();
  } catch { authState.error = 'Authorization is unavailable. Try again.'; }
  finally { authState.working = false; }
}

export async function signOut(): Promise<void> {
  try {
    await Call.ByName('main.AuthService.SignOut');
    authState.signedOut = true;
    await refresh();
  } catch { authState.error = 'Could not clear the device credentials. Try again.'; }
}
