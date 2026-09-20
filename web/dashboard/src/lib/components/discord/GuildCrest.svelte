<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  // The guild tile: the server's own icon when Discord has one, the monogram
  // otherwise. One component for the hub cards, the picker rows and the guild
  // header, which used to carry three copies of the same 44px crest.
  //
  // The icon is queried, never stored: the URL arrives with every read (the
  // hash from /users/@me/guilds on the picker, outgress's with_counts lookup
  // everywhere else), and a server that changes its icon 404s the old file.
  // A load error therefore falls back to the monogram rather than leaving a
  // broken box; so does a CSP block on a console that has not opened the CDN.
  // referrerpolicy keeps the dashboard URL out of the CDN request. The CSP
  // opens cdn.discordapp.com for images only (web/kit/svelte-config.js).
  import { guildIconSrc, guildMonogram } from '@bagel/kit';

  let { name, iconUrl = '' }: { name: string; iconUrl?: string } = $props();

  const src = $derived(guildIconSrc(iconUrl));
  // The url that failed, not a flag: a new url (the switcher moving to
  // another guild) gets its own chance instead of inheriting the failure.
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
