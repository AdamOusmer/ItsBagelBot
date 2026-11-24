<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { OverlayRuntime } from '../../overlay-runtime';
  import type { OverlayAsset, OverlayWidget } from '../../lib/overlay-document';
  import type { EmulationBus } from '../../lib/overlay-editor/emulate';

  let { widget, assets, scale, events }: {
    widget: OverlayWidget;
    assets: Record<string, OverlayAsset>;
    scale: number;
    events?: EmulationBus;
  } = $props();

  let host: HTMLDivElement | undefined = $state();
  let runtime: OverlayRuntime | undefined;

  $effect(() => {
    if (!host) return;
    runtime = new OverlayRuntime(host, { preview: true });
    return () => {
      runtime?.dispose();
      runtime = undefined;
    };
  });

  $effect(() => events?.subscribe((event) => runtime?.event(event)));

  $effect(() => {
    const placed = { ...widget, x: 0, y: 0, rotation: 0, opacity: 1, hidden: false };
    runtime?.setDocument({ width: widget.w, height: widget.h, widgets: [placed], assets });
    runtime?.scale(scale);
  });
</script>

<div bind:this={host} style:position="relative" style:width="100%" style:height="100%" style:overflow="hidden"></div>
