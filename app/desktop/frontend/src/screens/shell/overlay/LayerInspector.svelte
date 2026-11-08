<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import Button from '@bagel/ui/svelte/Button.svelte';
  import Chip from '@bagel/ui/svelte/Chip.svelte';
  import ColorPicker from '@bagel/ui/svelte/ColorPicker.svelte';
  import Field from '@bagel/ui/svelte/Field.svelte';
  import Icon from '@bagel/ui/svelte/Icon.svelte';
  import IconButton from '@bagel/ui/svelte/IconButton.svelte';
  import IconTile from '@bagel/ui/svelte/IconTile.svelte';
  import Input from '@bagel/ui/svelte/Input.svelte';
  import Select from '@bagel/ui/svelte/Select.svelte';
  import Slider from '@bagel/ui/svelte/Slider.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import Textarea from '@bagel/ui/svelte/Textarea.svelte';
  import { clampLayer } from '../../../lib/overlay/clamp';
  import { KIND_ICON, KIND_LABEL, KIND_TINT } from '../../../lib/overlay/kind';
  import { percentFromPx, pxFromPercent } from '../../../lib/overlay/resolutions';
  import type { Layer, LayerTone } from '../../../lib/overlay/types';

  let {
    layer,
    canvasW,
    canvasH,
    onCommit,
    onRemove,
    onDuplicate,
    onClose,
  }: {
    layer: Layer;
    canvasW: number;
    canvasH: number;
    onCommit: () => void;
    onRemove: () => void;
    onDuplicate: () => void;
    onClose: () => void;
  } = $props();

  const uid = $props.id();

  function setXPx(px: number): void {
    layer.x = percentFromPx(px, canvasW);
  }
  function setYPx(px: number): void {
    layer.y = percentFromPx(px, canvasH);
  }
  function setWPx(px: number): void {
    layer.w = percentFromPx(px, canvasW);
  }
  function setHPx(px: number): void {
    layer.h = percentFromPx(px, canvasH);
  }
  function setFontPx(px: number): void {
    if (layer.kind === 'text') layer.fontSize = percentFromPx(px, canvasH);
  }

  const ALIGN_OPTIONS = [
    { value: 'start', label: 'Left' },
    { value: 'center', label: 'Center' },
    { value: 'end', label: 'Right' },
  ];
  const FIT_OPTIONS = [
    { value: 'contain', label: 'Contain' },
    { value: 'cover', label: 'Cover' },
  ];
  const TONE_OPTIONS = [
    { value: 'free', label: 'Free' },
    { value: 'paid', label: 'Paid' },
    { value: 'vip', label: 'VIP' },
  ];

  function onBox(): void {
    clampLayer(layer);
  }
</script>

