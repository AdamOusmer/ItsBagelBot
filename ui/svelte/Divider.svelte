<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  // Svelte adapter for `.bb-divider` (../styles/elements/layout.css).
  // Astro twin: ../astro/Divider.astro.
  //
  // An <hr> for the horizontal case and a <span role="separator"> for the
  // vertical one. `<hr>` is semantically a THEMATIC BREAK in flow content; a
  // hairline between two buttons in a toolbar is not a thematic break, and a
  // screen reader announcing one there is noise. The vertical form is
  // decorative chrome and says so.
  import '../styles/elements/layout.css';

  let {
    vertical = false,
    flush = false,
    fade = false,
    class: className = '',
    ...rest
  }: {
    vertical?: boolean;
    /** Drops the surrounding margin. */
    flush?: boolean;
    /** Hairline that fades out at both ends, for use inside a framed card. */
    fade?: boolean;
    class?: string;
    [key: string]: unknown;
  } = $props();

  const classes = $derived(
    [
      'bb-divider',
      vertical ? 'bb-divider--v' : null,
      flush ? 'bb-divider--flush' : null,
      fade ? 'bb-divider--fade' : null,
      className || null,
    ]
      .filter(Boolean)
      .join(' '),
  );
</script>

{#if vertical}<span class={classes} aria-hidden="true" {...rest}></span>{:else}<hr
    class={classes}
    {...rest}
  />{/if}
