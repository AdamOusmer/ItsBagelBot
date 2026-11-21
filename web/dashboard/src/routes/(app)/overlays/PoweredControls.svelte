<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import { getI18n } from '@bagel/kit/i18n/context';
  import {
    MAX_EVERY_MINUTES, MAX_SHOW_SECONDS, MIN_EVERY_MINUTES, MIN_SHOW_SECONDS, POWERED_ALIGNS, POWERED_DIRECTIONS,
    type PoweredAlign, type PoweredConfig, type PoweredDirection
  } from '@bagel/kit/overlays';
  import Card from '@bagel/ui/svelte/Card.svelte';
  import Heading from '@bagel/ui/svelte/Heading.svelte';
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

<Card as="section" class="dock" aria-labelledby="powered-timing">
  <Heading level={6} as="h2" id="powered-timing">
    <bb-fit><span data-fit="">{t('overlays.timing')}</span></bb-fit>
  </Heading>
  <div class="sliders">
    {#each sliders as slider (slider.key)}
      <div class="slider-head">
        <label class="row-label bb-field__label bb-fit-box" for="powered-{slider.key}"><bb-fit group="powered-timing">{slider.label}</bb-fit></label>
        <span class="slider-value bb-fit-box" aria-hidden="true"><bb-fit>{slider.value}</bb-fit></span>
      </div>
      <Slider id="powered-{slider.key}" name={slider.key} min={slider.min} max={slider.max} step={1} value={slider.draft}
        aria-valuetext={slider.value} oninput={(event) => slide(slider.key, numeric(event))}
        onchange={(event) => commit(slider.key, numeric(event))} />
    {/each}
  </div>
</Card>

<Card as="section" class="dock" aria-labelledby="powered-placement">
  <Heading level={6} as="h2" id="powered-placement">
    <bb-fit><span data-fit="">{t('overlays.placement')}</span></bb-fit>
  </Heading>
  <div class="rows">
    <span class="row-label bb-field__label bb-fit-box" id="powered-from"><bb-fit group="powered-placement">{t('overlays.from')}</bb-fit></span>
    <SegmentedControl class="bb-tabs--plain" options={edges} value={config.directions[0] ?? ''} label={t('overlays.from')} even
      onValueChange={direction} />
    <span class="row-label bb-field__label bb-fit-box" id="powered-align"><bb-fit group="powered-placement">{t('overlays.align')}</bb-fit></span>
    <SegmentedControl class="bb-tabs--plain" options={aligns} value={config.align} label={t('overlays.align')} even
      onValueChange={align} />
  </div>
</Card>

<style>
  .rows {
    display: grid;
    grid-template-columns: minmax(0, 7rem) minmax(0, 1fr);
    align-items: center;
    gap: var(--bb-space-1) var(--bb-space-3);
  }
  .sliders {
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    row-gap: var(--bb-space-1);
  }
  .slider-head {
    display: grid;
    grid-template-columns: minmax(0, 1fr) var(--bb-space-7);
    align-items: center;
    gap: var(--bb-space-2);
  }
  .slider-head:not(:first-child) { margin-block-start: var(--bb-space-2); }
  .row-label {
    min-inline-size: 0;
    margin: 0;
    white-space: nowrap;
  }
  .slider-value {
    min-inline-size: 0;
    font-size: var(--bb-text-xs);
    color: var(--bb-muted-light);
    font-variant-numeric: tabular-nums;
    text-align: end;
    white-space: nowrap;
  }
</style>
