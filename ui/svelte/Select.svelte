<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  // Svelte adapter for a <select> inside the `.bb-input` frame
  // (../styles/elements/input.css). Astro twin: ../astro/Select.astro.
  //
  // The chevron is a real <svg> sibling and not a `background-image` data URI.
  // Two reasons, both recorded at the rule in input.css: the console ships a
  // CSP without `unsafe-inline`, and an SVG in a URL cannot inherit
  // currentColor, so the arrow would stay one grey through hover, focus and
  // disabled.
  //
  // Options are children, not a prop. An `options` array would have to grow a
  // shape for optgroups, for a disabled option and for a placeholder, and the
  // markup for all three is already HTML the caller can write.
  import '../styles/elements/field.css';
  import '../styles/elements/input.css';
  import type { Snippet } from 'svelte';

  let {
    value = $bindable(''),
    invalid = false,
    class: className = '',
    children,
    ...rest
  }: {
    value?: string;
    invalid?: boolean;
    class?: string;
    children: Snippet;
    [key: string]: unknown;
  } = $props();

  const classes = $derived(
    ['bb-input', 'bb-input--select', className || null].filter(Boolean).join(' '),
  );
</script>

<span class={classes} data-invalid={invalid ? '' : undefined}><select bind:value {...rest}
    >{@render children()}</select
  ><svg class="bb-input__chevron" viewBox="0 0 24 24" aria-hidden="true"><path d="m6 9 6 6 6-6"
    ></path></svg
  ></span>
