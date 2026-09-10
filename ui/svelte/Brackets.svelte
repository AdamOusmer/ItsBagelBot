<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  // Svelte adapter for the `.bb-ornaments` contract
  // (../styles/elements/brackets.css). Astro twin: ../astro/Brackets.astro.
  //
  // The two hairline corner brackets and the wordmark framing the bottom of a
  // page. `--ornament-inline` is the inset the social rail aligns its column
  // to, which is why it is a custom property rather than a number written in
  // two files with a comment between them.
  import '../styles/elements/brackets.css';

  let {
    variant = 'page',
    label,
    class: className = '',
    ...rest
  }: {
    /** page = over the content, under the nav; loader = under the loader art. */
    variant?: 'page' | 'loader';
    /** Wordmark under the brackets. Empty renders no box (`:empty` in the contract). */
    label?: string;
    class?: string;
    [key: string]: unknown;
  } = $props();

  const text = $derived(label ?? (variant === 'loader' ? 'ItsBagelBot' : ''));
  const classes = $derived(
    ['bb-ornaments', `bb-ornaments--${variant}`, className || null].filter(Boolean).join(' '),
  );
</script>

<div class={classes} aria-hidden="true" {...rest}><div class="bb-corner bb-corner--bl"></div><div
    class="bb-corner bb-corner--br"
  ></div><div class="bb-ornament-label">{text}</div></div>
