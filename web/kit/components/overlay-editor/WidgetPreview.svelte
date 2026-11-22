<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { getI18n } from '../../lib/i18n/context';
  import type { OverlayWidget } from '../../lib/overlay-document';
  import { completeSettings, type Settings } from '../../lib/overlay-editor/defaults';
  import { refAssetId, type AssetUrls } from '../../lib/overlay-editor/document';
  import { fontStack } from '../../lib/overlay-editor/fonts';
  import { previewMedia, previewText } from '../../lib/overlay-editor/preview';

  let { widget, scale, urls }: { widget: OverlayWidget; scale: number; urls: AssetUrls } = $props();

  const JUSTIFY: Record<string, string> = { left: 'flex-start', right: 'flex-end' };
  const { t } = getI18n();
  const settings = $derived(completeSettings(widget.kind, widget.settings));
  const font = $derived((typeof settings.style === 'object' ? settings.style : {}) as Settings);
  const mediaUrl = $derived(urls[refAssetId(previewMedia(widget.kind, settings)) ?? '']);
  const text = $derived(previewText(widget.kind, settings));
  const shadow = $derived(`0 0 ${Number(font.shadow ?? 0) * scale}px ${font.shadowColor ?? '#000000'}`);
  const fit = $derived(String(settings.fit ?? 'contain'));
</script>

<div style:opacity={widget.opacity} style:width="100%" style:height="100%">
  {#if mediaUrl && widget.kind === 'video'}
    <video src={mediaUrl} style:width="100%" style:height="100%" style:object-fit={fit} muted loop autoplay playsinline></video>
  {:else if mediaUrl}
    <img src={mediaUrl} alt={widget.name} style:width="100%" style:height="100%" style:object-fit={fit} />
  {:else if text !== undefined}
    <div
      style:width="100%"
      style:height="100%"
      style:display="flex"
      style:align-items="center"
      style:justify-content={JUSTIFY[String(settings.align)] ?? 'center'}
      style:font-family={fontStack(String(font.family ?? 'System sans'))}
      style:font-size={`${Number(font.size ?? 36) * scale}px`}
      style:font-weight={String(font.weight ?? 700)}
      style:color={String(font.color ?? '#ffffff')}
      style:text-shadow={shadow}
    >
      {text}
    </div>
  {:else}
    <div style:width="100%" style:height="100%" style:display="grid" style:place-items="center">{t(`overlayEditor.kind.${widget.kind}`)}</div>
  {/if}
</div>
