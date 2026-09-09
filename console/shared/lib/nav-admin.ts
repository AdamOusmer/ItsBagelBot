// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// The admin console's nav registry: one declarative list owning every (admin)
// section's path, icon, label key, match prefixes and least role.
//
// It replaces four hand-maintained literals in (admin)/+layout.svelte -- a
// CRUMBS prefix table, a rail `groups` array, a `mobileItems` array and an
// `isManager` visibility branch repeated across all three. Those had already
// drifted: /counters was offered to any manager while its route demands owner
// (ROLE_FOR['counters.manage']), so an admin clicking it was bounced to '/'.
// Roles are declared here and compared through the shared ladder, so the nav
// and the route gate cannot disagree again.
//
// Imports nav-core, NOT nav.ts: nav.ts drags MODULE_CATALOG into whatever
// imports it (16 KB gzip of dashboard data the admin shell has no use for; see
// nav-core.ts's header for the measurement).

import { navGroups, navItems, resolveSection, identity, type SectionDef } from './nav-core';
import { staffAtLeast, type StaffRole } from './staff-role';
import type { NavGroupDef, NavLink } from './types';
import type { MessageKey } from './i18n/keys';

export type AdminSectionId =
  | 'overview'
  | 'shards'
  | 'lanes'
  | 'events'
  | 'users'
  | 'notifications'
  | 'staff'
  | 'audit'
  | 'secrets'
  | 'counters';

/** Rail/dock grouping. Order below is display order. */
export type AdminGroupId = 'operate' | 'accounts' | 'access';

export interface AdminSectionDef extends SectionDef {
  id: AdminSectionId;
  group: AdminGroupId;
  minRole?: StaffRole;
}

export const ADMIN_GROUP_ORDER: readonly AdminGroupId[] = ['operate', 'accounts', 'access'];

export const ADMIN_GROUP_LABEL: Record<AdminGroupId, MessageKey> = {
  operate: 'adminNav.operate',
  accounts: 'adminNav.accounts',
  access: 'adminNav.access'
};

/**
 * Registry order IS display order within a group, and doubles as the breadcrumb
 * tiebreak. A new admin page joins here, nowhere else.
 *
 * `/analytics` is deliberately absent: its enrollment series folds into the
 * Overview, and the route survives only as a 301 for bookmarks.
 */
export const ADMIN_SECTIONS: readonly AdminSectionDef[] = [
  { id: 'overview', group: 'operate', labelKey: 'adminNav.overview', icon: 'overview', href: '/', match: ['/'] },
  { id: 'shards', group: 'operate', labelKey: 'adminNav.shards', icon: 'server', href: '/shards', match: ['/shards'] },
  { id: 'lanes', group: 'operate', labelKey: 'adminNav.lanes', icon: 'lanes', href: '/lanes', match: ['/lanes'] },
  { id: 'events', group: 'operate', labelKey: 'adminNav.events', icon: 'pulse', href: '/events', match: ['/events'] },
  { id: 'users', group: 'accounts', labelKey: 'adminNav.users', icon: 'users', href: '/users', match: ['/users'] },
  {
    id: 'notifications',
    group: 'accounts',
    labelKey: 'adminNav.notifications',
    icon: 'bell',
    href: '/notifications',
    match: ['/notifications']
  },
  // Access: every row here is gated server-side too (access.ts ROLE_FOR); the
  // minRole is what keeps the link from being offered in the first place.
  { id: 'staff', group: 'access', labelKey: 'adminNav.staff', icon: 'moderation', href: '/staff', match: ['/staff'], minRole: 'admin' },
  { id: 'audit', group: 'access', labelKey: 'adminNav.audit', icon: 'audit', href: '/audit', match: ['/audit'], minRole: 'admin' },
  { id: 'secrets', group: 'access', labelKey: 'adminNav.secrets', icon: 'lock', href: '/secrets', match: ['/secrets'], minRole: 'owner' },
  { id: 'counters', group: 'access', labelKey: 'adminNav.counters', icon: 'list', href: '/counters', match: ['/counters'], minRole: 'owner' }
];

/** The admin registry's resolution, defaulting to its landing section. */
export function adminSectionForPath(path: string): AdminSectionId {
  return resolveSection(ADMIN_SECTIONS, path, 'overview');
}

/** Whether `role` may be offered `def` at all. Unset minRole means any staff. */
export function adminSectionVisible(def: AdminSectionDef, role: StaffRole): boolean {
  return !def.minRole || staffAtLeast(role, def.minRole);
}

/** The label key for a section id, for the breadcrumb. */
export function adminSectionLabelKey(id: AdminSectionId): MessageKey {
  return ADMIN_SECTIONS.find((def) => def.id === id)?.labelKey ?? 'adminNav.overview';
}

/**
 * Every nav link this role may see, in registry order. Used for the rail, the
 * dock and (through adminNavGroups) the grouped mobile popovers.
 */
export function adminNavItems(opts: {
  role: StaffRole;
  section: AdminSectionId;
  t?: (key: MessageKey) => string;
}): NavLink[] {
  return navItems({
    sections: ADMIN_SECTIONS,
    visible: (def) => adminSectionVisible(def, opts.role),
    active: (def) => def.id === opts.section,
    t: opts.t
  });
}

/**
 * The rail's groups, in ADMIN_GROUP_ORDER, with empty ones dropped (a moderator
 * has no Access group at all). Rebuilt from the registry rather than from the
 * flat item list so a link and its group can never be separated: the flat list
 * carries no group, so it is regenerated per group here.
 */
export function adminNavGroups(opts: {
  role: StaffRole;
  section: AdminSectionId;
  t?: (key: MessageKey) => string;
}): NavGroupDef[] {
  const label = opts.t ?? identity;
  const groups: NavGroupDef[] = [];
  for (const id of ADMIN_GROUP_ORDER) {
    const items = navItems({
      sections: ADMIN_SECTIONS.filter((def) => def.group === id),
      visible: (def) => adminSectionVisible(def, opts.role),
      active: (def) => def.id === opts.section,
      t: opts.t
    });
    if (items.length) groups.push(...navGroups(label(ADMIN_GROUP_LABEL[id]), items));
  }
  return groups;
}
