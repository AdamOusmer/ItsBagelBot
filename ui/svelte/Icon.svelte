<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  // Svelte adapter for the icon contract. Its Astro twin is ../astro/Icon.astro
  // and ../test/parity.test.ts diffs the two, so the attribute list and its
  // order below are the contract.
  //
  // Two components drew these glyphs before this one: the console's had
  // `size = 18` and a `fill` prop while hard-coding stroke-width, and the
  // site's had `size = 16` and a `strokeWidth` prop while hard-coding fill.
  // Same set, same viewBox, different defaults and different knobs -- so the
  // same `<Icon name="check" />` rendered 18px on one surface and 16px on the
  // other. Unified (user decision, 2026-09-09): the default is 16 and BOTH
  // props stay. 16 rather than 18 because it is the size the smaller surfaces
  // ask for unprompted (inline marks, buttons, chips) while the console's
  // 18px spots are a countable list that now says `size={18}` out loud.
  import '../styles/elements/icon.css';
  import { icons, type IconName } from '../lib/icons';

  let {
    name,
    size = 16,
    strokeWidth = 1.6,
    fill = 'none',
    class: className = '',
    ...rest
  }: {
    /** A key of the generated set in ../lib/icons.ts. */
    name: IconName;
    /** Square edge in px. Written to width AND height; the viewBox is 24. */
    size?: number;
    /** Stroke weight for the Lucide bodies. Brand marks ignore it. */
    strokeWidth?: number;
    /** `none` for the stroke set. Brand marks carry their own fill. */
    fill?: string;
    class?: string;
    [key: string]: unknown;
  } = $props();

  const classes = $derived(['bb-icon', className || null].filter(Boolean).join(' '));
</script>

<!-- Presentation attributes rather than CSS, deliberately: an icon dropped
     into a surface with no svg styling still draws, instead of rendering an
     invisible black-on-dark glyph. CSS from the caller beats every one of
     them. `{@html}` is safe here: the bodies are generated design constants
     from ui/scripts/gen-icons.mjs, never user input. -->
<svg
  class={classes}
  viewBox="0 0 24 24"
  width={size}
  height={size}
  {fill}
  stroke="currentColor"
  stroke-width={strokeWidth}
  stroke-linecap="round"
  stroke-linejoin="round"
  aria-hidden="true"
  {...rest}>{@html icons[name]}</svg
>
