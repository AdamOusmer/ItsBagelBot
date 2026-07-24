// @ts-check
import { defineConfig } from 'astro/config';
import sitemap from '@astrojs/sitemap';
import { readdirSync } from 'node:fs';
import { fileURLToPath } from 'node:url';

// The chat rehearsal (command builder) shares ONE source of truth with the
// dashboard: the pure, framework-free engine mirror in console/shared. The
// builder imports it as `@bagel/rehearsal` and renders the returned data with
// its own DOM, so the slash-verb grammar and token expansion can never drift
// from the bot the way a hand-copied version would. The file is pure TS with
// no runtime deps (it pulls only two helpers from commands-validate.ts), so
// Vite bundles a few KB of logic into the client chunk — no server code, no
// @bagel/shared install. The catalog (sample values, bilingual copy) stays
// local in src/i18n/builder.ts; only the logic is shared.
const rehearsalCore = fileURLToPath(new URL('../console/shared/lib/rehearsal.ts', import.meta.url));
// Repo root, so Vite's dev server may read the shared file that lives outside web/.
const repoRoot = fileURLToPath(new URL('..', import.meta.url));

// Locales are discovered from the catalog files: one src/i18n/locales/<code>.json
// per language. Dropping in a new JSON adds the language to Astro's i18n config
// (and, via the same folder, to the runtime catalog in src/i18n/ui.ts) with no
// edits here.
const localesDir = fileURLToPath(new URL('./src/i18n/locales', import.meta.url));
const locales = readdirSync(localesDir)
  .filter((f) => f.endsWith('.json'))
  .map((f) => f.slice(0, -'.json'.length))
  .sort();

// https://astro.build/config
export default defineConfig({
  site: 'https://itsbagelbot.com',
  prefetch:  true,
  compressHTML: true,
  integrations: [sitemap()],

  // English at the root (/), other locales under /<code>/. prefixDefaultLocale:false
  // keeps every existing English URL exactly where it is, so nothing 301s.
  i18n: {
    defaultLocale: 'en',
    locales,
    routing: {
      prefixDefaultLocale: false,
      redirectToDefaultLocale: false,
    },
  },

  server: {
    host: true, // Listen on all local IP addresses
  },

  vite: {
    resolve: {
      // Single source of truth for the rehearsal logic (see rehearsalCore above).
      alias: { '@bagel/rehearsal': rehearsalCore },
    },
    server: {
      allowedHosts: true, // Bypass Vite 6's network host blocking for external devices
      // Let the dev server serve the shared rehearsal file from the repo root.
      fs: { allow: [repoRoot] },
    },
    build: {
      // The production CSP only permits scripts loaded from this origin.
      // Keep Astro/Vite from turning small script chunks into inline tags.
      assetsInlineLimit: 0,
    },
  },

  build: {
      inlineStylesheets: 'auto',
  },

  markdown: {
      // Legal copy is transcribed verbatim from the previous inline HTML. Keep
      // typography literal (no curly-quote / dash substitution) so the rendered
      // text stays byte-for-byte what it was.
      smartypants: false,
  },

});
