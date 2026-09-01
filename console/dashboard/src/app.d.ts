// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { Session } from '$lib/server/session';
import type { Locale } from '@bagel/shared/i18n';
import type { AccountState } from '$lib/server/services';

declare global {
  namespace App {
    interface Locals {
      session: Session | null;
      locale: Locale;
      cursorEnabled: boolean;
      /**
       * The account-state read guardSession already made for the request's
       * gates, so the (app) layout reuses it instead of paying a second RPC.
       * Settled result, never a live rejected promise: `{ ghost: true }` means
       * the users service authoritatively reported no such user; an unset
       * field means the read blipped and the caller may retry.
       */
      accountState?: { value: AccountState } | { ghost: true };
    }
    interface PageData {
      role?: 'streamer' | 'mod';
      displayName?: string;
      locale?: Locale;
      cursorEnabled?: boolean;
      // Import wizard: the Nightbot OAuth connect flow has parked an unexpired
      // access-token cookie for this browser (settings/import load).
      nightbotConnected?: boolean;
    }
  }
}

export {};
