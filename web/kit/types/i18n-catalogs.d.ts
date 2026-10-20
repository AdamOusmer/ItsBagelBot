// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

declare module 'virtual:bagel-i18n' {
  export const locales: string[];
  export const localeNames: Record<string, string>;
  export const urls: Record<string, string>;
  export const sliced: boolean;
  export const loaders: Record<string, () => Promise<{ default: import('../lib/i18n/types').MessageTree }>>;
}
