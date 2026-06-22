import { sveltekit } from '@sveltejs/kit/vite';
import { defineConfig } from 'vite';

export default defineConfig({
  plugins: [sveltekit()],
  // The shared package ships .svelte/.ts source; Vite must bundle (not externalize)
  // it for SSR so components compile. `newrelic` must stay external so it resolves
  // to the singleton preloaded via --import at runtime (bundling its native modules
  // + dynamic requires would break it and create a second, uninstrumented instance).
  ssr: { noExternal: ['@bagel/shared'], external: ['newrelic'] },
  server: { port: 5173 },
  build: {
    minify: 'terser'
  }
});
