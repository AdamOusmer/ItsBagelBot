// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { IconName } from './icons';

export interface UiNavLink {
  href?: string;
  label: string;
  icon?: IconName;
  current?: boolean;
  locked?: boolean;
  lockedHint?: string;
  count?: string | number;
  external?: boolean;
  children?: UiNavLink[];
}

export interface UiNavGroup {
  label?: string;
  items: UiNavLink[];
}

export interface UiBrand {
  title: string;
  sub?: string;
  href?: string;
  logoSrc?: string;
  logoAlt?: string;
  logoShape?: 'square' | 'circle';
}

export interface UiLocaleOption {
  code: string;
  href: string;
  label: string;
  title?: string;
  flag?: string;
  current?: boolean;
}

export interface UiCrumb {
  label: string;
  href?: string;
}

export interface UiFooterColumn {
  title: string;
  links: UiNavLink[];
}

export interface ProfileMenuLink {
  href: string;
  label: string;
  hint?: string;
  icon?: IconName;
  external?: boolean;
}

export interface ProfileAvatar {
  name: string;
  size: number;
  active: boolean;
  item: boolean;
}

// Values an app hands the rail's own account menu. The rail renders the
// trigger, dropdown and avatar; the app supplies no markup.
export interface UiAccount {
  name: string;
  caption: string;
  links?: ProfileMenuLink[];
  linksLabel?: string;
  exit?: ProfileMenuLink;
  help?: ProfileMenuLink[];
  helpLabel?: string;
  helpTitle?: string;
  feedback?: ProfileMenuLink;
  logoutLabel: string;
  logoutAction?: string;
  onLogout?: (event: SubmitEvent) => void;
  menuLabel?: string;
}
