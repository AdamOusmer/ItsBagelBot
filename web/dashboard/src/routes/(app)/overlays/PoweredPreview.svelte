<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import { getI18n } from '@bagel/kit/i18n/context';
  import Text from '@bagel/ui/svelte/Text.svelte';

  let { src }: { src: string } = $props();

  const { t } = getI18n();
  const STAGE_WIDTH = 1920;
  let width = $state(0);
</script>

<div class="preview">
  <span class="preview-label" id="powered-preview">
    <Text as="span" size="sm"><bb-fit><span data-fit="">{t('overlays.preview')}</span></bb-fit></Text>
  </span>
  <div class="stage" bind:clientWidth={width}>
    <iframe class="frame" class:ready={width > 0} title={t('overlays.previewFrame')} aria-describedby="powered-preview"
      {src} style:transform="scale({width / STAGE_WIDTH})" loading="lazy" referrerpolicy="no-referrer"
      sandbox="allow-scripts allow-same-origin" tabindex="-1"></iframe>
  </div>
</div>

<style>
  .preview {
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    gap: var(--bb-space-2);
    padding: 14px 18px 18px;
  }
  .preview-label { min-inline-size: 0; }
  .stage {
    position: relative;
    inline-size: 100%;
    max-inline-size: 48rem;
    aspect-ratio: 16 / 9;
    overflow: hidden;
    border-radius: var(--bb-radius-sm);
    background: var(--bb-bg-0);
  }
  .frame {
    position: absolute;
    inset-block-start: 0;
    inset-inline-start: 0;
    inline-size: 1920px;
    block-size: 1080px;
    border: 0;
    transform-origin: 0 0;
    pointer-events: none;
    opacity: 0;
  }
  .frame.ready { opacity: 1; }
  @media (prefers-reduced-motion: no-preference) {
    .frame { transition: opacity var(--bb-dur-fast) var(--bb-ease-out-expo); }
  }
</style>
