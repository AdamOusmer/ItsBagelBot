// @ts-check
import { defineConfig } from 'astro/config';
import sitemap from '@astrojs/sitemap';

import cloudflare from '@astrojs/cloudflare';

// https://astro.build/config
export default defineConfig({
  site: 'https://itsbagelbot.com',
  prefetch:  true,
  compressHTML: true,
  integrations: [sitemap()],

  server: {
      port: 4321,
      host: true
  },

  build: {
      inlineStylesheets: 'auto',
  },

  adapter: cloudflare()
});