<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { onDestroy } from 'svelte';
  import type { HTMLInputAttributes } from 'svelte/elements';
  import '../styles/elements/field.css';
  import '../styles/elements/search-input.css';

  let {
    value = $bindable(''),
    element = $bindable<HTMLInputElement | undefined>(),
    class: className = '',
    placeholder = 'Search…',
    debounceMs = 0,
    clearLabel = 'Clear search',
    fill = false,
    oninput,
    ...rest
  }: {
    value?: string;
    element?: HTMLInputElement;
    class?: string;
    placeholder?: string;
    debounceMs?: number;
    clearLabel?: string;
    fill?: boolean;
    oninput?: (value: string) => void;
  } & Omit<HTMLInputAttributes, 'value' | 'oninput' | 'type'> = $props();

  let timer: ReturnType<typeof setTimeout> | undefined;

  onDestroy(() => clearTimeout(timer));

  function changed() {
    if (!oninput) return;
    if (!debounceMs) {
      oninput(value);
      return;
    }
    clearTimeout(timer);
    timer = setTimeout(() => oninput?.(value), debounceMs);
  }

  function clear() {
    value = '';
    clearTimeout(timer);
    oninput?.('');
  }
</script>

<label class="bb-search bb-input{fill ? ' bb-input--fill' : ''}{className ? ` ${className}` : ''}">
  <svg viewBox="0 0 24 24" aria-hidden="true"><circle cx="11" cy="11" r="7"></circle><path d="m20 20-3.5-3.5"></path></svg>
  <input type="search" class="bb-search__input" {placeholder} bind:this={element} bind:value oninput={changed} {...rest} />
  {#if value}
    <button type="button" class="bb-search__clear" aria-label={clearLabel} onclick={clear}>
      <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M18 6 6 18M6 6l12 12"></path></svg>
    </button>
  {/if}
</label>
