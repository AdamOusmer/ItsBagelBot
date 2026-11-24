// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { FeedStats, OverlayEvent } from './feed';
import type { CustomValue } from './widget-types';

export type FrameBundle = { html: string; css: string; js: string };
export type ParentMessage =
  | { bagel: 1; type: 'load'; bundle: FrameBundle; fields: Record<string, CustomValue>; session: FeedStats }
  | { bagel: 1; type: 'event'; event: OverlayEvent }
  | { bagel: 1; type: 'session'; session: FeedStats };
export type FrameMessage = { bagel: 1; type: 'ready' };

export const isFrameMessage = (data: unknown): data is FrameMessage =>
  typeof data === 'object' && data !== null && (data as FrameMessage).bagel === 1 && (data as FrameMessage).type === 'ready';

const isRecord = (value: unknown): value is Record<string, unknown> =>
  typeof value === 'object' && value !== null && !Array.isArray(value);

const isBundle = (value: unknown): value is FrameBundle =>
  isRecord(value) && ['html', 'css', 'js'].every((key) => typeof value[key] === 'string');

const parentShapes: Record<ParentMessage['type'], (data: Record<string, unknown>) => boolean> = {
  load: (data) => isBundle(data.bundle) && isRecord(data.fields) && isRecord(data.session),
  event: (data) => isRecord(data.event),
  session: (data) => isRecord(data.session)
};

export const isParentMessage = (data: unknown): data is ParentMessage => {
  if (!isRecord(data) || data.bagel !== 1 || typeof data.type !== 'string') return false;
  const shape = Object.entries(parentShapes).find(([type]) => type === data.type);
  return shape !== undefined && shape[1](data);
};
