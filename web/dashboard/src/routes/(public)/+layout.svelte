<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import { page } from '$app/state';
  import PublicShell from '@bagel/kit/components/PublicShell.svelte';

  let { children } = $props();
</script>

<PublicShell footer={page.route.id !== '/(public)/login'}>{@render children()}</PublicShell>
