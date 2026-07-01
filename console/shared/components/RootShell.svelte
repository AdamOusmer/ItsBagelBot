<script lang="ts">
  import type { Snippet } from 'svelte';
  import { onMount } from 'svelte';
  import { afterNavigate, beforeNavigate } from '$app/navigation';
  import { updated } from '$app/state';
  import Cursor from './Cursor.svelte';
  import { initLenis } from '../lib/actions';

  let { children }: { children: Snippet } = $props();

  beforeNavigate((navigation) => {
    if (updated.current && navigation.to?.url && !navigation.willUnload) {
      navigation.cancel();
      location.href = navigation.to.url.href;
    }
  });

  onMount(() => {
    let teardown: (() => void) | undefined;
    initLenis().then((fn) => (teardown = fn));
    return () => teardown?.();
  });

  afterNavigate(() => {
    (window as unknown as { __lenis?: { scrollTo: (target: number, options?: object) => void } })
      .__lenis?.scrollTo(0, { immediate: true });
  });
</script>

<Cursor />
{@render children()}
