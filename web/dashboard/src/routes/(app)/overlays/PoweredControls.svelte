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
  import Text from '@bagel/ui/svelte/Text.svelte';

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
  <div class="fields">
    {#each sliders as slider (slider.key)}
      <div class="field">
        <div class="field-head">
          <label class="field-label bb-fit-box" for="powered-{slider.key}">
            <Text as="span" size="sm"><bb-fit group="powered-timing">{slider.label}</bb-fit></Text>
          </label>
          <span class="field-value" aria-hidden="true"><Text as="span" size="sm" tone="muted">{slider.value}</Text></span>
        </div>
        <Slider id="powered-{slider.key}" name={slider.key} min={slider.min} max={slider.max} step={1} value={slider.draft}
          aria-valuetext={slider.value} oninput={(event) => slide(slider.key, numeric(event))}
          onchange={(event) => commit(slider.key, numeric(event))} />
      </div>
    {/each}
  </div>
</Card>

<Card as="section" class="dock" aria-labelledby="powered-placement">
  <Heading level={6} as="h2" id="powered-placement">
    <bb-fit><span data-fit="">{t('overlays.placement')}</span></bb-fit>
  </Heading>
  <div class="fields">
    <div class="field">
      <span class="field-label bb-fit-box" id="powered-from">
        <Text as="span" size="sm"><bb-fit group="powered-placement">{t('overlays.from')}</bb-fit></Text>
      </span>
      <SegmentedControl class="bb-tabs--plain" options={edges} value={config.directions[0] ?? ''} label={t('overlays.from')} even
        onValueChange={direction} />
    </div>
    <div class="field">
      <span class="field-label bb-fit-box" id="powered-align">
        <Text as="span" size="sm"><bb-fit group="powered-placement">{t('overlays.align')}</bb-fit></Text>
      </span>
      <SegmentedControl class="bb-tabs--plain" options={aligns} value={config.align} label={t('overlays.align')} even
        onValueChange={align} />
    </div>
  </div>
</Card>

<style>
  .fields {
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    flex: 1;
    align-content: space-evenly;
    gap: var(--bb-space-5);
    margin-block-start: var(--bb-space-4);
  }
  .field {
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    gap: var(--bb-space-2);
  }
  .field-head {
    display: grid;
    grid-template-columns: minmax(0, 1fr) auto;
    align-items: baseline;
    gap: var(--bb-space-3);
  }
  .field-label {
    min-inline-size: 0;
    white-space: nowrap;
  }
  .field-value {
    font-variant-numeric: tabular-nums;
    white-space: nowrap;
  }
</style>
