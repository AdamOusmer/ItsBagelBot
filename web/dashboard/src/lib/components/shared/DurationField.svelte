<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import type { Snippet } from 'svelte';
  import { Select, getI18n } from '@bagel/kit';

  type Unit = 'seconds' | 'minutes' | 'hours';
  const UNIT_SECONDS: Record<Unit, number> = { seconds: 1, minutes: 60, hours: 3600 };
  const UNITS = Object.keys(UNIT_SECONDS) as Unit[];

  let {
    value = $bindable(0),
    min = 0,
    max = 86_400,
    id,
    name,
    label,
    invalid = false,
    describedby,
    disabled = false,
    hint,
    onblur
  }: {
    value?: number;
    min?: number;
    max?: number;
    id?: string;
    name?: string;
    label: string;
    invalid?: boolean;
    describedby?: string;
    disabled?: boolean;
    hint?: Snippet;
    onblur?: () => void;
  } = $props();

  const { t } = getI18n();
  const uid = $props.id();

  function fitUnit(seconds: number): Unit {
    if (seconds > 0 && seconds % 3600 === 0) return 'hours';
    if (seconds > 0 && seconds % 60 === 0) return 'minutes';
    return 'seconds';
  }

  // svelte-ignore state_referenced_locally
  let unit = $state<Unit>(fitUnit(value));
  // svelte-ignore state_referenced_locally
  let amount = $state<number | null>(value / UNIT_SECONDS[unit]);

  const unitOptions = $derived(
    UNITS.filter((u) => UNIT_SECONDS[u] <= Math.max(max, 1)).map((u) => ({
      value: u,
      label: t(`common.unit.${u}`)
    }))
  );

  function commit() {
    value = amount === null || !Number.isFinite(amount) ? Number.NaN : Math.round(amount * UNIT_SECONDS[unit]);
  }
  function onUnit() {
    commit();
  }
</script>

<div class="duration">
  <div class="duration-row">
    <input
      id={id ?? uid}
      {name}
      class="bb-input num"
      type="number"
      inputmode="numeric"
      min={min / UNIT_SECONDS[unit]}
      max={max / UNIT_SECONDS[unit]}
      step="1"
      {disabled}
      aria-label={label}
      aria-invalid={invalid ? 'true' : undefined}
      aria-describedby={describedby}
      data-invalid={invalid ? '' : undefined}
      bind:value={amount}
      oninput={commit}
      {onblur}
    />
    <Select
      bind:value={unit}
      options={unitOptions}
      {disabled}
      class="duration-unit"
      aria-label={t('common.unit.label')}
      onchange={onUnit}
    />
  </div>
  {#if hint}<small class="help">{@render hint()}</small>{/if}
</div>

<style>
  .duration-row { display: flex; align-items: center; gap: 10px; }
  .duration-row .num { width: 100px; flex: none; }
  .duration-row :global(.duration-unit) { flex: none; min-width: 130px; }
  .help { color: var(--bb-muted); opacity: 0.7; font-size: 11px; display: block; margin-top: 2px; }
</style>
