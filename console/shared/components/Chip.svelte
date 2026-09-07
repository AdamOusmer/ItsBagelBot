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

<button {type} class="bb-chip {on ? 'is-on' : ''} {cls}" {onclick} {...rest}>{@render children()}</button>

<style>
  /* Was a pill (--bb-radius-pill) with a translucent fill; the frame is now the
     global Tier-2 control (.bb-chip). `on` keeps its meaning, drawn as the
     tan-forward active state rather than a filled pill. */
  .is-on { color: var(--bb-tan-pale); background: rgba(201,168,124,0.10); border-color: rgba(201,168,124,0.50); }
  .bb-chip:disabled { cursor: not-allowed; opacity: 0.55; }
</style>
