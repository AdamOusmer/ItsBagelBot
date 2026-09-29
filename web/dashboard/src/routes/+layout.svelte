<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import '../app.css';
  import { page } from '$app/state';
  import RootShell from '@bagel/kit/components/RootShell.svelte';
  import ToastHost from '@bagel/ui/svelte/ToastHost.svelte';
  import { translate } from '@bagel/kit/i18n';
  import InstallAppPrompt from '$lib/components/InstallAppPrompt.svelte';
  let { data, children } = $props();

  const DEFAULT_TITLE = $derived(translate(data.locale, 'meta.defaultTitle'));
  const DEFAULT_DESC = $derived(translate(data.locale, 'meta.defaultDescription'));
</script>

<svelte:head>
  <title>{DEFAULT_TITLE}</title>
  <meta name="description" content={DEFAULT_DESC} />
  <meta property="og:title" content={DEFAULT_TITLE} />
  <meta property="og:description" content={DEFAULT_DESC} />
  <meta name="twitter:title" content={DEFAULT_TITLE} />
  <meta name="twitter:description" content={DEFAULT_DESC} />
</svelte:head>

<RootShell locale={data.locale} cursorEnabled={data.cursorEnabled} orbs={page.route.id !== '/(public)/login'}>
  {@render children()}
  <InstallAppPrompt />
  <ToastHost />
</RootShell>
