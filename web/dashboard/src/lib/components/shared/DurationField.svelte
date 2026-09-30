<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import type { Snippet } from 'svelte';
  import Input from '@bagel/ui/svelte/Input.svelte';
  import Select from '@bagel/ui/svelte/Select.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import { getI18n } from '@bagel/kit';

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
    <span class="num">
      <Input
        id={id ?? uid}
        {name}
        type="number"
        inputmode="numeric"
        min={min / UNIT_SECONDS[unit]}
        max={max / UNIT_SECONDS[unit]}
        step={1}
        {disabled}
        {invalid}
        aria-label={label}
        aria-invalid={invalid ? 'true' : undefined}
        aria-describedby={describedby}
        bind:value={amount}
        oninput={commit}
        {onblur}
      />
    </span>
    <span class="unit">
      <Select
        bind:value={unit}
        options={unitOptions}
        {disabled}
        label={t('common.unit.label')}
        onchange={onUnit}
      />
    </span>
  </div>
  {#if hint}<span class="help"><Text as="small" size="xs" tone="muted">{@render hint()}</Text></span>{/if}
</div>

<style>
  .duration-row { display: flex; align-items: center; gap: 10px; }
  .num { display: block; width: 100px; flex: none; }
  .unit { flex: 1; min-width: 130px; }
  .help { display: block; margin-top: 2px; opacity: 0.7; }
</style>
