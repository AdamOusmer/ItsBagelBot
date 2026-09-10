<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  // Svelte adapter for `.bb-check` (../styles/elements/input.css).
  // Astro twin: ../astro/Checkbox.astro.
  //
  // THE NATIVE INPUT STAYS IN THE DOM, visually hidden, with a drawn <span>
  // beside it. Not `display:none` (removes it from the a11y tree and, in some
  // engines, from form submission) and not a `role="checkbox"` <button> (which
  // posts nothing and has to reimplement the space key). Checked and focus are
  // then pure CSS off the real control's state, so this adapter carries no
  // event handling at all beyond the binding.
  //
  // Sibling order is contract: `.bb-check__input:checked + .bb-check__box`
  // needs the box to be the input's NEXT sibling, so the label text comes
  // third and never between them.
  import '../styles/elements/input.css';
  import type { Snippet } from 'svelte';

  let {
    checked = $bindable(false),
    class: className = '',
    children,
    ...rest
  }: {
    checked?: boolean;
    class?: string;
    children: Snippet;
    [key: string]: unknown;
  } = $props();

  const classes = $derived(['bb-check', className || null].filter(Boolean).join(' '));
</script>

<label class={classes}><input type="checkbox" class="bb-check__input" bind:checked {...rest} /><span
    class="bb-check__box"
    aria-hidden="true"
  ></span><span class="bb-check__label">{@render children()}</span></label>
