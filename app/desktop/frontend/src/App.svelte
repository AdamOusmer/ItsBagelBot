<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { Component } from 'svelte';
  import { authState } from './lib/auth.svelte';
  import Login from './screens/Login.svelte';

  // Dev-only shell preview: ?screen=shell. `import.meta.env.DEV` is a build-time
  // constant, so a production build erases this whole branch (and the dynamic
  // import it guards) instead of shipping a dormant path into the shell.
  let Shell = $state<Component<Record<string, never>> | null>(null);

  if (import.meta.env.DEV) {
    const requested =
      import.meta.env.VITE_SCREEN ?? new URLSearchParams(location.search).get('screen');
    if (requested === 'shell') {
      import('./screens/Shell.svelte').then((module) => {
        Shell = module.default;
      });
    }
  }
</script>

{#if Shell && !authState.signedOut}
  <Shell />
{:else}
  <Login />
{/if}
