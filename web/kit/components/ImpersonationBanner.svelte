<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  // The standing bar that says a staff member is acting AS someone else.
  //
  // A wrapper, not an element: the fixed inverted band is @bagel/ui's
  // `.bb-alert--impersonation`, and what is left here is the bot's — which
  // route ends the session (`POST /auth/logout`) and whether this app exits by
  // form or by link. That split is why the file has no <style> block.
  //
  // Form vs link is not cosmetic. The dashboard ends impersonation with a POST
  // (it mutates the session), so it cannot be an <a>; the admin console links
  // back to a page that does the same server-side. Both render the same
  // control, which is the whole point of the shared class.
  import AlertBanner from '@bagel/ui/svelte/AlertBanner.svelte';
  import type { Snippet } from 'svelte';
  let { exitHref, exitForm = false, exitLabel = 'Exit', children }:
    { exitHref?: string; exitForm?: boolean; exitLabel?: string; children: Snippet } = $props();
</script>

<AlertBanner variant="impersonation" role="status">
  {@render children()}
  {#snippet action()}
    {#if exitForm}
      <form method="POST" action="/auth/logout"><button type="submit" class="bb-alert__exit">{exitLabel}</button></form>
    {:else}
      <a href={exitHref} class="bb-alert__exit">{exitLabel}</a>
    {/if}
  {/snippet}
</AlertBanner>
