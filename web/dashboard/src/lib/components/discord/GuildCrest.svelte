<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { guildIconSrc, guildMonogram } from '@bagel/kit';

  let { name, iconUrl = '' }: { name: string; iconUrl?: string } = $props();

  const src = $derived(guildIconSrc(iconUrl));
  let failedSrc = $state('');
</script>

<span class="crest" aria-hidden="true">
  {#if src && src !== failedSrc}
    <img
      {src}
      alt=""
      width="44"
      height="44"
      loading="lazy"
      decoding="async"
      referrerpolicy="no-referrer"
      onerror={() => (failedSrc = src)}
    />
  {:else}
    {guildMonogram(name)}
  {/if}
</span>

<style>
  .crest {
    flex: none;
    width: 44px;
    height: 44px;
    border-radius: var(--bb-radius-sm);
    display: grid;
    place-items: center;
    overflow: hidden;
    background: rgba(201, 168, 124, 0.12);
    border: 1px solid var(--glass-border);
    color: var(--bb-tan-light);
    font-family: var(--bb-font-display);
    font-weight: 700;
    font-size: 15px;
    letter-spacing: 0.02em;
  }
  img {
    display: block;
    width: 100%;
    height: 100%;
    object-fit: cover;
  }
</style>
