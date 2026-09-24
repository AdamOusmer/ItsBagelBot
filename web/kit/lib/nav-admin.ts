// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { navGroups, navItems, resolveSection, identity, type SectionDef } from './nav-core';
import { staffAtLeast, type StaffRole } from './staff-role';
import type { NavGroupDef, NavLink } from './types';
import type { MessageKey } from './i18n/keys';

export type AdminSectionId =
  | 'overview'
  | 'shards'
  | 'trials'
  | 'lanes'
  | 'events'
  | 'deploys'
  | 'users'
  | 'notifications'
  | 'giveaways'
  | 'staff'
  | 'audit'
  | 'secrets'
  | 'counters';

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

export const ADMIN_SECTIONS: readonly AdminSectionDef[] = [
  { id: 'overview', group: 'operate', labelKey: 'adminNav.overview', icon: 'overview', href: '/', match: ['/'] },
  { id: 'shards', group: 'operate', labelKey: 'adminNav.shards', icon: 'server', href: '/shards', match: ['/shards'] },
  { id: 'trials', group: 'operate', labelKey: 'adminNav.trials', icon: 'pulse', href: '/trials', match: ['/trials'], minRole: 'admin' },
  { id: 'lanes', group: 'operate', labelKey: 'adminNav.lanes', icon: 'lanes', href: '/lanes', match: ['/lanes'] },
  { id: 'events', group: 'operate', labelKey: 'adminNav.events', icon: 'pulse', href: '/events', match: ['/events'] },
  { id: 'deploys', group: 'operate', labelKey: 'adminNav.deploys', icon: 'github', href: '/deploys', match: ['/deploys'], minRole: 'owner' },
  { id: 'users', group: 'accounts', labelKey: 'adminNav.users', icon: 'users', href: '/users', match: ['/users'] },
  {
    id: 'notifications',
    group: 'accounts',
    labelKey: 'adminNav.notifications',
    icon: 'bell',
    href: '/notifications',
    match: ['/notifications']
  },
  {
    id: 'giveaways',
    group: 'accounts',
    labelKey: 'adminNav.giveaways',
    icon: 'activity',
    href: '/giveaways',
    match: ['/giveaways'],
    minRole: 'admin'
  },
  { id: 'staff', group: 'access', labelKey: 'adminNav.staff', icon: 'moderation', href: '/staff', match: ['/staff'], minRole: 'admin' },
  { id: 'audit', group: 'access', labelKey: 'adminNav.audit', icon: 'audit', href: '/audit', match: ['/audit'], minRole: 'admin' },
  { id: 'secrets', group: 'access', labelKey: 'adminNav.secrets', icon: 'lock', href: '/secrets', match: ['/secrets'], minRole: 'owner' },
  { id: 'counters', group: 'access', labelKey: 'adminNav.counters', icon: 'list', href: '/counters', match: ['/counters'], minRole: 'owner' }
];

export function adminSectionForPath(path: string): AdminSectionId {
  return resolveSection(ADMIN_SECTIONS, path, 'overview');
}

export function adminSectionVisible(def: AdminSectionDef, role: StaffRole): boolean {
  return !def.minRole || staffAtLeast(role, def.minRole);
}

export function adminSectionLabelKey(id: AdminSectionId): MessageKey {
  return ADMIN_SECTIONS.find((def) => def.id === id)?.labelKey ?? 'adminNav.overview';
}

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
