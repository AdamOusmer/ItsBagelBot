// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { navItems, resolveSection, identity, type SectionDef } from './nav-core';
import { staffAtLeast, type StaffRole } from './staff-role';
import type { NavChild, NavGroupDef, NavLink } from './types';
import type { IconName } from '@bagel/ui/lib/icons';
import type { MessageKey } from './i18n/keys';

export type AdminSectionId =
  | 'overview'
  | 'analytics'
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
  | 'counters'
  | 'settings';

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
  { id: 'analytics', group: 'operate', labelKey: 'adminNav.analytics', icon: 'activity', href: '/analytics', match: ['/analytics'], minRole: 'admin' },
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
  { id: 'settings', group: 'accounts', labelKey: 'adminNav.settings', icon: 'settings', href: '/settings', match: ['/settings'] },
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

type AdminNavOptions = {
  role: StaffRole;
  section: AdminSectionId;
  t?: (key: MessageKey) => string;
  liveCount?: number | null;
};

function adminLinks(opts: AdminNavOptions, sections: readonly AdminSectionDef[]): NavLink[] {
  const links = navItems({
    sections,
    visible: (def) => adminSectionVisible(def, opts.role),
    current: (def) => def.id === opts.section,
    t: opts.t
  });
  const users = links.find((link) => link.href === '/users');
  if (users && opts.liveCount !== undefined) {
    users.count = String(opts.liveCount ?? '');
  }
  return links;
}

export function adminNavItems(opts: AdminNavOptions): NavLink[] {
  return adminLinks(opts, ADMIN_SECTIONS);
}

const STANDALONE: ReadonlySet<AdminSectionId> = new Set(['overview', 'settings']);

const ADMIN_GROUP_ICON: Record<AdminGroupId, IconName> = {
  operate: 'server',
  accounts: 'users',
  access: 'moderation'
};

function branchSections(id: AdminGroupId): AdminSectionDef[] {
  return ADMIN_SECTIONS.filter((def) => def.group === id && !STANDALONE.has(def.id));
}

function asChild({ href, label, count }: NavLink): NavChild {
  return count === undefined ? { href, label } : { href, label, count };
}

function adminBranch(opts: AdminNavOptions, id: AdminGroupId): NavLink[] {
  const links = adminLinks(opts, branchSections(id));
  if (!links.length) return [];
  const label = opts.t ?? identity;
  return [
    {
      href: links[0].href,
      icon: ADMIN_GROUP_ICON[id],
      label: label(ADMIN_GROUP_LABEL[id]),
      current: links.some((link) => link.current),
      children: links.map(asChild)
    }
  ];
}

function standaloneLinks(opts: AdminNavOptions, id: AdminSectionId): NavLink[] {
  return adminLinks(opts, ADMIN_SECTIONS.filter((def) => def.id === id));
}

export function adminNavGroups(opts: AdminNavOptions): NavGroupDef[] {
  const branches = ADMIN_GROUP_ORDER.flatMap((id) => adminBranch(opts, id));
  return [{ items: [...standaloneLinks(opts, 'overview'), ...branches, ...standaloneLinks(opts, 'settings')] }];
}
