<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  // Svelte adapter for `.bb-btn--icon` (../styles/elements/button.css).
  // Astro twin: ../astro/IconButton.astro.
  //
  // A separate element and not `<Button variant="icon">`, because the ONE
  // thing that goes wrong with icon-only controls is the missing accessible
  // name, and Button can only warn about it after the fact. Here `label` is a
  // REQUIRED prop: an icon button without a name does not type-check, which
  // moves the failure from a console warning nobody reads to the editor.
  //
  // The label becomes `aria-label` and, when `tooltip` is set, is also shown
  // on hover through the tooltip contract. It is never rendered as visible
  // text -- that is what Button with children is for.
  //
  // Descended from web/kit/components/MiniButton.svelte, which was the
  // console's local spelling of the same thing.
  import '../styles/elements/button.css';
  import '../styles/elements/tooltip.css';
  import type { Snippet } from 'svelte';

  let {
    label,
    tooltip = false,
    size = 'md',
    type = 'button',
    onclick,
    disabled = false,
    class: className = '',
    children,
    ...rest
  }: {
    /** The accessible name. Required: an icon carries none of its own. */
    label: string;
    /** Also show `label` as a hover hint. */
    tooltip?: boolean;
    size?: 'md' | 'sm';
    type?: 'button' | 'submit' | 'reset';
    onclick?: (e: MouseEvent) => void;
    disabled?: boolean;
    class?: string;
    children: Snippet;
    [key: string]: unknown;
  } = $props();

  const classes = $derived(
    ['bb-btn', 'bb-btn--icon', size === 'sm' ? 'bb-btn--sm' : null, className || null]
      .filter(Boolean)
      .join(' '),
  );
</script>

{#if tooltip}<span class="bb-tooltip"><button
      class={classes}
      {type}
      {disabled}
      aria-label={label}
      data-mark=""
      {onclick}
      {...rest}><span class="bb-btn__content">{@render children()}</span></button
    ><span class="bb-tooltip__bubble" aria-hidden="true">{label}</span></span
  >{:else}<button
    class={classes}
    {type}
    {disabled}
    aria-label={label}
    data-mark=""
    {onclick}
    {...rest}><span class="bb-btn__content">{@render children()}</span></button
  >{/if}
