<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import '../styles/elements/nav.css';

  let {
    label,
    closeLabel,
    controls = 'bb-mobile-menu',
    class: className = '',
    ...rest
  }: {
    label: string;
    closeLabel?: string;
    controls?: string;
    class?: string;
    [key: string]: unknown;
  } = $props();

  const classes = $derived(['bb-hamburger', className || null].filter(Boolean).join(' '));
</script>

<button
  class={classes}
  type="button"
  data-menu-toggle=""
  aria-label={label}
  aria-expanded="false"
  aria-controls={controls}
  data-label-close={closeLabel}
  {...rest}
  ><span class="bb-hamburger__bar"></span><span class="bb-hamburger__bar"></span><span
    class="bb-hamburger__bar"
  ></span></button
>
