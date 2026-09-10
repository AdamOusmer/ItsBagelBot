<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  // Svelte adapter for a text input inside the `.bb-input` frame
  // (../styles/elements/field.css for the frame, ../styles/elements/input.css
  // for the states). Astro twin: ../astro/Input.astro.
  //
  // THE FRAME IS A WRAPPER ELEMENT, not a class on the <input>. That is the
  // contract as it was extracted from the console's `.search`: the border,
  // the padding and the focus ring are on the wrapper, which is what lets a
  // field put an icon or a unit suffix beside the control without the icon
  // sitting outside the box. `:focus-within` on the wrapper is what makes the
  // frame light up when the inner control takes focus.
  //
  // `value` is bindable so the common case (`bind:value`) works; everything
  // else -- name, required, autocomplete, aria-describedby -- passes through
  // to the <input> via ...rest rather than being re-declared here, because
  // this element has no opinion about any of them.
  import '../styles/elements/field.css';
  import '../styles/elements/input.css';
  import type { Snippet } from 'svelte';

  let {
    value = $bindable(''),
    type = 'text',
    invalid = false,
    fill = false,
    mono = false,
    class: className = '',
    icon,
    trail,
    ...rest
  }: {
    value?: string;
    type?: 'text' | 'email' | 'url' | 'tel' | 'number' | 'password' | 'search';
    /** Draws the error frame. Set when the value has been JUDGED wrong. */
    invalid?: boolean;
    /** Take the container's width instead of the 240px default. */
    fill?: boolean;
    mono?: boolean;
    class?: string;
    icon?: Snippet;
    trail?: Snippet;
    [key: string]: unknown;
  } = $props();

  const classes = $derived(
    ['bb-input', fill ? 'bb-input--fill' : null, mono ? 'bb-input--mono' : null, className || null]
      .filter(Boolean)
      .join(' '),
  );
</script>

<span class={classes} data-invalid={invalid ? '' : undefined}>{#if icon}{@render icon()}{/if}<input
    {type}
    bind:value
    {...rest}
  />{#if trail}{@render trail()}{/if}</span>
