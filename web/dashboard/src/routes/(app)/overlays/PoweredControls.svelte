<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import { getI18n } from '@bagel/kit/i18n/context';
  import {
    MAX_EVERY_MINUTES, MAX_SHOW_SECONDS, MIN_EVERY_MINUTES, MIN_SHOW_SECONDS, POWERED_ALIGNS, POWERED_DIRECTIONS,
    type PoweredAlign, type PoweredConfig, type PoweredDirection
  } from '@bagel/kit/overlays';
  import Field from '@bagel/ui/svelte/Field.svelte';
  import SegmentedControl from '@bagel/ui/svelte/SegmentedControl.svelte';
  import Slider from '@bagel/ui/svelte/Slider.svelte';

  let { config, onChange }: {
    config: PoweredConfig;
    onChange: (key: keyof PoweredConfig, next: PoweredConfig) => void;
  } = $props();

  const { t } = getI18n();
  let every = $derived(config.every_minutes);
  let show = $derived(config.show_seconds);

  const sliders = $derived([
    {
      key: 'every_minutes', label: t('overlays.every'), value: t('overlays.everyValue', { minutes: every }),
      draft: every, min: MIN_EVERY_MINUTES, max: MAX_EVERY_MINUTES
    },
    {
      key: 'show_seconds', label: t('overlays.show'), value: t('overlays.showValue', { seconds: show }),
      draft: show, min: MIN_SHOW_SECONDS, max: MAX_SHOW_SECONDS
    }
  ] as const);
  const aligns = $derived(POWERED_ALIGNS.map((value) => ({ value, label: t(`overlays.alignOption.${value}`) })));
  const edges = $derived(POWERED_DIRECTIONS.map((value) => ({ value, label: t(`overlays.edge.${value}`) })));

  function slide(key: 'every_minutes' | 'show_seconds', value: number) {
    if (key === 'every_minutes') every = value;
    else show = value;
  }

  function commit(key: 'every_minutes' | 'show_seconds', value: number) {
    if (config[key] !== value) onChange(key, { ...config, [key]: value });
  }

  function direction(value: string) {
    if (value !== config.directions[0]) onChange('directions', { ...config, directions: [value as PoweredDirection] });
  }

  function align(value: string) {
    if (value !== config.align) onChange('align', { ...config, align: value as PoweredAlign });
  }

  const numeric = (event: Event) => Number((event.currentTarget as HTMLInputElement).value);
</script>

<div class="fields">
  {#each sliders as slider (slider.key)}
    <Field fit label={slider.label} for="powered-{slider.key}">
      <div class="slider-row">
        <Slider id="powered-{slider.key}" name={slider.key} min={slider.min} max={slider.max} step={1} value={slider.draft}
          aria-valuetext={slider.value} oninput={(event) => slide(slider.key, numeric(event))}
          onchange={(event) => commit(slider.key, numeric(event))} />
        <span class="slider-value bb-fit-box" aria-hidden="true"><bb-fit>{slider.value}</bb-fit></span>
      </div>
    </Field>
  {/each}
</div>

<div class="bb-field group">
  <span class="bb-field__label bb-fit-box" id="powered-from"><bb-fit mode="block">{t('overlays.from')}</bb-fit></span>
  <SegmentedControl class="bb-tabs--plain" options={edges} value={config.directions[0] ?? ''} label={t('overlays.from')} even
    onValueChange={direction} />
</div>

<div class="bb-field group">
  <span class="bb-field__label bb-fit-box" id="powered-align"><bb-fit mode="block">{t('overlays.align')}</bb-fit></span>
  <SegmentedControl class="bb-tabs--plain" options={aligns} value={config.align} label={t('overlays.align')} even
    onValueChange={align} />
</div>

<style>
  .fields {
    display: flex;
    flex-direction: column;
    gap: var(--bb-space-4);
    --field-mb: 0;
    --field-gap: var(--bb-space-2);
  }
  .group {
    --field-mb: 0;
    --field-gap: var(--bb-space-2);
  }
  .slider-row {
    display: grid;
    grid-template-columns: minmax(0, 1fr) var(--bb-space-7);
    align-items: center;
    gap: var(--bb-space-2);
    min-block-size: var(--field-control-h, 36px);
  }
  .slider-value {
    font-size: var(--bb-text-xs);
    color: var(--bb-muted-light);
    font-variant-numeric: tabular-nums;
    text-align: end;
    white-space: nowrap;
  }
</style>
