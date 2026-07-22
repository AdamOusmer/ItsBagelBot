// Side-effect import: sorts src/ directory reads so the native ARM/Intel image
// builds assign identical SvelteKit node IDs and emit byte-identical client
// bundles. Must live here (inside the build process) — bun ignores
// NODE_OPTIONS=--require, so a script-level shim never runs.
import '../sorted-readdir.mjs';
import { sveltekit } from '@sveltejs/kit/vite';
import { defineConfig } from 'vite';

export default defineConfig({
  plugins: [sveltekit()],
  // The shared package ships .svelte/.ts source; Vite must bundle (not externalize)
  // it for SSR so components compile. Native-ish server libraries stay external
  // and are resolved by Bun at runtime.
  // newrelic is a CJS agent with native modules + dynamic requires; it must stay
  // external so it resolves to the singleton preloaded via --import at runtime
  // (bundling it would break it and create a second, uninstrumented instance).
  // `pino` stays external so the New Relic agent's require-hook wraps the real
  // module at runtime and local-decorates its log lines (bundling defeats the hook).
  ssr: { noExternal: ['@bagel/shared'], external: ['mysql2', 'newrelic', 'iovalkey', 'pino'] },
  server: { port: 5174 },
  build: {
    minify: 'terser'
  }
});
