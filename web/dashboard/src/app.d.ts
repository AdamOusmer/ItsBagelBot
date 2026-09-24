// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { Session } from '$lib/server/session';
import type { Locale } from '@bagel/kit/i18n';
import type { AccountState } from '$lib/server/services';

declare global {
  namespace App {
    interface Locals {
      session: Session | null;
      locale: Locale;
      cursorEnabled: boolean;
      accountState?: { value: AccountState } | { ghost: true };
      edgeCache404?: boolean;
    }
    interface PageData {
      role?: 'streamer' | 'mod';
      displayName?: string;
      locale?: Locale;
      cursorEnabled?: boolean;
      connected?: Record<string, boolean>;
    }
    interface PageState {
      importing?: boolean;
    }
  }
}

export {};
