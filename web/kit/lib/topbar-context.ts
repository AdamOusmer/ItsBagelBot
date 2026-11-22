// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { UiContextAction, UiTopbarContext } from '@bagel/ui/lib/nav-types';
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

function sharedStatus(t: Translate, target: SharedTarget): string {
  const sections = target.sectionLabels.join(', ');
  return `${t('banner.sharedPre')}${target.delegateLogin}${t('banner.sharedPost', { sections })}`;
}

function adminStatus(t: Translate, target: AdminTarget): string {
  return `${t('banner.viewingPre')}${target.login}${t('banner.viewingPost', { admin: target.impersonatorLogin })}`;
}

function ownerSubject(t: Translate, target: SharedTarget): Pick<UiTopbarContext, 'subjectPre' | 'subject' | 'subjectPost'> {
  const [subjectPre = '', subjectPost = ''] = t('banner.sharedOwner', { owner: '\0' }).split('\0');
  return { subjectPre, subject: target.delegateLogin, subjectPost };
}

function leaveShared(t: Translate): UiContextAction {
  return { label: t('banner.leaveShared'), compactLabel: t('banner.exit'), href: '/delegate/exit' };
}

function endView(t: Translate): UiContextAction {
  return { label: t('banner.endView'), compactLabel: t('banner.exit'), formAction: '/auth/logout' };
}

function buildSharedOnly(t: Translate, target: SharedTarget): UiTopbarContext {
  return {
    tone: 'warm',
    title: t('banner.sharedTitle'),
    statusText: sharedStatus(t, target),
    ...ownerSubject(t, target),
    scopesTitle: target.sectionLabels.join(', ') || undefined,
    actions: [leaveShared(t)],
  };
}

function buildAdminOnly(t: Translate, target: AdminTarget): UiTopbarContext {
  return {
    tone: 'danger',
    title: t('banner.adminTitle'),
    statusText: adminStatus(t, target),
    subjectPre: t('banner.viewingPre'),
    subject: target.login,
    subjectPost: t('banner.viewingPost', { admin: target.impersonatorLogin }),
    actions: [endView(t)],
  };
}

function buildStacked(t: Translate, target: StackedTarget): UiTopbarContext {
  return {
    tone: 'danger',
    title: t('banner.adminTitle'),
    statusText: `${sharedStatus(t, target)} · ${adminStatus(t, target)}`,
    ...ownerSubject(t, target),
    actions: [{ ...leaveShared(t), subtle: true }, endView(t)],
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
