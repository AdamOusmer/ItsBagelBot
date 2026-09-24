<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  let {
    labels,
    current,
    maxStep,
    label,
    onselect
  }: {
    labels: string[];
    current: number;
    maxStep: number;
    label: string;
    onselect: (i: number) => void;
  } = $props();
</script>

<nav
  class="rail"
  aria-label={label}
  style="--i: {Math.max(current, 0)}; --n: {Math.max(labels.length, 1)};"
>
  <span class="track" class:idle={current < 0} aria-hidden="true"><span class="trail"></span></span>
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
      <span class="num">{String(i + 1).padStart(2, '0')}</span>
      <span class="pip" aria-hidden="true"></span>
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
    position: relative;
    display: grid;
    place-items: center;
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
  .idx:not(:disabled):hover { color: var(--bb-white); }
  .idx:focus-visible {
    outline: 2px solid var(--bb-tan);
    outline-offset: -2px;
    border-radius: var(--bb-radius-xs, 4px);
  }

  .idx.seen { color: var(--bb-tan); }

  .idx.on { color: var(--bb-tan-pale); }

  .pip {
    position: absolute;
    left: 50%;
    bottom: -2px;
    width: 5px;
    height: 5px;
    margin-left: -2.5px;
    border-radius: 50%;
    border: 1px solid var(--bb-border-strong);
    background: var(--bb-bg-0, #101611);
    transform: scale(0.8);
    transition:
      transform 600ms var(--bb-ease-out-expo),
      background 400ms var(--bb-ease-out-expo),
      border-color 400ms var(--bb-ease-out-expo),
      box-shadow 400ms var(--bb-ease-out-expo);
  }
  .idx.seen .pip {
    border-color: var(--bb-tan);
    background: var(--bb-tan);
  }
  .idx.on .pip {
    border-color: var(--bb-green-glow);
    background: var(--bb-green-glow);
    transform: scale(1.25);
    box-shadow: 0 0 0 3px rgba(var(--bb-green-glow-rgb), 0.18), 0 0 12px rgba(var(--bb-green-glow-rgb), 0.55);
  }

  .track {
    position: absolute;
    left: calc(var(--cell) / 2);
    right: calc(var(--cell) / 2);
    bottom: 0;
    height: 1px;
    background: var(--bb-border);
    pointer-events: none;
  }
  .trail {
    position: absolute;
    inset: 0;
    transform-origin: left;
    transform: scaleX(calc(var(--i) / max(var(--n) - 1, 1)));
    background: linear-gradient(90deg, var(--bb-tan), var(--bb-green-glow));
    transition:
      transform 900ms var(--bb-ease-out-expo),
      opacity 300ms var(--bb-ease-out-expo);
  }
  .track.idle .trail { opacity: 0; }

  .idx:disabled {
    cursor: not-allowed;
    opacity: 0.35;
  }

  .glide {
    position: absolute;
    left: 0;
    top: 4px;
    width: var(--cell);
    height: 28px;
    border-radius: var(--bb-radius-sm);
    background: radial-gradient(closest-side, rgba(var(--bb-tan-rgb), 0.16), transparent);
    pointer-events: none;
    transform: translateX(calc(var(--i) * var(--cell)));
    transition:
      transform 700ms var(--bb-ease-out-expo),
      opacity var(--bb-dur-base) var(--bb-ease-out-expo);
  }

  .glide.hidden { opacity: 0; }

  @media (max-width: 760px) {
    .rail { --cell: clamp(25px, 8vw, 34px); }
  }

  @media (prefers-reduced-motion: reduce) {
    .idx,
    .pip,
    .trail,
    .glide { transition: none; }
  }
</style>
