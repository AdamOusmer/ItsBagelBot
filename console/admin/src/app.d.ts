import type { Session } from '$lib/server/session';

declare global {
  namespace App {
    interface Locals {
      session: Session | null;
    }
    interface PageData {
      displayName?: string;
      login?: string;
      role?: 'moderator' | 'admin' | 'owner';
    }
  }
}

export {};
