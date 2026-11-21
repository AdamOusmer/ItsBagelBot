<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import { untrack } from 'svelte';
  import { getI18n } from '@bagel/kit/i18n/context';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import '@bagel/ui/styles/elements/dot-canvas.css';

  let { src }: { src: string } = $props();

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

<div class="scene">
  <figure class="scene-frame">
    <figcaption class="scene-label">
      <span class="scene-name bb-fit-box" id="powered-preview">
        <Text as="span" size="xs" tone="muted"><bb-fit>{t('overlays.preview')}</bb-fit></Text>
      </span>
      <Text as="span" size="xs" tone="muted" mono>1920 × 1080</Text>
    </figcaption>
    <div class="stage bb-dot-canvas" bind:clientWidth={width}>
      <iframe bind:this={frame} class="frame" class:ready title={t('overlays.previewFrame')} aria-describedby="powered-preview"
        {src} style:transform="scale({width / STAGE_WIDTH})" loading="lazy" referrerpolicy="no-referrer"
        sandbox="allow-scripts allow-same-origin" tabindex="-1"></iframe>
    </div>
  </figure>
</div>

<style>
  .scene {
    position: absolute;
    inset-block: calc(var(--stage-top, 0px) + 16px) var(--overlays-scene-bottom, 16px);
    inset-inline: 16px calc(var(--editor-panel-right, 360px) + 32px);
    display: grid;
    place-items: center;
    container-type: size;
  }
  .scene-frame {
    display: grid;
    grid-template-rows: 20px auto;
    gap: var(--bb-space-2);
    inline-size: min(100cqw, (100cqh - 28px) * 16 / 9);
    margin: 0;
  }
  .scene-label {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--bb-space-3);
    min-inline-size: 0;
  }
  .scene-name { flex: 1; min-inline-size: 0; }
  .stage {
    position: relative;
    aspect-ratio: 16 / 9;
    overflow: hidden;
    border-radius: var(--bb-radius-sm);
    box-shadow: inset 0 0 0 1px rgba(var(--bb-white-rgb), 0.08), 0 16px 48px rgba(var(--bb-shadow-rgb), 0.55);
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
  @media (max-width: 760px), (pointer: coarse) and (max-width: 1023px) {
    .scene { inset-inline: 16px; }
  }
  @media (prefers-reduced-motion: no-preference) {
    .frame { transition: opacity var(--bb-dur-base) var(--bb-ease-out-expo); }
  }
</style>
