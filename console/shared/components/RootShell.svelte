<script lang="ts">
  import type { Snippet } from 'svelte';
  import { onMount } from 'svelte';
  import { browser } from '$app/environment';
  import { afterNavigate, beforeNavigate } from '$app/navigation';
  import { updated } from '$app/state';
  import Cursor from './Cursor.svelte';
  import { customCursor } from '../lib/cursor';
  import { initLenis } from '../lib/actions';
  import { setI18n } from '../lib/i18n/context';
  import { DEFAULT_LOCALE, type Locale } from '../lib/i18n/messages';

  let {
    children,
    locale = DEFAULT_LOCALE,
    cursorEnabled = true
  }: { children: Snippet; locale?: Locale; cursorEnabled?: boolean } = $props();

  // Publish the i18n translator to the whole render tree. Apps that don't pass a
  // locale (admin) get the default-locale translator, so nothing breaks. Reading
  // the initial value is intentional: switching locale sets a cookie and does a
  // full reload, so this render tree never needs to react to it in place.
  // svelte-ignore state_referenced_locally
  setI18n(locale);

  // Seed the custom-cursor preference before Cursor's effect first runs, so a
  // user who disabled it never sees a flash of the animated cursor. Client-only:
  // the store is a module singleton, so mutating it during SSR would leak one
  // request's preference into another's render. Apps that pass no value (admin)
  // keep the default (on).
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
