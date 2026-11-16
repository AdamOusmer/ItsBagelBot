<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import { page } from '$app/state';
  import PublicShell from '@bagel/kit/components/PublicShell.svelte';
  import { publicPageForRoute, usagePayload } from '@bagel/kit/analytics-beacon';

  let { children } = $props();

  $effect(() => {
    const dim = publicPageForRoute(page.route.id);
    const payload = dim ? usagePayload({ event: 'public.views', dim }) : null;
    if (payload) navigator.sendBeacon('/b', payload);
  });
</script>

<PublicShell footer={page.route.id !== '/(public)/login'}>{@render children()}</PublicShell>
