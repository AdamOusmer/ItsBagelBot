<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import { getI18n } from '@bagel/kit/i18n/context';
  import {
    MAX_EVERY_MINUTES, MAX_SHOW_SECONDS, MIN_EVERY_MINUTES, MIN_SHOW_SECONDS, POWERED_ALIGNS, POWERED_DIRECTIONS,
    type PoweredAlign, type PoweredConfig, type PoweredDirection
  } from '@bagel/kit/overlays';
  import Chip from '@bagel/ui/svelte/Chip.svelte';
  import Divider from '@bagel/ui/svelte/Divider.svelte';
  import SaveStatus, { type SaveState } from '@bagel/ui/svelte/SaveStatus.svelte';
  import SegmentedControl from '@bagel/ui/svelte/SegmentedControl.svelte';
  import Slider from '@bagel/ui/svelte/Slider.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';

  let { config, status, onChange }: {
    config: PoweredConfig;
    status: Record<string, SaveState>;
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
  const stateOf = (key: keyof PoweredConfig) => status[key] ?? 'idle';
  const lastEdge = (dir: PoweredDirection) => config.directions.length === 1 && config.directions[0] === dir;

  function slide(key: 'every_minutes' | 'show_seconds', value: number) {
    if (key === 'every_minutes') every = value;
    else show = value;
  }

  function commit(key: 'every_minutes' | 'show_seconds', value: number) {
    if (config[key] !== value) onChange(key, { ...config, [key]: value });
  }

  function toggle(dir: PoweredDirection) {
    const on = !config.directions.includes(dir);
    const directions = POWERED_DIRECTIONS.filter((d) => (d === dir ? on : config.directions.includes(d)));
    if (directions.length > 0) onChange('directions', { ...config, directions });
  }

  function align(value: string) {
    if (value !== config.align) onChange('align', { ...config, align: value as PoweredAlign });
  }

  const numeric = (event: Event) => Number((event.currentTarget as HTMLInputElement).value);
</script>

{#each sliders as slider (slider.key)}
  <div class="row">
    <div class="row-head">
      <label class="row-label" for="powered-{slider.key}">
        <Text as="span" size="sm"><bb-fit><span data-fit="">{slider.label}</span></bb-fit></Text>
      </label>
      <SaveStatus state={stateOf(slider.key)} />
      <span class="row-value"><Text as="span" size="sm" tone="muted"><bb-fit><span data-fit="">{slider.value}</span></bb-fit></Text></span>
    </div>
    <Slider id="powered-{slider.key}" name={slider.key} min={slider.min} max={slider.max} step={1} value={slider.draft}
      aria-valuetext={slider.value} oninput={(event) => slide(slider.key, numeric(event))}
      onchange={(event) => commit(slider.key, numeric(event))} />
  </div>
  <Divider flush />
{/each}

<div class="row" role="group" aria-labelledby="powered-from">
  <div class="row-head">
    <span class="row-label" id="powered-from">
      <Text as="span" size="sm"><bb-fit><span data-fit="">{t('overlays.from')}</span></bb-fit></Text>
    </span>
    <SaveStatus state={stateOf('directions')} />
  </div>
  <div class="edges">
    {#each POWERED_DIRECTIONS as dir (dir)}
      <Chip pressed={config.directions.includes(dir)} disabled={lastEdge(dir)} onclick={() => toggle(dir)}>
        <bb-fit class="edge-label"><span data-fit="">{t(`overlays.edge.${dir}`)}</span></bb-fit>
      </Chip>
    {/each}
  </div>
  <div class="row-hint bb-fit-box">
    <Text as="div" size="xs" tone="muted"><bb-fit mode="block">{t('overlays.fromHint')}</bb-fit></Text>
  </div>
</div>
<Divider flush />

<div class="row">
  <div class="row-head">
    <span class="row-label" id="powered-align">
      <Text as="span" size="sm"><bb-fit><span data-fit="">{t('overlays.align')}</span></bb-fit></Text>
    </span>
    <SaveStatus state={stateOf('align')} />
  </div>
  <SegmentedControl options={aligns} value={config.align} label={t('overlays.align')} even onValueChange={align} />
</div>

<style>
  .row {
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    gap: var(--bb-space-2);
    padding: 14px 18px;
  }
  .row-head {
    display: grid;
    grid-template-columns: minmax(0, 1fr) auto minmax(0, 7rem);
    align-items: center;
    gap: var(--bb-space-2);
  }
  .row-label, .row-value { min-inline-size: 0; }
  .row-value { grid-column: 3; justify-self: end; text-align: end; }
  .edges {
    display: grid;
    grid-template-columns: repeat(4, minmax(0, 1fr));
    gap: var(--bb-space-2);
  }
  .edge-label { min-inline-size: 0; }
  .row-hint { block-size: 1.5rem; min-inline-size: 0; align-content: center; }
  @media (max-width: 560px) {
    .edges { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  }
</style>
