// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { SITE, webHref } from '@bagel/kit/site-links';
import type { Locale } from '@bagel/kit';

type Translate = (key: string, params?: Record<string, string>) => string;

export type ChannelPage = {
  ns: 'songPage' | 'queuePage';
  featurePath: string;
  url: string;
  login: string;
  channelName: string;
};

function twitchUrl(login: string): string {
  return `https://www.twitch.tv/${login}`;
}

export function channelPageSchema(page: ChannelPage, t: Translate, locale: Locale): Record<string, unknown> {
  const channel = { channel: page.channelName };
  return {
    '@context': 'https://schema.org',
    '@type': 'WebPage',
    '@id': `${page.url}#webpage`,
    url: page.url,
    name: t(`${page.ns}.title`, channel),
    description: t(`${page.ns}.metaDescription`, channel),
    inLanguage: locale,
    isPartOf: { '@id': `${SITE.web}/#website` },
    publisher: { '@id': `${SITE.web}/#organization` },
    about: { '@type': 'Person', name: page.channelName, url: twitchUrl(page.login) }
  };
}

export function channelAbout(page: ChannelPage, t: Translate, locale: Locale) {
  const channel = { channel: page.channelName };
  return {
    title: t(`${page.ns}.aboutTitle`, channel),
    body: t(`${page.ns}.aboutBody`, channel),
    links: [
      { href: twitchUrl(page.login), label: t(`${page.ns}.aboutTwitch`, channel), external: true },
      { href: webHref(locale, page.featurePath), label: t(`${page.ns}.aboutLink`) }
    ]
  };
}
