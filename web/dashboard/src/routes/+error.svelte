<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import '@bagel/kit/styles/ui-console.css';
  import { goto } from '$app/navigation';
  import { page } from '$app/state';
  import { ErrorView } from '@bagel/kit';
  import { errorTarget } from '#lib/error-routes';

  $effect(() => {
    const target = errorTarget(page.url, page.route.id, page.status);
    if (target) void goto(target, { replaceState: true });
  });
</script>

<ErrorView appName="Dashboard" loginHref="/login" />
