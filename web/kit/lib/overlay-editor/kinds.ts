// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { IconName } from '@bagel/ui/lib/icons';
import type { WidgetKind } from '../overlay-document';
import type { EditableKind } from './defaults';

const KIND_ICON: Record<EditableKind, IconName> = {
  alertbox: 'bell',
  eventlist: 'list',
  goal: 'activity',
  label: 'card',
  chatbox: 'users',
  emotewall: 'modules',
  timer: 'clock',
  credits: 'audit',
  text: 'edit',
  image: 'overlay',
  video: 'waveform',
  slideshow: 'sidebar'
};

export const kindIcon = (kind: WidgetKind): IconName => KIND_ICON[kind as EditableKind] ?? 'overlay';

export const KIND_GROUPS = [
  { key: 'alerts', kinds: ['alertbox', 'eventlist'] },
  { key: 'goals', kinds: ['goal', 'label', 'credits', 'timer'] },
  { key: 'chat', kinds: ['chatbox', 'emotewall'] },
  { key: 'media', kinds: ['text', 'image', 'video', 'slideshow'] }
] as const satisfies readonly { key: string; kinds: readonly EditableKind[] }[];

export type KindGroupKey = (typeof KIND_GROUPS)[number]['key'];

const GROUP_ICON: Record<KindGroupKey, IconName> = { alerts: 'bell', goals: 'activity', chat: 'users', media: 'overlay' };

export const groupIcon = (key: KindGroupKey): IconName => GROUP_ICON[key];

export type KindLabel = { kind: EditableKind; name: string; hint: string };

export function filterKinds(items: readonly KindLabel[], query: string): KindLabel[] {
  const needle = query.trim().toLowerCase();
  return needle ? items.filter((item) => `${item.name} ${item.hint}`.toLowerCase().includes(needle)) : [...items];
}
