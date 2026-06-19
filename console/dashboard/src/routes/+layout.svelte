<script lang="ts">
  import '../app.css';
  import { onMount } from 'svelte';
  import { afterNavigate, beforeNavigate } from '$app/navigation';
  import { updated } from '$app/state';
  import { Cursor, initLenis } from '@bagel/shared';
  let { children } = $props();

  // When version.json polling detects a new deploy, hard-navigate to the target
  // so the fresh manifest + chunks load, instead of the client router fetching
  // now-deleted chunk hashes (the 404 / "Importing a module script failed" storm).
  beforeNavigate((nav) => {
    if (updated.current && nav.to?.url && !nav.willUnload) {
      nav.cancel();
      location.href = nav.to.url.href;
    }
  });

  onMount(() => {
    let teardown: (() => void) | undefined;
    initLenis().then((fn) => (teardown = fn));
    return () => teardown?.();
  });

  // Keep lenis in sync with SvelteKit's scroll reset on navigation.
  afterNavigate(() => {
    (window as unknown as { __lenis?: { scrollTo: (t: number, o?: object) => void } }).__lenis?.scrollTo(0, { immediate: true });
  });
</script>

<Cursor />
{@render children()}
