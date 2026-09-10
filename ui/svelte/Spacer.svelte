<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  // Svelte adapter for `.bb-spacer` (../styles/elements/layout.css).
  // Astro twin: ../astro/Spacer.astro.
  //
  // NOT for spacing stack children -- that is `Stack gap`. This exists for the
  // two cases flexbox has no other answer to: an explicit block of air in
  // flowed content, and `grow`, which pushes everything after it to the far
  // end of a flex row (the toolbar shape, where `Cluster justify="between"`
  // cannot reach because there are three children and the split is after the
  // first).
  //
  // `aria-hidden` is unconditional: an empty box is already skipped by
  // assistive tech, but saying so keeps it skipped if it ever grows a border.
  import '../styles/elements/layout.css';

  let {
    size = 4,
    grow = false,
    class: className = '',
    ...rest
  }: {
    /** Step on the --bb-space ramp, not a length. */
    size?: 1 | 2 | 3 | 4 | 5 | 6 | 7 | 8;
    /** Absorbs the free space in a flex row instead of taking a fixed size. */
    grow?: boolean;
    class?: string;
    [key: string]: unknown;
  } = $props();

  const classes = $derived(
    [
      'bb-spacer',
      grow ? 'bb-spacer--grow' : `bb-spacer--${size}`,
      className || null,
    ]
      .filter(Boolean)
      .join(' '),
  );
</script>

<span class={classes} aria-hidden="true" {...rest}></span>
