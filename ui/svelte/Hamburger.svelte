<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  // Svelte adapter for `.bb-hamburger`. Its Astro twin is
  // ../astro/Hamburger.astro; ../test/parity.test.ts diffs the two.
  //
  // Renders CLOSED and says so: aria-expanded="false" and the open label. The
  // panel engine (../lib/nav-menu.ts) owns every state change from there, and
  // it finds this button by `data-menu-toggle` rather than by a class, so a
  // caller may restyle the button without unhooking it.
  import '../styles/elements/nav.css';

  let {
    label,
    closeLabel,
    controls = 'bb-mobile-menu',
    class: className = '',
    ...rest
  }: {
    /** aria-label in the closed state. The engine swaps it on open. */
    label: string;
    /** aria-label while open. Rides on the element rather than reaching the
        engine as a second prop, so a surface that renders this button itself
        cannot supply one half of the pair. */
    closeLabel?: string;
    /** id of the panel this opens. */
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
