<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

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
    active?: boolean;
    external?: boolean;
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
