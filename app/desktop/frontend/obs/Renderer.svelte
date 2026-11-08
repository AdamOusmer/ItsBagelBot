<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { onMount } from 'svelte';
  import CanvasCard from '@bagel/ui/svelte/CanvasCard.svelte';
  import LayerContent from '../src/screens/shell/overlay/LayerContent.svelte';
  import type { OverlayScene } from '../src/lib/overlay/types';
  import { RESOLUTIONS } from '../src/lib/overlay/resolutions';

  let scene = $state<OverlayScene>({ resolution: '1080p', layers: [] });
  let width = $state(window.innerWidth);
  let height = $state(window.innerHeight);
  const size = $derived(RESOLUTIONS[scene.resolution]);
  const scale = $derived(Math.min(width / size.width, height / size.height));

  onMount(() => {
    const controller = new AbortController();
    // Fragment credentials never enter HTTP URLs, referrers, or server access logs.
    const token = location.hash.slice(1);
    let revision = 0;
    async function watch(): Promise<void> {
      while (!controller.signal.aborted) {
        try {
          const response = await fetch(`/state?after=${revision}`, {
            headers: { Authorization: `Bearer ${token}` }, signal: controller.signal,
          });
          if (response.status === 401 || response.status === 403) { scene = { ...scene, layers: [] }; return; }
          if (!response.ok) throw new Error('Overlay unavailable');
          const next = await response.json();
          if (next.revision !== revision) { scene = next.scene; revision = next.revision; }
        } catch {
          if (!controller.signal.aborted) await new Promise((resolve) => setTimeout(resolve, 2000));
        }
      }
    }
    void watch();
    return () => controller.abort();
  });
</script>

<svelte:window bind:innerWidth={width} bind:innerHeight={height} />
<div class="canvas" style:width="{size.width}px" style:height="{size.height}px" style:transform="scale({scale})" style:--canvas-scale={size.height / 360}>
  {#each scene.layers as layer (layer.id)}
    <CanvasCard class="layer" background={layer.kind !== 'text'} style="position:absolute;left:{layer.x}%;top:{layer.y}%;width:{layer.w}%;height:{layer.h}%;opacity:{layer.opacity / 100}">
      <LayerContent {layer} canvasH={size.height} />
    </CanvasCard>
  {/each}
</div>
<style>
  .canvas { position: relative; transform-origin: top left; }
  .canvas :global(.layer) { overflow: hidden; pointer-events: none; }
</style>
