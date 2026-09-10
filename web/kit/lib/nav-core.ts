// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// The console-agnostic half of the nav registry: the section shape and the
// three functions that turn a registry into breadcrumbs, links and groups.
//
// It is a separate module from nav.ts for a measured reason. nav.ts statically
// imports MODULE_CATALOG (types.ts) and MODULE_CATEGORY_ORDER (module-index.ts)
// for the dashboard's Modules fan-out, and those are values, not types: any
// importer of '@bagel/kit/nav' pays for the whole dashboard module catalog.
// Measured 2026-09-09 on macOS/arm64: importing only resolveSection/navItems/
// navGroups from nav.ts is 16253 B gzip; the same three from here is ~0. The
// admin console's shell needs exactly those three and none of the catalog, so
// it imports '@bagel/kit/nav-core' (via nav-admin.ts) instead.
//
// Everything here is pure data / pure functions -- no import.meta.glob, no
// Vite-only entry points -- so it stays safe in a boot import graph.

import type { IconName } from './icons';
import type { NavChild, NavGroupDef, NavLink } from './types';
import type { MessageKey } from './i18n/keys';

// The label translator defaults to identity rather than pulling in
// i18n/messages: that module loads locales via Vite's import.meta.glob, which
// has no meaning outside a Vite build (bun test throws), and this module sits
// in the boot import graph. Each layout injects its real `t`.
export const identity =
  (key: MessageKey): string =>
  key;

/**
 * The shape a section registry has, whichever console owns it.
 *
 * The dashboard's registry (DASHBOARD_SECTIONS) was the first; the admin
 * console has the same ladder problem with a different access rule, so the
 * resolution functions underneath take a registry rather than closing over one.
 * Access is declared, never computed here: `ownerOnly`/`grant` are the
 * dashboard's delegate model and `minRole` the admin console's staff ladder,
 * and each app passes the predicate that reads its own fields.
 */
export interface SectionDef {
  id: string;
  labelKey: MessageKey;
  icon: IconName;
  href: string;
  /** Path prefixes that resolve to this section ('/' is exact-match only). */
  match: readonly string[];
  /** Hidden from delegates. */
  ownerOnly?: boolean;
  /** Visible to a delegate only when granted this section. */
  grant?: string;
  /** Lowest staff role that may see this section (admin console). */
  minRole?: 'moderator' | 'admin' | 'owner';
}

/**
 * Longest-prefix resolution over a registry's match lists ('/' exact-match
 * only), falling back to `fallback`. Prefixes are disjoint today, so length and
 * declaration order can never disagree.
 */
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

/**
 * Nav links for a registry: the caller supplies which entries this viewer may
 * see, which one is current, and any nested children, because those three are
 * the only parts that differ between the consoles' access models. The mapping
 * from a section to a link -- and the injected translator defaulting to
 * identity so pure callers need no i18n context -- is the same everywhere.
 */
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

/** The single sidebar/mobile group wrapping a set of nav items. */
export function navGroups(label: string, items: readonly NavLink[]): NavGroupDef[] {
  return [{ label, items: [...items] }];
}
