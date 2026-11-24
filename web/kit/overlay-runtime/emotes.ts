// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { emoteURL } from './events';
import type { OverlayEvent } from './feed';
import { node } from './view';

type Run = { text: string; url?: string };

export function messageRuns(message: OverlayEvent['message']): Run[] {
  const { parts, text } = message ?? {};
  if (!parts?.length) return [{ text: text ?? '' }];
  return parts.map((part) => (part.t === 'emote' ? { text: part.text ?? '', url: emoteURL(part.id) } : { text: part.text ?? '' }));
}

export function messageNodes(message: OverlayEvent['message']): Node[] {
  return messageRuns(message).map((run) => {
    if (!run.url) return document.createTextNode(run.text);
    const img = node('img', 'obs-chat-emote');
    img.src = run.url;
    img.alt = run.text;
    return img;
  });
}
