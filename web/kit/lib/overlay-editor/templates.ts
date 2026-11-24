// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { OverlayDocument } from '../overlay-document';
import { blankDocument } from './document';
import { newWidget, type EditableKind } from './defaults';

type Box = { x: number; y: number; w: number; h: number };
type TemplateItem = [EditableKind, Box];

const templates = {
  alerts: [['alertbox', { x: 480, y: 230, w: 960, h: 580 }]],
  justChatting: [
    ['chatbox', { x: 1420, y: 270, w: 440, h: 700 }],
    ['alertbox', { x: 360, y: 170, w: 880, h: 500 }],
    ['goal', { x: 65, y: 930, w: 650, h: 100 }],
    ['eventlist', { x: 1420, y: 50, w: 440, h: 200 }]
  ],
  startingSoon: [
    ['timer', { x: 410, y: 380, w: 1100, h: 235 }],
    ['label', { x: 560, y: 655, w: 800, h: 100 }]
  ]
} satisfies Record<string, TemplateItem[]>;

export type Template = keyof typeof templates;
export const templateNames = Object.keys(templates) as Template[];

export function createOverlay(template: Template, name: string, kindName: (kind: EditableKind) => string): OverlayDocument {
  const doc = blankDocument(name);
  const widgets = templates[template].map(([kind, box]) => ({ ...newWidget(kind, doc, kindName(kind)), ...box }));
  return { ...doc, widgets };
}
