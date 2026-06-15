<script lang="ts">
  import '../app.css';
  import { onMount } from 'svelte';
  import { afterNavigate } from '$app/navigation';
  import { Cursor, initLenis } from '@bagel/shared';
  let { children } = $props();

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
