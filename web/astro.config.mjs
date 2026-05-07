// @ts-check
import { defineConfig } from 'astro/config';
import sitemap from '@astrojs/sitemap';

// https://astro.build/config
export default defineConfig({
  site: 'https://itsbagelbot.com',
  prefetch:  true,
  compressHTML: true,
  integrations: [sitemap()],

  server: {
    host: true, // Listen on all local IP addresses
  },

  vite: {
    server: {
      allowedHosts: true, // Bypass Vite 6's network host blocking for external devices
    },
  },

  build: {
      inlineStylesheets: 'auto',
  },

});