<aside class="panel" aria-label="{KIND_LABEL[layer.kind]} properties">
  <header class="head">
    <IconTile style="--tile: 36px; color: {KIND_TINT[layer.kind]}">
      <Icon name={KIND_ICON[layer.kind]} size={18} />
    </IconTile>
    <div class="titles">
      <h2 class="name">{layer.name}</h2>
      <p class="kind">{KIND_LABEL[layer.kind]}</p>
    </div>
    <IconButton label="Close settings" tooltip size="sm" onclick={onClose}><Icon name="x" size={14} /></IconButton>
  </header>

  <div class="scroll">
    <div class="fields">
      <div class="pair">
        <Field label="X" for="{uid}-x">
          <Input
            id="{uid}-x"
            type="number"
            value={pxFromPercent(layer.x, canvasW)}
            oninput={(e) => setXPx(Number(e.currentTarget.value))}
            onchange={() => {
              onBox();
              onCommit();
            }}
          >
            {#snippet trailing()}<span class="unit">px</span>{/snippet}
          </Input>
        </Field>
        <Field label="Y" for="{uid}-y">
          <Input
            id="{uid}-y"
            type="number"
            value={pxFromPercent(layer.y, canvasH)}
            oninput={(e) => setYPx(Number(e.currentTarget.value))}
            onchange={() => {
              onBox();
              onCommit();
            }}
          >
            {#snippet trailing()}<span class="unit">px</span>{/snippet}
          </Input>
        </Field>
      </div>

      <div class="pair">
        <Field label="Width" for="{uid}-w">
          <Input
            id="{uid}-w"
            type="number"
            value={pxFromPercent(layer.w, canvasW)}
            oninput={(e) => setWPx(Number(e.currentTarget.value))}
            onchange={() => {
              onBox();
              onCommit();
            }}
          >
            {#snippet trailing()}<span class="unit">px</span>{/snippet}
          </Input>
        </Field>
        <Field label="Height" for="{uid}-h">
          <Input
            id="{uid}-h"
            type="number"
            value={pxFromPercent(layer.h, canvasH)}
            oninput={(e) => setHPx(Number(e.currentTarget.value))}
            onchange={() => {
              onBox();
              onCommit();
            }}
          >
            {#snippet trailing()}<span class="unit">px</span>{/snippet}
          </Input>
        </Field>
      </div>

      <Field label="Opacity" for="{uid}-opacity">
        <div class="slider-row">
          <Slider
            id="{uid}-opacity"
            min={10}
            max={100}
            value={layer.opacity}
            oninput={(e) => (layer.opacity = Number(e.currentTarget.value))}
            onchange={onCommit}
          />
          <span class="unit">{Math.round(layer.opacity)}%</span>
        </div>
      </Field>

      {#if layer.kind === 'text'}
        {@const t = layer}
        <Field label="Content" for="{uid}-content">
          <Textarea
            id="{uid}-content"
            rows={3}
            value={t.content}
            oninput={(e) => (t.content = e.currentTarget.value)}
            onchange={onCommit}
          />
        </Field>
        <div class="pair">
          <Field label="Font size" for="{uid}-font-size">
            <Input
              id="{uid}-font-size"
              type="number"
              value={pxFromPercent(t.fontSize, canvasH)}
              oninput={(e) => setFontPx(Number(e.currentTarget.value))}
              onchange={onCommit}
            >
              {#snippet trailing()}<span class="unit">px</span>{/snippet}
            </Input>
          </Field>
          <Field label="Align" for="{uid}-align">
            <Select
              id="{uid}-align"
              options={ALIGN_OPTIONS}
              value={t.align}
              onchange={(e) => {
                t.align = e.currentTarget.value as typeof t.align;
                onCommit();
              }}
            />
          </Field>
        </div>
        <Field label="Color" for="{uid}-color">
          <ColorPicker
            id="{uid}-color"
            value={t.color}
            onValueChange={(hex) => {
              t.color = hex;
              onCommit();
            }}
          />
        </Field>
      {:else if layer.kind === 'image'}
        {@const img = layer}
        <Field label="Placeholder caption" for="{uid}-caption">
          <Input
            id="{uid}-caption"
            value={img.caption}
            oninput={(e) => (img.caption = e.currentTarget.value)}
            onchange={onCommit}
          />
        </Field>
        <Field label="Fit" for="{uid}-fit">
          <Select
            id="{uid}-fit"
            options={FIT_OPTIONS}
            value={img.fit}
            onchange={(e) => {
              img.fit = e.currentTarget.value as typeof img.fit;
              onCommit();
            }}
          />
        </Field>
      {:else if layer.kind === 'alert'}
        {@const alert = layer}
        <Field label="Title" for="{uid}-title">
          <Input
            id="{uid}-title"
            value={alert.title}
            oninput={(e) => (alert.title = e.currentTarget.value)}
            onchange={onCommit}
          />
        </Field>
        <Field label="Subtitle" for="{uid}-subtitle">
          <Input
            id="{uid}-subtitle"
            value={alert.subtitle}
            oninput={(e) => (alert.subtitle = e.currentTarget.value)}
            onchange={onCommit}
          />
        </Field>
        <Field label="Tone" for="{uid}-tone">
          <div class="tone-row">
            <Select
              id="{uid}-tone"
              options={TONE_OPTIONS}
              value={alert.tone}
              onchange={(e) => {
                alert.tone = e.currentTarget.value as LayerTone;
                onCommit();
              }}
            />
            <Chip tone={alert.tone} pressed as="span">{TONE_OPTIONS.find((o) => o.value === alert.tone)?.label}</Chip>
          </div>
        </Field>
      {:else if layer.kind === 'chat'}
        {@const chat = layer}
        <Field label="Rows visible" for="{uid}-rows" hint="How many sample messages to show.">
          <Input
            id="{uid}-rows"
            type="number"
            min={1}
            max={chat.messages.length}
            value={chat.rows}
            oninput={(e) => (chat.rows = Number(e.currentTarget.value))}
            onchange={onCommit}
          />
        </Field>
        <Text size="sm" tone="muted">Sample chat messages; not editable here.</Text>
      {/if}
    </div>
  </div>

  <footer class="foot">
    <Button variant="ghost" onclick={onDuplicate}>Duplicate</Button>
    <Button tone="danger" onclick={onRemove}>Remove layer</Button>
  </footer>
</aside>

<style>
  .panel {
    display: flex;
    flex-direction: column;
    height: 100%;
    min-height: 0;
    overflow: hidden;
    border-radius: var(--bb-radius-md);
    background: var(--bb-bg-1);
    box-shadow: 0 16px 48px rgba(var(--bb-shadow-rgb), 0.65);
  }

  .head {
    display: flex;
    align-items: flex-start;
    gap: 12px;
    flex: none;
    padding: 16px 12px 8px 16px;
  }

  .titles {
    flex: 1;
    min-width: 0;
  }

  .name {
    margin: 0;
    font-family: var(--bb-font-body);
    font-size: 16px;
    font-weight: 600;
    line-height: 1.3;
    color: var(--bb-white);
    overflow: hidden;
    white-space: nowrap;
    text-overflow: ellipsis;
  }

  .kind {
    margin: 2px 0 0;
    font-size: 13px;
    color: var(--bb-muted);
  }

  .scroll {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    padding: 16px;
  }

  .fields {
    display: flex;
    flex-direction: column;
    gap: 18px;
  }

  .pair {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 12px;
  }

  .unit {
    font-size: 13px;
    color: var(--bb-muted);
  }

  .slider-row {
    display: flex;
    align-items: center;
    gap: 12px;
  }

  .slider-row :global(.bb-slider) {
    flex: 1;
  }

  .slider-row .unit {
    flex: none;
    width: 40px;
    text-align: right;
    font-family: var(--bb-font-mono);
  }

  .tone-row {
    display: flex;
    align-items: center;
    gap: 10px;
  }

  .foot {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    flex: none;
    padding: 12px 16px;
    box-shadow: 0 -8px 16px rgba(var(--bb-shadow-rgb), 0.3);
  }
</style>
