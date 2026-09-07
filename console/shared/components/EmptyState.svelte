<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import type { Snippet } from 'svelte';

  let {
    title,
    body = undefined as string | undefined,
    children = undefined as Snippet | undefined
  }: {
    title: string;
    body?: string;
    children?: Snippet;
  } = $props();
</script>

<div class="empty">
  <p class="title">{title}</p>
  {#if body}<p class="body">{body}</p>{/if}
  {#if children}<div class="cta">{@render children()}</div>{/if}
</div>

<style>
  .empty {
    padding: 36px 20px;
    text-align: center;
    color: var(--bb-muted);
    font-family: var(--bb-font-body);
  }
  .title {
    font-family: var(--bb-font-display);
    font-weight: 700;
    font-size: 17px;
    color: var(--bb-white);
    margin: 0 0 6px;
    letter-spacing: -0.01em;
  }
  .body { font-size: 13px; line-height: 1.5; margin: 0 auto; max-width: 42ch; }
  .cta { margin-top: 16px; display: flex; justify-content: center; gap: 10px; flex-wrap: wrap; }
</style>
