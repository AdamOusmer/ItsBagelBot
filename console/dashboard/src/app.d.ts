import type { Session } from '$lib/server/session';
import type { Locale } from '@bagel/shared/i18n';

declare global {
  namespace App {
    interface Locals {
      session: Session | null;
      locale: Locale;
      cursorEnabled: boolean;
    }
    interface PageData {
      role?: 'streamer' | 'mod';
      displayName?: string;
      locale?: Locale;
      cursorEnabled?: boolean;
    }
  }
}

export {};
