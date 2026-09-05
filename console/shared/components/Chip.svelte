<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import type { Snippet } from 'svelte';
  // type defaults to "button", not the HTML default "submit". Every Chip in
  // this console is an in-page toggle or a label, and several sit inside the
  // one big <form> a settings page posts: with the HTML default, clicking a
  // category chip to REMOVE it also submitted the whole form. Callers that
  // genuinely want a submit chip pass type="submit" explicitly.
  let { on = false, onclick, type = 'button', class: cls = '', children, ...rest }:
    {
      on?: boolean;
      onclick?: () => void;
      type?: 'button' | 'submit' | 'reset';
      class?: string;
      children: Snippet;
      [key: string]: unknown;
    } = $props();
</script>

<button {type} class="chip {on ? 'on' : ''} {cls}" {onclick} {...rest}>{@render children()}</button>

<style>
  .chip { font-family: var(--bb-font-mono); font-size: 11px; letter-spacing: 0.06em; padding: 8px 14px; border-radius: var(--bb-radius-pill); white-space: nowrap;
    background: rgba(255,255,255,0.03); border: 1px solid var(--glass-border); color: var(--bb-muted); cursor: pointer; transition: all var(--bb-dur-base) var(--bb-ease-out-expo); }
  .chip:hover:not(:disabled) { color: var(--bb-white); border-color: var(--bb-border-strong); }
  .chip:disabled { cursor: not-allowed; opacity: 0.55; }
  .chip.on { color: var(--bb-white); background: var(--ui-accent-soft); border-color: var(--bb-border-strong); }
</style>
