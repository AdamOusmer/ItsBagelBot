// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { UiTopbarContext } from '@bagel/ui/lib/nav-types';
import type { I18n } from './i18n/context';

type Translate = I18n['t'];

export interface ConsoleContextInput {
  delegateLogin?: string;
  sectionLabels?: string[];
  login?: string;
  impersonatorLogin?: string;
}

export interface ConsoleRoleInput {
  isDelegate: boolean;
  impersonatorLogin?: string;
}

interface SharedTarget {
  delegateLogin: string;
  sectionLabels: string[];
}

interface AdminTarget {
  login: string;
  impersonatorLogin: string;
}

interface StackedTarget extends SharedTarget, AdminTarget {}

function initialFor(target: { delegateLogin: string }): string {
  const trimmed = target.delegateLogin.trim();
  return trimmed ? trimmed.charAt(0).toUpperCase() : '';
}

function sharedStatus(t: Translate, target: SharedTarget): string {
  const sections = target.sectionLabels.join(', ');
  return `${t('banner.sharedPre')}${target.delegateLogin}${t('banner.sharedPost', { sections })}`;
}

function adminStatus(t: Translate, target: AdminTarget): string {
  return `${t('banner.viewingPre')}${target.login}${t('banner.viewingPost', { admin: target.impersonatorLogin })}`;
}

function ownerAffixes(t: Translate): { subjectPre: string; subjectPost: string } {
  const [subjectPre = '', subjectPost = ''] = t('banner.sharedOwner', { owner: '\0' }).split('\0');
  return { subjectPre, subjectPost };
}

function buildSharedOnly(t: Translate, target: SharedTarget): UiTopbarContext {
  const joined = target.sectionLabels.join(', ');
  const { subjectPre, subjectPost } = ownerAffixes(t);
  return {
    tone: 'warm',
    statusText: sharedStatus(t, target),
    badges: [{ label: t('banner.sharedBadge'), icon: 'users', tone: 'warm' }],
    avatar: initialFor(target),
    subjectPre,
    subject: target.delegateLogin,
    subjectPost,
    scopes: target.sectionLabels,
    scopesTitle: joined || undefined,
    actions: [
      {
        label: t('banner.leaveShared'),
        compactLabel: t('banner.exit'),
        href: '/delegate/exit',
        tone: 'warm',
      },
    ],
  };
}

function buildAdminOnly(t: Translate, target: AdminTarget): UiTopbarContext {
  return {
    tone: 'danger',
    statusText: adminStatus(t, target),
    badges: [{ label: t('banner.adminBadge'), icon: 'moderation', tone: 'danger' }],
    subjectPre: t('banner.viewingPre'),
    subject: target.login,
    subjectPost: t('banner.viewingPost', { admin: target.impersonatorLogin }),
    actions: [
      {
        label: t('banner.endView'),
        compactLabel: t('banner.exit'),
        formAction: '/auth/logout',
        tone: 'danger',
      },
    ],
  };
}

function buildStacked(t: Translate, target: StackedTarget): UiTopbarContext {
  return {
    tone: 'danger',
    statusText: `${sharedStatus(t, target)} · ${adminStatus(t, target)}`,
    badges: [
      { label: target.delegateLogin, icon: 'users', tone: 'warm' },
      { label: t('banner.adminTag', { admin: target.impersonatorLogin }), icon: 'moderation', tone: 'danger' },
    ],
    actions: [
      {
        label: t('banner.leaveShared'),
        compactLabel: t('banner.exit'),
        href: '/delegate/exit',
        tone: 'warm',
        subtle: true,
      },
      {
        label: t('banner.endView'),
        compactLabel: t('banner.exit'),
        formAction: '/auth/logout',
        tone: 'danger',
      },
    ],
  };
}

export function buildConsoleContext(t: Translate, input: ConsoleContextInput): UiTopbarContext | undefined {
  const { delegateLogin, impersonatorLogin } = input;
  const sectionLabels = input.sectionLabels ?? [];
  const login = input.login ?? '';
  if (delegateLogin && impersonatorLogin) {
    return buildStacked(t, { delegateLogin, sectionLabels, login, impersonatorLogin });
  }
  if (delegateLogin) {
    return buildSharedOnly(t, { delegateLogin, sectionLabels });
  }
  if (impersonatorLogin) {
    return buildAdminOnly(t, { login, impersonatorLogin });
  }
  return undefined;
}

export function consoleAccountRole(t: Translate, input: ConsoleRoleInput): string {
  if (input.impersonatorLogin) return t('topbar.roleAdminView', { admin: input.impersonatorLogin });
  if (input.isDelegate) return t('topbar.roleSignedIn');
  return t('topbar.roleBroadcaster');
}
