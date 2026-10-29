<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { Snippet } from 'svelte';
  import '../styles/elements/var-card.css';

  type Own = {
    token: string;
    hint?: string;
    title?: string;
    onclick?: () => void;
    onfocus?: () => void;
    class?: string;
    children?: Snippet;
  };

  let { token, hint, title, onclick, onfocus, class: className = '', children }: Own = $props();

  const classes = $derived(
    ['bb-var-card', 'bb-var-card--list', onclick ? null : 'bb-var-card--static', className || null]
      .filter(Boolean)
      .join(' '),
  );
</script>

{#snippet content()}
  <span class="bb-var-card__token">{token}</span>
  {#if hint || children}
    <span class="bb-var-card__body">
      {#if hint}<small>{hint}</small>{/if}
      {#if children}{@render children()}{/if}
    </span>
  {/if}
  {#if onclick}<i class="bb-var-card__plus" aria-hidden="true">+</i>{/if}
{/snippet}

{#if onclick}
  <button type="button" class={classes} {title} {onclick} {onfocus}>{@render content()}</button>
{:else}
  <div class={classes} {title}>{@render content()}</div>
{/if}
