<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { clampBox } from '@bagel/ui/lib/box-transform';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import Field from '@bagel/ui/svelte/Field.svelte';
  import Slider from '@bagel/ui/svelte/Slider.svelte';
  import Stack from '@bagel/ui/svelte/Stack.svelte';
  import { getI18n } from '../../lib/i18n/context';
  import type { OverlayWidget } from '../../lib/overlay-document';
  import type { Canvas } from '../../lib/overlay-editor/defaults';
  import { geometryFields } from '../../lib/overlay-editor/fields';
  import { centerPosition } from '../../lib/overlay-editor/ops';
  import Fit from './Fit.svelte';
  import SettingControl from './SettingControl.svelte';

  let { widget, canvas, update }: {
    widget: OverlayWidget;
    canvas: Canvas;
    update: (widget: OverlayWidget) => void;
  } = $props();

  const { t } = getI18n();
  const fieldOf = (key: string) => geometryFields.find((field) => field.key === key)!;
  const rows = [['w', 'h'], ['y', 'x'], ['rotation']].map((keys) => keys.map(fieldOf));
  const opacity = fieldOf('opacity');
  const opacityLabel = t('overlayEditor.field.geometry.opacity');
  let live = $state<number>();

  const percent = $derived(live ?? Math.round(widget.opacity * 100));
  const read = (key: string): unknown => (widget as unknown as Record<string, unknown>)[key];

  function moveBox(key: string, value: unknown): void {
    const next = { ...widget, [key]: value };
    update({ ...next, ...clampBox(next, canvas) });
  }

  function commitOpacity(value: number): void {
    live = undefined;
    update({ ...widget, opacity: value / 100 });
  }
</script>

<Stack gap={3}>
  {#each rows as row (row[0]?.key)}
    <div class="pair">
      {#each row as field (field.key)}
        <SettingControl {field} value={read(field.key)} disabled={widget.locked} update={(next) => moveBox(field.key, next)} />
      {/each}
    </div>
  {/each}
  <Field label={opacityLabel} fit hint={`${percent}%`}>
    <Slider
      value={percent}
      min={opacity.min! * 100}
      max={opacity.max! * 100}
      step={1}
      aria-label={opacityLabel}
      oninput={(value) => (live = value)}
      onchange={commitOpacity}
    />
  </Field>
  <Button variant="secondary" block disabled={widget.locked} onclick={() => update({ ...widget, ...centerPosition(widget, canvas) })}>
    <Fit>{t('overlayEditor.ui.centerWidget')}</Fit>
  </Button>
</Stack>

<style>
  .pair { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 12px; }
</style>
