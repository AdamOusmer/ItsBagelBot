<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import { untrack, type Snippet } from 'svelte';
  import { getI18n } from '@bagel/kit/i18n/context';
  import Card from '@bagel/ui/svelte/Card.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import '@bagel/ui/styles/elements/dot-canvas.css';

  let { src, describedBy, head, status }: { src: string; describedBy: string; head: Snippet; status: Snippet } = $props();

  const { t } = getI18n();
  const STAGE_WIDTH = 1920;
  let width = $state(0);
  let loadedSrc = $state('');
  const ready = $derived(width > 0 && loadedSrc === src);
  let frame = $state<HTMLIFrameElement>();

  $effect(() => {
    const el = frame;
    if (!el) return;
    const loaded = () => (loadedSrc = el.getAttribute('src') ?? '');
    el.addEventListener('load', loaded);
    el.src = untrack(() => src);
    return () => el.removeEventListener('load', loaded);
  });
</script>

<Card class="scene">
  <div class="scene-frame">
    <div class="scene-label">
      <div class="scene-name">{@render head()}</div>
      <div class="scene-meta">
        {@render status()}
        <Text as="span" size="xs" tone="muted" mono>1920 × 1080</Text>
      </div>
    </div>
    <div class="stage bb-dot-canvas" bind:clientWidth={width}>
      <iframe bind:this={frame} class="frame" class:ready title={t('overlays.previewFrame')} aria-describedby={describedBy}
        {src} style:transform="scale({width / STAGE_WIDTH})" loading="lazy" referrerpolicy="no-referrer"
        sandbox="allow-scripts allow-same-origin" tabindex="-1"></iframe>
    </div>
  </div>
</Card>

<style>
  .scene-frame {
    display: grid;
    grid-template-rows: var(--scene-head-h, 22px) auto;
    gap: var(--scene-gap, var(--bb-space-3));
  }
  .scene-label {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--bb-space-3);
    min-inline-size: 0;
  }
  .scene-name {
    display: flex;
    flex: 1;
    align-items: baseline;
    gap: var(--bb-space-3);
    min-inline-size: 0;
  }
  .scene-meta {
    display: flex;
    flex: none;
    align-items: center;
    gap: var(--bb-space-3);
  }
  .stage {
    position: relative;
    justify-self: center;
    inline-size: var(--stage-max-inline, 100%);
    max-inline-size: 100%;
    aspect-ratio: 16 / 9;
    overflow: hidden;
    border-radius: var(--bb-radius-md);
    background-color: color-mix(in srgb, var(--bb-gray-4-dark) 55%, var(--bb-gray-5-dark));
    box-shadow: inset 0 0 0 1px rgba(var(--bb-white-rgb), 0.08), 0 16px 48px rgba(var(--bb-shadow-rgb), 0.55);
  }
  @media (max-width: 1023px) {
    .scene-frame { grid-template-rows: auto auto; }
    .scene-label { align-items: start; }
    .scene-name { flex-wrap: wrap; row-gap: var(--bb-space-1); }
  }
  .frame {
    position: absolute;
    inset-block-start: 0;
    inset-inline-start: 0;
    inline-size: 1920px;
    block-size: 1080px;
    border: 0;
    color-scheme: light;
    transform-origin: 0 0;
    pointer-events: none;
    opacity: 0;
  }
  .frame.ready { opacity: 1; }
  @media (prefers-reduced-motion: no-preference) {
    .frame { transition: opacity var(--bb-dur-base) var(--bb-ease-out-expo); }
  }
</style>
