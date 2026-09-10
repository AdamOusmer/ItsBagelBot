<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  // Svelte adapter for the `.bb-h` contract (../styles/elements/typography.css).
  // Astro twin: ../astro/Heading.astro; ../test/parity.test.ts diffs the two.
  //
  // LEVEL AND VARIANT ARE TWO PROPS. `level` is the document outline -- the
  // rank a screen reader navigates by. `variant` is the treatment. A card
  // title is very often an <h3> by outline and `card` by treatment; a hero is
  // an <h1> by outline and `display` by treatment. Folding them into one prop
  // is what makes people choose the wrong tag to get the right size, which is
  // the most common way a heading outline gets broken. The contract file
  // carries the same record.
  //
  // `as` overrides the TAG only, never the classes: the case it exists for is
  // a heading that must not enter the outline at all (a <p> or a <div> styled
  // as display type, e.g. the second line of a two-line hero), which is an
  // accessibility decision the caller has to make explicitly.
  import '../styles/elements/typography.css';
  import type { Snippet } from 'svelte';

  let {
    level = 2,
    variant,
    as: tag,
    class: className = '',
    children,
    ...rest
  }: {
    /** Outline rank. Picks the tag and, with no `variant`, the size. */
    level?: 1 | 2 | 3 | 4 | 5 | 6;
    /** Type treatment. Overrides the level's size, never its tag. */
    variant?: 'display' | 'section' | 'card' | 'eyebrow';
    /** Tag override, for type that must stay out of the heading outline. */
    as?: string;
    class?: string;
    children: Snippet;
    [key: string]: unknown;
  } = $props();

  const element = $derived(tag ?? `h${level}`);
  const classes = $derived(
    ['bb-h', `bb-h--l${level}`, variant ? `bb-h--${variant}` : null, className || null]
      .filter(Boolean)
      .join(' '),
  );
</script>

<svelte:element this={element} class={classes} {...rest}>{@render children()}</svelte:element>
