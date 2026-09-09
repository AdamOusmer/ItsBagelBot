// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { Session } from '$lib/server/session';
import type { Locale } from '@bagel/shared/i18n';

declare global {
  namespace App {
    interface Locals {
      session: Session | null;
      locale: Locale;
    }
    interface PageData {
      displayName?: string;
      login?: string;
      role?: 'moderator' | 'admin' | 'owner';
      locale?: Locale;
    }
  }
}

export {};
