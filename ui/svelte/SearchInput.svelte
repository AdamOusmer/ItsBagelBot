<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  //
  // Search field on the shared .bb-input frame, with an optional debounce for
  // callers that trigger work per keystroke and a one-tap clear.
  //
  // The two glyphs are inline SVG rather than <Icon>. @bagel/ui must not import
  // @bagel/kit (the dependency direction is one-way and
  // scripts/assert-framework-free.mjs enforces it), and the ui-side Icon lands
  // in the nav PR; two 60-byte paths inlined here cost less than blocking this
  // element on that one, and the Astro twin needs the same literal markup for
  // the parity test anyway.
  //
  // The debounce is a plain timer in the adapter, not an engine in ui/lib: it
  // owns no DOM and has no lifecycle beyond this component, so abstracting it
  // would buy an import and nothing else.
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
    // Take the container's width instead of the frame's 240px default. The
    // toolbar search boxes on five pages each carried a scoped
    // `:global(.bb-input) { width: 100% }` for this; the modifier is the
    // contract's own (../styles/elements/field.css).
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
