<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import type { Snippet } from 'svelte';
  import { onMount } from 'svelte';
  import { browser } from '$app/environment';
  import { afterNavigate, beforeNavigate } from '$app/navigation';
  import { updated } from '$app/state';
  import Cursor from '@bagel/ui/svelte/Cursor.svelte';
  import BackgroundOrbs from '@bagel/ui/svelte/BackgroundOrbs.svelte';
  import { customCursor } from '../lib/cursor';
  import { initLenis } from '../lib/actions';
  import { setI18n } from '../lib/i18n/context';
  import { DEFAULT_LOCALE, type Locale } from '../lib/i18n/messages';

  let {
    children,
    locale = DEFAULT_LOCALE,
    cursorEnabled = true,
    orbs = true
  }: {
    children: Snippet;
    locale?: Locale;
    cursorEnabled?: boolean;
    orbs?: boolean;
  } = $props();

  // svelte-ignore state_referenced_locally
  setI18n(locale);

  // svelte-ignore state_referenced_locally
  if (browser) customCursor.set(cursorEnabled);

  beforeNavigate((navigation) => {
    if (updated.current && navigation.to?.url && !navigation.willUnload) {
      navigation.cancel();
      location.href = navigation.to.url.href;
    }
  });

  onMount(() => {
    let teardown: (() => void) | undefined;
    initLenis().then((fn) => (teardown = fn));

    // bfcache guard: Safari restores the previous page's DOM even with no-store, so force a real load.
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

{#if orbs}<BackgroundOrbs />{/if}
<Cursor enabled={$customCursor} />
{@render children()}
