<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import { untrack } from 'svelte';
  import { getI18n } from '@bagel/kit/i18n/context';
  import Card from '@bagel/ui/svelte/Card.svelte';
  import Heading from '@bagel/ui/svelte/Heading.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import '@bagel/ui/styles/elements/dot-canvas.css';

  let { src }: { src: string } = $props();

  const { t } = getI18n();
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

<Card as="section" class="scene" aria-labelledby="powered-preview">
  <div class="scene-frame">
    <div class="scene-label">
      <Heading level={6} as="h2" id="powered-preview">
        <bb-fit><span data-fit="">{t('overlays.previewFrame')}</span></bb-fit>
      </Heading>
      <Text as="span" size="xs" tone="muted" mono>1920 × 1080</Text>
    </div>
    <div class="stage bb-dot-canvas" bind:clientWidth={width}>
      <iframe bind:this={frame} class="frame" class:ready title={t('overlays.previewFrame')}
        {src} loading="lazy" referrerpolicy="no-referrer"
        sandbox="allow-scripts allow-same-origin" tabindex="-1"></iframe>
    </div>
  </div>
</Card>

<style>
  .scene-frame {
    --scene-head-h: 1.5rem;
    --scene-gap: var(--bb-space-3);
    display: grid;
    grid-template-rows: var(--scene-head-h) auto;
    gap: var(--scene-gap);
  }
  .scene-label {
    display: grid;
    grid-template-columns: minmax(0, 1fr) auto;
    align-items: center;
    gap: var(--bb-space-3);
  }
  .stage {
    position: relative;
    justify-self: center;
    inline-size: 100%;
    aspect-ratio: 16 / 9;
    overflow: hidden;
    border-radius: var(--bb-radius-sm);
    background-color: color-mix(in srgb, var(--bb-gray-4-dark) 55%, var(--bb-gray-5-dark));
    box-shadow: inset 0 0 0 1px rgba(var(--bb-white-rgb), 0.08), 0 16px 48px rgba(var(--bb-shadow-rgb), 0.55);
  }
  @media (min-width: 1024px) {
    :global(.scene) { container-type: size; }
    .stage { inline-size: min(100cqi, (100cqb - var(--scene-head-h) - var(--scene-gap)) * 16 / 9); }
  }
  .frame {
    position: absolute;
    inset-block-start: 0;
    inset-inline-start: 0;
    inline-size: 100%;
    block-size: 100%;
    border: 0;
    color-scheme: light;
    pointer-events: none;
    opacity: 0;
  }
  .frame.ready { opacity: 1; }
  @media (prefers-reduced-motion: no-preference) {
    .frame { transition: opacity var(--bb-dur-base) var(--bb-ease-out-expo); }
  }
</style>
