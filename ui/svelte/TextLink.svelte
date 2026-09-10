<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  // Svelte adapter for the `.bb-text-link` contract
  // (../styles/elements/text-link.css). Astro twin: ../astro/TextLink.astro,
  // diffed against this file by ../test/parity.test.ts — which matters more
  // here than anywhere else in the package, because this element emits one
  // <span> per glyph, twice, each carrying its index.
  //
  // `Array.from(label)` and not `label.split('')`: split breaks surrogate
  // pairs, so a character composed of two code units renders as two broken
  // glyphs with two different delays.
  import '../styles/elements/text-link.css';

  let {
    href,
    label,
    active = false,
    external = false,
    size,
    class: className = '',
    ...rest
  }: {
    href: string;
    label: string;
    /** Persistent lit state for the current route. */
    active?: boolean;
    /** Opens in a new tab, with the rel that makes that safe. */
    external?: boolean;
    /** Font-size override, any CSS length. The roll geometry is in em. */
    size?: string;
    class?: string;
    [key: string]: unknown;
  } = $props();

  const glyphs = $derived(Array.from(label));
  const classes = $derived(
    ['bb-text-link', className || null, active ? 'is-active' : null].filter(Boolean).join(' '),
  );
</script>

<a
  class={classes}
  {href}
  aria-label={label}
  aria-current={active ? 'page' : undefined}
  target={external ? '_blank' : undefined}
  rel={external ? 'noopener noreferrer' : undefined}
  style={size ? `--text-link-size: ${size};` : undefined}
  {...rest}
  ><span class="bb-text-link__mask" aria-hidden="true"><span
      class="bb-text-link__row bb-text-link__row--rest"
    >{#each glyphs as glyph, index}<span class="bb-text-link__glyph" style="--gi: {index};"
        >{glyph}</span
      >{/each}</span
    ><span class="bb-text-link__row bb-text-link__row--over"
    >{#each glyphs as glyph, index}<span class="bb-text-link__glyph" style="--gi: {index};"
        >{glyph}</span
      >{/each}</span
    ></span
  ></a
>
