// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
//
// Every value below is sample data for the overlay editor preview. None of
// it is real: no real streamer, viewer, or chat message. Keep all fake
// overlay content in this file so it stays easy to find and audit.
import type { ChatMessage, Layer } from './types';

export const SAMPLE_CHAT_MESSAGES: readonly ChatMessage[] = [
  { user: 'sample_viewer_one', tone: 'none', message: 'this is a sample chat message' },
  { user: 'sample_subscriber', tone: 'paid', message: 'another placeholder line' },
  { user: 'sample_vip_viewer', tone: 'vip', message: 'preview text, not a real chat' },
  { user: 'sample_regular', tone: 'free', message: 'fake message for layout only' },
  { user: 'sample_newcomer', tone: 'none', message: 'sample overlay content' },
];

export function sampleLayers(): Layer[] {
  return [
    {
      id: 'sample-alert',
      kind: 'alert',
      name: 'Sample alert box',
      x: 6,
      y: 6,
      w: 44,
      h: 28,
      opacity: 100,
      title: 'Sample Streamer just subscribed',
      subtitle: 'Thanks for the sample support',
      tone: 'paid',
    },
    {
      id: 'sample-text',
      kind: 'text',
      name: 'Sample lower text',
      x: 6,
      y: 82,
      w: 60,
      h: 10,
      opacity: 100,
      content: 'Sample overlay text, not real stream copy',
      fontSize: 5,
      color: '#f6efe1',
      align: 'start',
    },
    {
      id: 'sample-image',
      kind: 'image',
      name: 'Sample image placeholder',
      x: 68,
      y: 6,
      w: 26,
      h: 26,
      opacity: 100,
      caption: 'Sample image placeholder',
      fit: 'contain',
    },
    {
      id: 'sample-chat',
      kind: 'chat',
      name: 'Sample chat box',
      x: 66,
      y: 40,
      w: 32,
      h: 50,
      opacity: 100,
      rows: 4,
      messages: SAMPLE_CHAT_MESSAGES.slice(),
    },
  ];
}
