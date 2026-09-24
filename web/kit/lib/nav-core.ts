// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { IconName } from '@bagel/ui/lib/icons';
import type { NavChild, NavGroupDef, NavLink } from './types';
import type { MessageKey } from './i18n/keys';

export const identity =
  (key: MessageKey): string =>
  key;

export interface SectionDef {
  id: string;
  labelKey: MessageKey;
  icon: IconName;
  href: string;
  match: readonly string[];
  ownerOnly?: boolean;
  grant?: string;
  minRole?: 'moderator' | 'admin' | 'owner';
}

export function resolveSection<D extends SectionDef>(
  sections: readonly D[],
  path: string,
  fallback: D['id']
): D['id'] {
  let best = fallback;
  let bestLen = 0;
  for (const def of sections) {
    for (const prefix of def.match) {
      const hit = prefix === '/' ? path === '/' : path.startsWith(prefix);
      if (hit && prefix.length > bestLen) {
        best = def.id;
        bestLen = prefix.length;
      }
    }
  }
  return best;
}

export function navItems<D extends SectionDef>(opts: {
  sections: readonly D[];
  visible: (def: D) => boolean;
  active: (def: D) => boolean;
  children?: (def: D) => NavChild[] | undefined;
  t?: (key: MessageKey) => string;
}): NavLink[] {
  const t = opts.t ?? identity;
  return opts.sections.filter(opts.visible).map((def) => {
    const children = opts.children?.(def);
    return {
      href: def.href,
      icon: def.icon,
      label: t(def.labelKey),
      active: opts.active(def),
      ...(children ? { children } : {})
    };
  });
}

export function navGroups(label: string, items: readonly NavLink[]): NavGroupDef[] {
  return [{ label, items: [...items] }];
}
