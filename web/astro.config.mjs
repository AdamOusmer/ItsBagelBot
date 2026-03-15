// @ts-check
import { defineConfig } from 'astro/config';
import sitemap from '@astrojs/sitemap';

// https://astro.build/config
export default defineConfig({
    site: 'https://adam-ousmer.dev',
    prefetch:  true,

    compressHTML: true,

    integrations: [sitemap()],

    server: {
        port: 4321,
        host: true
    },


    build: {
        inlineStylesheets: 'auto',
    }
});
