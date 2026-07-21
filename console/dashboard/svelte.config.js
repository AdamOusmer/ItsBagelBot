import { consoleKitConfig } from '../shared/svelte-config.js';

/** @type {import('@sveltejs/kit').Config} */
export default consoleKitConfig({
  // The dashboard never embeds anything; block iframes outright (the admin
  // keeps the default-src fallback).
  directives: { 'frame-src': ['none'] }
});
