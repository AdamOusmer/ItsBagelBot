// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// The shapes every navigation surface in this library takes as props. Types
// only: this module compiles to nothing, so importing it costs a consumer no
// bytes and no runtime.
//
// They are written to be STRUCTURALLY compatible with the console's own nav
// registry types (`NavLink`, `NavGroupDef`, `DashboardLink` in
// web/kit/lib/types.ts), so `nav.ts`, `nav-core.ts` and `nav-admin.ts` stay in
// kit as data and feed these components unchanged. That compatibility is the
// whole reason this file declares its own types rather than the components
// taking the console's: a design library that imports the bot's types is not
// extractable, and a library that demands its own DTO makes every caller write
// a mapper.
//
// What is deliberately NOT here: anything the bot names. No route constants,
// no icon-name union beyond the generated set, and above all no copy --
// `lockedHint` is a caller-supplied string precisely because the sentence it
// used to hold ("Broadcaster only") is a product decision, resolved from the
// console's i18n catalog by the kit wrapper.

import type { IconName } from './icons';

export interface UiNavLink {
  /** Target. Omitted for a locked entry, which renders as a non-link. */
  href?: string;
  label: string;
  icon?: IconName;
  /** Current route. Renders aria-current="page", which the CSS keys on. */
  active?: boolean;
  /** Visible but not reachable. */
  locked?: boolean;
  /** sr-only sentence explaining a locked entry. Caller's wording. */
  lockedHint?: string;
  /** A badge value: unread count, item count. Rendered verbatim. */
  count?: string | number;
  /** Opens in a new tab, with the rel that makes that safe. */
  external?: boolean;
  /** Sub-entries. A rail row with children collapses; a dock group folds. */
  children?: UiNavLink[];
}

export interface UiNavGroup {
  /** Section heading. Omitted for an unlabelled group. */
  label?: string;
  items: UiNavLink[];
}

export interface UiBrand {
  title: string;
  sub?: string;
  href?: string;
  /** Pre-resolved URL; see ui/styles/elements/brand-mark.css. */
  logoSrc?: string;
  logoAlt?: string;
  premium?: boolean;
}

export interface UiLocaleOption {
  /** BCP-47 code. Rendered as the label when `label` is omitted. */
  code: string;
  href: string;
  label: string;
  current?: boolean;
}

export interface UiCrumb {
  label: string;
  /** A crumb without an href is the current page. */
  href?: string;
}

export interface UiFooterColumn {
  title: string;
  links: UiNavLink[];
}
