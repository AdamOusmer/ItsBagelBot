<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  // The public nav's link, with the marketing site's roll-over motion (ported
  // from web/src/components/layout/NavLink.astro): two stacked glyph rows in a
  // mask — the resting row rolls out as the lit row rolls in, glyph by glyph —
  // over an ember rail that sweeps open underneath. Extracted verbatim from
  // routes/user/[channel], which is where this markup first landed.
  let {
    href,
    label,
    active = false
  }: { href: string; label: string; active?: boolean } = $props();

  const glyphs = $derived(Array.from(label));
</script>

<a
  class="nav-link-motion"
  class:is-active={active}
  {href}
  aria-label={label}
  aria-current={active ? 'page' : undefined}
>
  <span class="nav-link-motion__mask" aria-hidden="true">
    <span class="nav-link-motion__line nav-link-motion__line--rest">
      {#each glyphs as glyph, i}
        <span class="nav-link-motion__glyph" style={`--glyph-index: ${i};`}>{glyph}</span>
      {/each}
    </span>
    <span class="nav-link-motion__line nav-link-motion__line--active">
      {#each glyphs as glyph, i}
        <span class="nav-link-motion__glyph" style={`--glyph-index: ${i};`}>{glyph}</span>
      {/each}
    </span>
  </span>
</a>

<style>
  .nav-link-motion {
    --nav-link-shift: 1.22em;
    position: relative;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    min-height: 32px;
    padding: 0.42rem 0.08rem;
    color: var(--bb-muted);
    font-family: var(--bb-font-mono);
    font-size: 0.78rem;
    font-weight: 500;
    line-height: 1;
    text-decoration: none;
    text-transform: uppercase;
    white-space: nowrap;
    outline: none;
    isolation: isolate;
  }
  .nav-link-motion::before {
    content: "";
    position: absolute;
    left: 0; right: 0;
    bottom: 0.28rem;
    height: 1px;
    pointer-events: none;
    background: linear-gradient(90deg, transparent, rgba(224,196,154,0.15), rgba(224,196,154,0.78), rgba(82,183,136,0.54), transparent);
    opacity: 0;
    transform: scaleX(0.18);
    transform-origin: center;
  }
  .nav-link-motion__mask {
    position: relative;
    display: block;
    height: 1.12em;
    overflow: hidden;
    text-shadow: 0 3px 12px rgba(0,0,0,0.9), 0 0 18px rgba(224,196,154,0.18);
  }
  .nav-link-motion__line { display: inline-flex; align-items: baseline; color: inherit; }
  .nav-link-motion__line--active {
    position: absolute;
    inset: 0 auto auto 0;
    color: var(--bb-white);
    text-shadow: 0 3px 12px rgba(0,0,0,0.9), 0 0 18px rgba(224,196,154,0.34), 0 0 26px rgba(82,183,136,0.18);
  }
  .nav-link-motion__glyph { display: inline-block; transform: translateY(0); }
  .nav-link-motion__line--active .nav-link-motion__glyph {
    opacity: 0.4;
    transform: translateY(var(--nav-link-shift)) rotate(3deg);
  }
  .nav-link-motion:focus-visible { outline: 1px solid rgba(224,196,154,0.58); outline-offset: 6px; }

  /* Current route: the rail stays open and the label sits at full brightness,
     the same persistent lit state the marketing TextLink uses. */
  .nav-link-motion.is-active { color: var(--bb-white); }
  .nav-link-motion.is-active::before { opacity: 1; transform: scaleX(1); }

  @media (min-width: 1024px) and (hover: hover) and (pointer: fine) {
    .nav-link-motion { transition: color 180ms ease; }
    .nav-link-motion::before {
      transition: opacity 260ms ease, transform 620ms var(--bb-ease-out-expo);
    }
    .nav-link-motion__glyph {
      transition: opacity 280ms ease, transform 420ms var(--bb-ease-out-expo);
      transition-delay: calc(var(--glyph-index) * 16ms);
      will-change: transform, opacity;
    }
    .nav-link-motion:is(:hover, :focus-visible) { color: var(--bb-white); }
    .nav-link-motion:is(:hover, :focus-visible)::before { opacity: 1; transform: scaleX(1); }
    .nav-link-motion:is(:hover, :focus-visible) .nav-link-motion__line--rest .nav-link-motion__glyph {
      opacity: 0.32;
      transform: translateY(calc(var(--nav-link-shift) * -1)) rotate(-3deg);
    }
    .nav-link-motion:is(:hover, :focus-visible) .nav-link-motion__line--active .nav-link-motion__glyph {
      opacity: 1;
      transform: translateY(0) rotate(0deg);
      transition-delay: calc(72ms + (var(--glyph-index) * 18ms));
    }
  }
</style>
