import type { Session } from '$lib/server/session';

declare global {
  namespace App {
    interface Locals {
      session: Session | null;
    }
    interface PageData {
      role?: 'streamer' | 'mod';
      displayName?: string;
    }
  }
}

export {};
