<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  // The console's error page: bot copy bound to `page.status`, rendered through
  // the design library's ErrorScene.
  //
  // The split is the point. Everything visual — the orbits, the outlined code,
  // the entrance stagger, the two action tones — is @bagel/ui's
  // `.bb-error-scene`. What is left here is the only part that is about this
  // product: which sentence a status code deserves, and what the one useful
  // action is from there. That is why this file has no <style> block and why
  // it stays in kit: it reads `$app/state`, which a design library must not.
  import ErrorScene from '@bagel/ui/svelte/ErrorScene.svelte';
  import { page } from '$app/state';

  let { appName, loginHref = '/login' }: { appName: string; loginHref?: string } = $props();

  const view = $derived.by(() => {
    if (page.status === 404) return {
      eyebrow: 'Lost in the crumbs',
      title: 'This page wandered off.',
      description: "I looked under every sesame seed, but the page you're after isn't here.",
      action: 'home' as const
    };
    if (page.status === 401 || page.status === 403) return {
      eyebrow: 'Behind the counter',
      title: 'This one is staff only.',
      description: `Sign in with an account that has access to the ${appName.toLowerCase()}.`,
      action: 'login' as const
    };
    if (page.status === 500 || page.status === 503) return {
      eyebrow: 'A little overbaked',
      title: 'Something went sideways.',
      description: 'A tray tipped over behind the scenes. Give it a moment, then try the page again.',
      action: 'retry' as const
    };
    return {
      eyebrow: 'An unexpected detour',
      title: 'I hit a rough patch.',
      description: page.error?.message ?? 'Something unexpected happened while loading this page.',
      action: 'retry' as const
    };
  });

  const homeLabel = $derived(appName === 'Dashboard' ? 'Back to dashboard' : `Back to ${appName.toLowerCase()}`);

  function retry() {
    window.location.reload();
  }

  // History length > 1 means there is somewhere to go back TO. Landing straight
  // on an error URL (a stale link, a bookmark) leaves history at 1, where
  // history.back() either does nothing or leaves the site entirely.
  function goBack() {
    if (window.history.length > 1) window.history.back();
    else window.location.assign('/');
  }
</script>

<svelte:head>
  <title>{page.status}: ItsBagelBot {appName}</title>
</svelte:head>

<ErrorScene
  status={page.status}
  eyebrow={view.eyebrow}
  title={view.title}
  description={view.description}
  aside="The oven is still warm. Everything else is right where you left it."
  labelledBy="error-title"
>
  {#snippet actions()}
    {#if view.action === 'retry'}
      <button class="bb-error-scene__action bb-error-scene__action--primary" type="button" onclick={retry}>Try again</button>
    {:else if view.action === 'login'}
      <a class="bb-error-scene__action bb-error-scene__action--primary" href={loginHref}>Sign in</a>
    {:else}
      <a class="bb-error-scene__action bb-error-scene__action--primary" href="/">{homeLabel}</a>
    {/if}
    <button class="bb-error-scene__action bb-error-scene__action--quiet" type="button" onclick={goBack}>Go back</button>
  {/snippet}
</ErrorScene>
