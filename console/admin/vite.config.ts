import { sveltekit } from '@sveltejs/kit/vite';
import { defineConfig } from 'vite';

export default defineConfig({
  plugins: [sveltekit()],
  // The shared package ships .svelte/.ts source; Vite must bundle (not externalize)
  // it for SSR so components compile. `nats` stays external (native-ish, server).
  ssr: { noExternal: ['@bagel/shared'] },
  server: { port: 5174 }
});
