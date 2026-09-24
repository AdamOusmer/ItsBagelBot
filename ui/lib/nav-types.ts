// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { IconName } from './icons';

export interface UiNavLink {
  href?: string;
  label: string;
  icon?: IconName;
  active?: boolean;
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
  premium?: boolean;
}

export interface UiLocaleOption {
  code: string;
  href: string;
  label: string;
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
