<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { Component } from 'svelte';
  import { authState, observeAuth } from './lib/auth.svelte';
  import { onMount } from 'svelte';
  import Login from './screens/Login.svelte';

  let Shell = $state<Component<Record<string, never>> | null>(null);
  let preview = $state(false);

  if (import.meta.env.DEV) {
    const requested =
      import.meta.env.VITE_SCREEN ?? new URLSearchParams(location.search).get('screen');
    if (requested === 'shell') {
      preview = true;
    }
  }

  $effect(() => {
    if ((authState.signedIn || preview) && !Shell) {
      import('./screens/Shell.svelte').then((module) => { Shell = module.default; });
    }
  });
  onMount(observeAuth);
</script>

{#if Shell && (authState.signedIn || preview) && !authState.signedOut}
  <Shell />
{:else}
  <Login />
{/if}
