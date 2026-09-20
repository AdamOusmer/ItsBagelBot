<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  // The tour's index: one numeral per step and a single bar that glides to
  // whichever is current. One bar rather than an underline per numeral for
  // the same reason the app rail has one highlight (ui/lib/rail-glide.ts): a
  // per-item style cannot animate BETWEEN two elements, so the marker would
  // blink from one step to the next instead of travelling. Every cell is the
  // same fixed width, which is what lets the bar's position be a plain
  // multiple of the index and skip measuring altogether.
  let {
    labels,
    current,
    maxStep,
    label,
    onselect
  }: {
    /** One accessible name per step, in order. */
    labels: string[];
    /** Index of the current step, or -1 while the intro is up. */
    current: number;
    /** Furthest step reachable right now (the consent gate decides). */
    maxStep: number;
    /** Accessible name of the whole rail. */
    label: string;
    onselect: (i: number) => void;
  } = $props();
</script>

<nav class="rail" aria-label={label} style="--i: {Math.max(current, 0)};">
  {#each labels as text, i (i)}
    <button
      type="button"
      class="idx"
      class:on={i === current}
      class:seen={i < current}
      aria-label={text}
      aria-current={i === current ? 'step' : undefined}
      disabled={i > maxStep}
      onclick={() => onselect(i)}
    >
      {String(i + 1).padStart(2, '0')}
    </button>
  {/each}
  <span class="glide" class:hidden={current < 0} aria-hidden="true"></span>
</nav>

<style>
  .rail {
    --cell: 44px;
    position: relative;
    display: flex;
  }

  .idx {
    width: var(--cell);
    height: 36px;
    padding: 0;
    border: 0;
    background: none;
    font-family: var(--bb-font-mono);
    font-size: 11px;
    letter-spacing: 0.1em;
    color: var(--bb-muted);
    cursor: pointer;
    transition: color var(--bb-dur-base) var(--bb-ease-out-expo);
  }

  .idx.seen { color: var(--bb-tan); }

  .idx.on { color: var(--bb-tan-pale); }

  /* A step the consent gate has not opened yet: visibly not a target, rather
     than a numeral that looks clickable and refuses. */
  .idx:disabled {
    cursor: not-allowed;
    opacity: 0.35;
  }

  .glide {
    position: absolute;
    left: 0;
    bottom: 0;
    width: var(--cell);
    height: 2px;
    border-radius: 1px;
    background: linear-gradient(90deg, var(--bb-tan), var(--bb-green-glow));
    transform: translateX(calc(var(--i) * var(--cell)));
    transition:
      transform 700ms var(--bb-ease-out-expo),
      opacity var(--bb-dur-base) var(--bb-ease-out-expo);
  }

  .glide.hidden { opacity: 0; }

  @media (max-width: 760px) {
    .rail { --cell: 34px; }
  }

  @media (prefers-reduced-motion: reduce) {
    .idx,
    .glide { transition: none; }
  }
</style>
