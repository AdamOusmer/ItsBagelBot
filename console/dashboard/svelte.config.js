import adapter from '@sveltejs/adapter-node';
import { vitePreprocess } from '@sveltejs/vite-plugin-svelte';

/** @type {import('@sveltejs/kit').Config} */
export default {
  preprocess: vitePreprocess(),
  kit: {
    adapter: adapter({ precompress: true }),
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
