// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// @ts-check
import { defineConfig } from 'astro/config';
import sitemap from '@astrojs/sitemap';
import { surfaceLocales } from '@bagel/kit/i18n/fs';

const locales = surfaceLocales('website');
const defaultLocale = 'en';

export default defineConfig({
  site: 'https://itsbagelbot.com',
  prefetch:  true,
  compressHTML: true,
  integrations: [
    sitemap({
      filter: (page) => !/\/changelog\/v[^/]+\/$/.test(page),
      i18n: {
        defaultLocale,
        locales: Object.fromEntries(locales.map((l) => [l, l])),
      },
      serialize(item) {
        const fallback = item.links?.find((l) => l.lang === defaultLocale);
        if (!fallback) return item;
        return { ...item, links: [...item.links, { lang: 'x-default', url: fallback.url }] };
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
