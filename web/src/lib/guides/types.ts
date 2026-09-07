// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

/**
 * The guides content model. Everything a broadcaster reads lives in a content
 * file under src/content/guides/<slug>.<lang>.ts shaped by these types; the
 * components under src/components/guides render them and hold no copy of their
 * own. Type-only import of Lang so a content file never pulls the i18n runtime.
 */
import type { Lang } from '../../i18n/ui';

export type { Lang };

/** One line in a ChatMock vignette. Mirrors ChatMock.astro's own props. */
export interface ChatLine {
  who: 'viewer' | 'mod' | 'bot' | 'system';
  name?: string;
  text: string;
  time?: string;
}

/** A numbered annotation under a dashboard mock, paired with a df-mark anchor in the screen; the legend line lights that element on hover. */
export interface Note {
  n: number;
  text: string;
}

/**
 * Dashboard mock screens. One component per distinct mock UI under
 * src/components/guides/screens; add a name here and a file there together.
 */
export type ScreenName =
  | 'DashboardHome'
  | 'CommandsList'
  | 'CommandEditor'
  | 'ModulesGrid'
  | 'ModuleConfigure'
  | 'NewCounter'
  | 'RewardCounter'
  | 'DataSourceModal'
  | 'DataSourcePicker';

/** Interactive widgets. Resolved through src/components/guides/widgets/index.ts. */
export type WidgetName =
  | 'Checklist'
  | 'PathPicker'
  | 'FetchBudget'
  | 'FetchOutcomes'
  | 'ModuleCatalog'
  | 'Rehearsal'
  | 'CounterPlay';

/**
 * One piece of a section. `html` fields are authored, trusted HTML: they are
 * written by us in the content files and rendered with set:html, never built
 * from anything a visitor types.
 */
export type Block =
  | { kind: 'prose'; html: string }
  | { kind: 'callout'; tone: 'tip' | 'warn' | 'note'; html: string }
  | { kind: 'table'; head: string[]; rows: string[][]; caption?: string; id?: string }
  | { kind: 'chat'; title?: string; caption?: string; lines: ChatLine[] }
  | { kind: 'dash'; screen: ScreenName; path?: string; caption?: string; notes?: Note[]; labels?: Record<string, string> }
  | { kind: 'steps'; items: { title: string; html: string }[] }
  | { kind: 'cards'; columns?: 2 | 3; items: { title: string; html: string; chips?: string[]; badge?: string }[] }
  | { kind: 'widget'; name: WidgetName; labels?: Record<string, string>; props?: Record<string, unknown> };

/** A numbered chapter of a guide: the TOC entry, the glance note, the body. */
export interface Section {
  id: string;
  heading: string;
  note?: string;
  blocks: Block[];
}

/**
 * Everything outside the body: the <head>, the hero, and the hub card.
 * title/description are the <head> pair; heading/lead are the PageHero pair
 * (they say different things, so they are separate fields, not one reused).
 */
export interface GuideMeta {
  title: string;
  description: string;
  eyebrow: string;
  heading: string;
  lead: string;
  minutes: string;
  card: { title: string; description: string; meta: string; chips: string[] };
}

export interface GuideContent {
  slug: string;
  meta: GuideMeta;
  sections: Section[];
}

/** A link in the hub's help strip. `external` opens in a new tab. */
export interface HubLink {
  href: string;
  label: string;
  external?: boolean;
}

export interface HubContent {
  meta: { title: string; description: string; eyebrow: string; heading: string; lead: string };
  tool: {
    href: string;
    eyebrow: string;
    name: string;
    description: string;
    cta: string;
    demoIn: string;
    demoOut: string;
  };
  help: { prompt: string; links: HubLink[] };
}
