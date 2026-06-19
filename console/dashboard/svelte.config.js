import adapter from '@sveltejs/adapter-node';
import { vitePreprocess } from '@sveltejs/vite-plugin-svelte';

/** @type {import('@sveltejs/kit').Config} */
export default {
  preprocess: vitePreprocess(),
  kit: {
    adapter: adapter({ precompress: true }),
    output: {
      bundleStrategy: 'single'
    },
    // Deterministic build id (commit SHA via BUILD_VERSION) so the per-arch
    // native builds (ARM/Intel) emit identical hashed asset names. Default is a
    // timestamp, which diverges across the two builds and 404s chunks under the
    // stateless LB.
    // pollInterval lets the client poll _app/version.json and flip the `updated`
    // store on a new deploy, so the root layout can force a full reload instead
    // of fetching a now-deleted bundle hash (404 -> dead SPA until hard refresh).
    version: { name: process.env.BUILD_VERSION || 'dev', pollInterval: 60000 },
    paths: {
      relative: false
    },
    // SvelteKit owns script/style nonces (mode 'auto'); the remaining headers
    // (HSTS, frame/sniff/referrer) are set in hooks.server.ts.
    csp: {
      mode: 'auto',
      directives: {
        'default-src': ['self'],
        'script-src': ['self'],
        'style-src': ['self'],
        'style-src-attr': ['unsafe-inline'],
        'font-src': ['self'],
        'img-src': ['self', 'data:'],
        'connect-src': ['self', 'https://dashboard.itsbagelbot.com'],
        'object-src': ['none'],
        'base-uri': ['self'],
        'frame-ancestors': ['none']
      }
    }
  }
};
