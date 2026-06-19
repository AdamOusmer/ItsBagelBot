import { sveltekit } from '@sveltejs/kit/vite';
import { defineConfig } from 'vite';

export default defineConfig({
  plugins: [sveltekit()],
  // The shared package ships .svelte/.ts source; Vite must bundle (not externalize)
  // it for SSR so components compile. Native-ish server libraries stay external
  // and are resolved by Bun at runtime.
  ssr: { noExternal: ['@bagel/shared'], external: ['mysql2'] },
  server: { port: 5174 }
});
