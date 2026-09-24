// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { AstroComponentFactory } from 'astro/runtime/server/index.js';
import type { Lang } from '../../i18n/ui';
import type { GuideSlug } from './slugs';

export type { Lang };

export interface ChatLine {
  who: 'viewer' | 'mod' | 'bot' | 'system';
  name?: string;
  text: string;
  time?: string;
}

export interface Note {
  n: number;
  text: string;
}

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

export type GuideComponent = AstroComponentFactory;

export type WidgetName =
  | 'Checklist'
  | 'PathPicker'
  | 'FetchBudget'
  | 'FetchOutcomes'
  | 'ModuleCatalog'
  | 'Rehearsal'
  | 'CounterPlay';

/** Trusted authored HTML rendered with set:html; never build it from visitor input. */
export type Block =
  | { kind: 'prose'; html: string }
  | { kind: 'callout'; tone: 'tip' | 'warn' | 'note'; html: string }
  | { kind: 'table'; head: string[]; rows: string[][]; caption?: string; id?: string }
  | { kind: 'chat'; title?: string; caption?: string; lines: ChatLine[] }
  | { kind: 'dash'; screen: ScreenName; path?: string; caption?: string; notes?: Note[]; labels?: Record<string, string> }
  | { kind: 'steps'; items: { title: string; html: string }[] }
  | { kind: 'cards'; columns?: 2 | 3; items: { title: string; html: string; chips?: string[]; badge?: string }[] }
  | { kind: 'widget'; name: WidgetName; labels?: Record<string, string>; props?: Record<string, unknown> };

export interface Section {
  id: string;
  heading: string;
  note?: string;
  blocks: Block[];
}

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
  slug: GuideSlug;
  meta: GuideMeta;
  sections: Section[];
}

export interface HubLink {
  href: string;
  label: string;
  external?: boolean;
}

export interface HubContent {
  meta: { title: string; description: string; eyebrow: string; heading: string; lead: string };
  readCta: string;
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
