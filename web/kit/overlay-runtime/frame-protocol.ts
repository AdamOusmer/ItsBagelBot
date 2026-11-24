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

const parentTypes = ['load', 'event', 'session'];
export const isParentMessage = (data: unknown): data is ParentMessage =>
  typeof data === 'object' &&
  data !== null &&
  (data as ParentMessage).bagel === 1 &&
  parentTypes.includes((data as ParentMessage).type);
