// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// @ts-check
import { defineConfig } from 'astro/config';
import sitemap from '@astrojs/sitemap';
import { readdirSync } from 'node:fs';
import { fileURLToPath } from 'node:url';

// The chat rehearsal (command builder) shares ONE source of truth with the
// dashboard: the pure, framework-free engine mirror in web/kit. The builder
// imports it as `@bagel/kit/engine/rehearsal` and renders the returned data
// with its own DOM, so the slash-verb grammar and token expansion can never
// drift from the bot the way a hand-copied version would. Same for the token
// lexer behind it (`@bagel/kit/engine/tmpl`), so the builder can ASK what a
// token is instead of matching one with a regex -- it is the same file pkg/tmpl
// is pinned against, so "{pointsname} is a var named pointsname" is answered by
// the grammar rather than re-guessed by a pattern that has to be kept in step
// with it (and was not: the pattern this replaced missed {pointsname}).
//
// These were two absolute-path Vite aliases until the kit rename. They are now
// real package imports: kit publishes exactly these six pure files under
// `./engine/*` subpath exports, so marketing can depend on `@bagel/kit`
// without Vite ever resolving kit's server half (nats, iovalkey, pino) -- the
// subpath map is the boundary the alias used to enforce by hand, and unlike the
// alias it also holds for `astro check` and for editors. The catalog (sample
// values, bilingual copy) stays local in src/i18n/builder.ts; only the logic is
// shared.

// The design primitives (the mote field's physics, brand.css, the card
// atmosphere) are no longer reached by path at all: they are `@bagel/ui/...`
// package imports, resolved through that package's subpath exports. This
// config used to carry two Vite aliases and an `fs.allow` widened to the
// workspace root to make those reach-ins work, and all three are gone with
// them -- an alias is invisible to `astro check` and to editors, and
// `fs.allow` on a directory hands the dev server every file under it.
//
// @bagel/ui is linked (`link:../ui`), so node_modules/@bagel/ui is a symlink
// and Vite resolves through it to the realpath. Nothing extra is needed for
// that here; what it does need is ui's own dependencies installed, which
// web/package.json's postinstall handles.

// Locales are discovered from the catalog files: one src/i18n/locales/<code>.json
// per language. Dropping in a new JSON adds the language to Astro's i18n config
// (and, via the same folder, to the runtime catalog in src/i18n/ui.ts) with no
// edits here.
const localesDir = fileURLToPath(new URL('./src/i18n/locales', import.meta.url));
const locales = readdirSync(localesDir)
  .filter((f) => f.endsWith('.json'))
  .map((f) => f.slice(0, -'.json'.length))
  .sort();
// English lives at the root; it is the locale that has no URL prefix. Named once
// here because both the router (i18n.defaultLocale) and the sitemap need it, and
// they must not be allowed to disagree.
const defaultLocale = 'en';

// https://astro.build/config
export default defineConfig({
  site: 'https://itsbagelbot.com',
  prefetch:  true,
  compressHTML: true,
  integrations: [
    sitemap({
      // Astro's i18n block below teaches the ROUTER how to build URLs. The sitemap
      // integration is a separate consumer and has to be told the same map again:
      // with `sitemap()` bare, the output is a flat list of locs and a crawler
      // reads /fr/pricing/ as a near-duplicate of /pricing/ rather than as its
      // French translation. Given this, each localized URL carries an
      // <xhtml:link rel="alternate"> per locale plus x-default, the same pairing
      // Layout.astro emits in <head>, so sitemap and markup agree instead of
      // contradicting one another (a contradiction Search Console reports and
      // then resolves by ignoring both).
      //
      // Keys are URL path prefixes, values hreflang codes; they match 1:1 here
      // because the locale folders are named with their language tag. defaultLocale
      // is the un-prefixed one (prefixDefaultLocale:false), so '/' pairs with
      // '/fr/' and not with an '/en/' this site never emits.
      i18n: {
        defaultLocale,
        locales: Object.fromEntries(locales.map((l) => [l, l])),
      },
      // The integration pairs the real locales but stops there: it never emits
      // x-default, the entry that names the page to serve a visitor whose
      // language matches none of them. Layout.astro already prints one in <head>
      // (pointing at the un-prefixed English URL), so without this the two
      // sources disagree on whether the site declares a fallback at all. Appending
      // it here re-uses the alternate the integration already resolved for the
      // default locale rather than rebuilding the URL, so the pair can't drift.
      //
      // Copy the array instead of pushing into it: the integration hands every
      // URL in a locale group the SAME links array by reference, so a push here
      // lands once per locale and the emitted entry carries a duplicate
      // x-default per translation.
      serialize(item) {
        const fallback = item.links?.find((l) => l.lang === defaultLocale);
        if (!fallback) return item;
        return { ...item, links: [...item.links, { lang: 'x-default', url: fallback.url }] };
      },
    }),
  ],

  // English at the root (/), other locales under /<code>/. prefixDefaultLocale:false
  // keeps every existing English URL exactly where it is, so nothing 301s.
  i18n: {
    defaultLocale,
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
    server: {
      allowedHosts: true, // Bypass Vite 6's network host blocking for external devices
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
