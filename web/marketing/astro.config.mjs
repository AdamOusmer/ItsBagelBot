// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// @ts-check
import { defineConfig } from 'astro/config';
import sitemap from '@astrojs/sitemap';
import { existsSync } from 'node:fs';
import { surfaceLocales } from '@bagel/kit/i18n/fs';

const locales = surfaceLocales('website');
const defaultLocale = 'en';
const guideDir = new URL('./src/content/guides/', import.meta.url);

function hasGuideBody(url) {
  const match = new URL(url).pathname.match(/^\/(?:([^/]+)\/)?guides\/(?:([^/]+)\/)?$/);
  if (!match) return true;
  const [, lang = defaultLocale, slug = 'hub'] = match;
  if (lang === defaultLocale || !locales.includes(lang)) return true;
  if (!existsSync(new URL(`${slug}.${defaultLocale}.ts`, guideDir)) && slug !== 'hub') return true;
  return existsSync(new URL(`${slug}.${lang}.json`, guideDir));
}

export default defineConfig({
  site: 'https://itsbagelbot.com',
  prefetch:  true,
  compressHTML: true,
  integrations: [
    sitemap({
      filter: (page) => !/\/changelog\/v[^/]+\/$/.test(page) && hasGuideBody(page),
      i18n: {
        defaultLocale,
        locales: Object.fromEntries(locales.map((l) => [l, l])),
      },
      serialize(item) {
        const links = item.links?.filter((l) => hasGuideBody(l.url));
        const fallback = links?.find((l) => l.lang === defaultLocale);
        if (!links || !fallback) return item;
        return { ...item, links: [...links, { lang: 'x-default', url: fallback.url }] };
      },
    }),
  ],

  i18n: {
    defaultLocale,
    locales,
    routing: {
      prefixDefaultLocale: false,
      redirectToDefaultLocale: false,
    },
  },

  server: {
    host: true,
  },

  vite: {
    server: {
      allowedHosts: true,
    },
    build: {
      // The CSP allows only same-origin scripts: never inline small chunks.
      assetsInlineLimit: 0,
    },
  },

  build: {
      inlineStylesheets: 'never',
  },

  markdown: {
      // Legal copy must render byte-for-byte: no typographic substitution.
      smartypants: false,
  },

});
