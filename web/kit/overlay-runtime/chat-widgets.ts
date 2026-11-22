// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { recent, track } from './data-widgets';
import { messageNodes } from './emotes';
import { displayName } from './events';
import type { FeedEvent } from './feed';
import { applyFont, changed, node, timed, type Frame, type Install } from './view';
import type { WidgetSettings } from './widget-types';

const roles: Record<string, { glyph: string; label: string }> = {
  broadcaster: { glyph: 'B', label: 'Broadcaster' }, moderator: { glyph: 'M', label: 'Moderator' },
  vip: { glyph: 'V', label: 'VIP' }, subscriber: { glyph: 'S', label: 'Subscriber' },
  founder: { glyph: 'F', label: 'Founder' },
};

function badge(role: string): HTMLElement[] {
  const entry = roles[role];
  if (!entry) return [];
  const glyph = node('span', 'obs-chat-badge', entry.glyph);
  glyph.title = entry.label;
  glyph.setAttribute('aria-label', entry.label);
  return [glyph];
}

function chatLine(event: FeedEvent, settings: WidgetSettings['chatbox']): HTMLElement {
  const line = node('div', `obs-chat-line obs-chat-${settings.theme}`);
  const name = node('strong', 'obs-chat-name', `${displayName(event.user)}: `);
  const native = event.user?.color;
  name.style.color = settings.nativeColors && /^#[0-9a-f]{6}$/i.test(native ?? '') ? native ?? '' : settings.nameColor;
  const message = settings.emotes ? messageNodes(event.message) : [document.createTextNode(event.message?.text ?? '')];
  line.append(...(settings.badges ? (event.badges ?? []).flatMap(badge) : []), name, ...message);
  return line;
}

function chatAllowed(event: FeedEvent, settings: WidgetSettings['chatbox']): boolean {
  if (settings.hideCommands && event.message?.text?.trimStart().startsWith('!')) return false;
  const login = event.user?.login?.toLowerCase() ?? '';
  return !settings.hideUsers.some(user => user.trim().toLowerCase() === login);
}

function visibleChat(frame: Frame, transient: FeedEvent[], cleared: Map<string, number>, settings: WidgetSettings['chatbox']): FeedEvent[] {
  const { snapshot, now } = frame;
  const items = [...(snapshot.chat ?? []), ...transient].sort((a, b) => a.seq - b.seq);
  const visible = items.filter(item => item.seq > (cleared.get(item.user?.id ?? '') ?? -1) && chatAllowed(item, settings));
  return recent(visible, { now, fade: settings.fade }, settings.count, settings.direction);
}

export const installChat: Install<'chatbox'> = (host, settings, { assets }) => {
  applyFont(host, settings.style, assets);
  host.classList.toggle('obs-chat-bottom', settings.direction === 'newest_last');
  let transient: FeedEvent[] = [];
  const cleared = new Map<string, number>();
  return {
    event: event => {
      if (event.kind === 'user_cleared' && event.user?.id) cleared.set(event.user.id, event.seq);
      if (event.kind === 'chat' && event.test) transient = track(transient, event, 100);
    },
    ...timed(changed(frame => visibleChat(frame, transient, cleared, settings),
      lines => host.replaceChildren(...lines.map(line => chatLine(line, settings)))), 250),
  };
};