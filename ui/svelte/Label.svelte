<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  // Svelte adapter for `.bb-label` (../styles/elements/typography.css).
  // Astro twin: ../astro/Label.astro.
  //
  // For a control that is NOT inside a `Field`. Field owns its own label
  // (`.bb-field__label`), which carries the field's 6px gap and its position
  // inside the wrapping <label>; using this one there would double the label.
  //
  // `for` is spelled `htmlFor` in the prop list because `for` is a reserved
  // word in a destructuring pattern, and emitted as `for` on the element.
  import '../styles/elements/typography.css';
  import type { Snippet } from 'svelte';

  let {
    htmlFor,
    class: className = '',
    children,
    ...rest
  }: {
    /** The id of the control this labels. Emitted as `for`. */
    htmlFor?: string;
    class?: string;
    children: Snippet;
    [key: string]: unknown;
  } = $props();

  const classes = $derived(['bb-label', className || null].filter(Boolean).join(' '));
</script>

<label class={classes} for={htmlFor} {...rest}>{@render children()}</label>
