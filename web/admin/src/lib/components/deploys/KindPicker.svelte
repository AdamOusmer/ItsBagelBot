<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import Icon from '@bagel/ui/svelte/Icon.svelte';
  import { getI18n } from '@bagel/kit/i18n/context';
  import { RUN_KINDS, STAGES_FOR } from '$lib/deploys/types';
  import type { RunKind } from '$lib/deploys/types';
  import { KIND_HINT_KEY, KIND_KEY } from './view';

  let { kind, onpick }: { kind: RunKind; onpick: (k: RunKind) => void } = $props();

  const { t } = getI18n();
</script>

<div class="kinds" role="group" aria-label={t('admin.deploys.kindLabel')}>
  {#each RUN_KINDS as k, i (k)}
    <button
      type="button"
      class="kind"
      class:selected={kind === k}
      aria-pressed={kind === k}
      style="--side: {i % 2 ? 1 : -1};"
      onclick={() => onpick(k)}
    >
      <span class="top">
        <span class="idx" aria-hidden="true">{String(i + 1).padStart(2, '0')}</span>
        <span class="tick" aria-hidden="true"><Icon name="check" size={12} /></span>
      </span>
      <span class="name">{t(KIND_KEY[k])}</span>
      <span class="hint">{t(KIND_HINT_KEY[k])}</span>
      <span class="stages">{t('admin.deploys.flow.stagesCount', { n: String(STAGES_FOR[k].length) })}</span>
    </button>
  {/each}
</div>

<style>
  .kinds {
    display: grid;
    grid-template-columns: repeat(5, minmax(0, 1fr));
    gap: 10px;
    width: 100%;
  }
  .kind {
    position: relative;
    isolation: isolate;
    display: grid;
    grid-template-rows: auto auto 1fr auto;
    gap: 8px;
    min-height: 196px;
    padding: 14px;
    overflow: hidden;
    text-align: left;
    background: rgba(255, 255, 255, 0.03);
    color: var(--bb-white);
    border: 1px solid var(--bb-border);
    border-radius: var(--bb-radius-md);
    box-shadow: inset 0 1px 0 rgba(255, 255, 255, 0.05);
    cursor: pointer;
    transition:
      border-color 240ms ease,
      box-shadow 420ms var(--bb-ease-out-expo);
  }
  .kind::before {
    content: '';
    position: absolute;
    inset: 0;
    z-index: -1;
    background: linear-gradient(100deg, rgba(var(--bb-tan-rgb), 0.16), rgba(var(--bb-green-glow-rgb), 0.08));
    transform: translateX(calc(var(--side) * 101%));
    transition: transform 620ms var(--bb-ease-out-expo);
  }
  .kind.selected::before {
    transform: none;
  }
  .kind:hover,
  .kind.selected {
    border-color: var(--bb-tan);
  }
  .kind.selected {
    box-shadow:
      inset 0 1px 0 rgba(255, 255, 255, 0.07),
      0 0 0 1px rgba(var(--bb-tan-rgb), 0.35),
      0 14px 32px rgba(0, 0, 0, 0.22);
  }
  .kind:focus-visible {
    outline: 2px solid var(--bb-tan);
    outline-offset: 2px;
  }
  .top {
    display: flex;
    align-items: center;
    justify-content: space-between;
  }
  .idx {
    font-family: var(--bb-font-mono);
    font-size: 11px;
    letter-spacing: 0.1em;
    color: var(--bb-tan);
    transition: transform 520ms var(--bb-ease-out-expo);
  }
  .kind:hover .idx {
    transform: translateX(3px);
  }
  .tick {
    display: inline-grid;
    place-items: center;
    width: 18px;
    height: 18px;
    border-radius: 50%;
    border: 1px solid var(--bb-border-strong);
    color: var(--bb-bg-0, #101611);
    transition:
      background 240ms ease,
      border-color 240ms ease;
  }
  .tick :global(svg) {
    transform: scale(0);
    transition: transform 420ms var(--bb-ease-out-expo);
  }
  .kind.selected .tick {
    background: var(--bb-green-glow);
    border-color: var(--bb-green-glow);
  }
  .kind.selected .tick :global(svg) {
    transform: scale(1);
  }
  .name {
    font: 700 15px/1.2 var(--bb-font-display);
  }
  .hint {
    font: 12px/1.45 var(--bb-font-body);
    color: var(--bb-muted);
  }
  .stages {
    font-family: var(--bb-font-mono);
    font-size: 10.5px;
    letter-spacing: 0.08em;
    text-transform: uppercase;
    color: var(--bb-muted);
  }
  .kind.selected .stages {
    color: var(--bb-tan-pale);
  }

  @media (max-width: 980px) {
    .kinds {
      grid-template-columns: repeat(5, minmax(168px, 1fr));
      overflow-x: auto;
      scroll-snap-type: x mandatory;
      padding-bottom: 8px;
    }
    .kind {
      scroll-snap-align: start;
    }
  }

  @media (prefers-reduced-motion: reduce) {
    .kind,
    .kind::before,
    .idx,
    .tick :global(svg) {
      transition: none;
    }
  }
</style>
