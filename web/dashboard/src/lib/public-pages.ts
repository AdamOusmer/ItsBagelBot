// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { commandsHref, leaderboardHref, songqueueHref } from '@bagel/kit/site-links';

export const PUBLIC_PAGES = ['commands', 'leaderboard', 'songqueue'] as const;

export type PublicPage = (typeof PUBLIC_PAGES)[number];

export type PageFlags = Record<PublicPage, boolean>;

export type PageHiddenField = `${PublicPage}_page_hidden`;

export const pageHiddenField = (page: PublicPage) => `${page}_page_hidden` as const;

export const pageSetVerb = (page: PublicPage) => `${page}_page_set` as const;

export const pageScope = (page: PublicPage) => `${page}_page` as const;

export const PAGE_ACTION = {
  commands: 'setCommandsPage',
  leaderboard: 'setLeaderboardPage',
  songqueue: 'setSongQueuePage'
} as const satisfies Record<PublicPage, string>;

export const PAGE_HREF: Record<PublicPage, (login: string) => string> = {
  commands: commandsHref,
  leaderboard: leaderboardHref,
  songqueue: songqueueHref
};

// Object.freeze tables: the i18n slicer only proves coverage for t(TABLE[key]) over a frozen table of strings.
export const PAGE_LABEL = Object.freeze({
  commands: 'settings.commandsPage',
  leaderboard: 'settings.leaderboardPage',
  songqueue: 'settings.songqueuePage'
});

export const PAGE_HINT = Object.freeze({
  commands: 'settings.commandsPageHint',
  leaderboard: 'settings.leaderboardPageHint',
  songqueue: 'settings.songqueuePageHint'
});

export const PAGE_DELAYED = Object.freeze({
  commands: 'settings.commandsPageDelayed',
  leaderboard: 'settings.leaderboardPageDelayed',
  songqueue: 'settings.songqueuePageDelayed'
});

export const PAGE_TOAST_ON = Object.freeze({
  commands: 'settings.toastCommandsPageOn',
  leaderboard: 'settings.toastLeaderboardPageOn',
  songqueue: 'settings.toastSongqueuePageOn'
});

export const PAGE_TOAST_OFF = Object.freeze({
  commands: 'settings.toastCommandsPageOff',
  leaderboard: 'settings.toastLeaderboardPageOff',
  songqueue: 'settings.toastSongqueuePageOff'
});
