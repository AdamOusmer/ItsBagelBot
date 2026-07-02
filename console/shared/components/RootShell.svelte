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

    // bfcache guard: Safari (and iOS) restore the frozen DOM of the last page
    // even with Cache-Control: no-store, so reopening/returning to the app shows
    // the previous route's body while the fresh nav highlights the new URL (e.g.
    // stale /settings under an "Overview" nav). Force a real load so SSR is
    // authoritative and the visible page always matches the URL.
    const onPageShow = (e: PageTransitionEvent) => {
      if (e.persisted) location.reload();
    };
    window.addEventListener('pageshow', onPageShow);

    return () => {
      teardown?.();
      window.removeEventListener('pageshow', onPageShow);
    };
  });

  afterNavigate(() => {
    (window as unknown as { __lenis?: { scrollTo: (target: number, options?: object) => void } })
      .__lenis?.scrollTo(0, { immediate: true });
  });
</script>

<Cursor />
{@render children()}
