import adapter from 'svelte-adapter-bun';
import { vitePreprocess } from '@sveltejs/vite-plugin-svelte';

/** @type {import('@sveltejs/kit').Config} */
export default {
  preprocess: vitePreprocess(),
  kit: {
    adapter: adapter({ precompress: true }),
    // Deterministic build id (commit SHA via BUILD_VERSION) so the per-arch
    // native builds (ARM/Intel) emit identical hashed asset names. Default is a
    // timestamp, which diverges across the two builds and 404s chunks under the
    // stateless LB.
    version: { name: process.env.BUILD_VERSION || 'dev' },
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
        'connect-src': ['self'],
        'object-src': ['none'],
        'base-uri': ['self'],
        'frame-ancestors': ['none']
      }
    }
  }
};
