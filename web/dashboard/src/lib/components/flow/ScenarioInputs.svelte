<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { getI18n } from '@bagel/kit';
  import { MAX_COOLDOWN_SECONDS } from '@bagel/kit/automations';
  import type { ScenarioField, ScenarioStateField } from '@bagel/kit/automations';
  import type { RehearsalScenario, ScenarioInputField, ScenarioValue } from '#lib/flow/rehearse';
  import { scenarioInputLabel, scenarioRoleLabel } from '#lib/flow/rehearse';
  import Input from '@bagel/ui/svelte/Input.svelte';
  import Label from '@bagel/ui/svelte/Label.svelte';
  import Select from '@bagel/ui/svelte/Select.svelte';
  import Switch from '@bagel/ui/svelte/Switch.svelte';

  let { fields, scenario, onChange }: { fields: readonly ScenarioInputField[]; scenario: RehearsalScenario; onChange: (patch: Partial<RehearsalScenario>) => void } = $props();
  const { t } = getI18n();
  const uid = $props.id();
  const options = (field: ScenarioField) => (field.options ?? []).map((value) => ({ value, label: field.kind === 'role' ? scenarioRoleLabel(t, value) : value.replace('000', '') }));

  type TimerField = Extract<ScenarioStateField, { seed: 'countdowns' | 'cooldowns' }>;
  let timerDrafts = $state<Record<string, number>>({});
  const timerKey = (field: TimerField) => `${field.seed}:${field.name}`;
  const timerSeconds = (field: TimerField) => Math.ceil((scenario.state?.[field.seed]?.[field.name] ?? 0) / 1000);

  function inputValue(field: ScenarioInputField): ScenarioValue {
    if (!('seed' in field)) return scenario[field.key] ?? field.default;
    if (field.seed === 'points') return scenario.state?.points ?? field.default;
    if (field.seed === 'counters') return scenario.state?.counters?.[field.name] ?? field.default;
    const seconds = timerSeconds(field);
    return field.kind === 'toggle' ? seconds > 0 : (seconds || timerDrafts[timerKey(field)] || field.default);
  }

  function timerSeed(field: TimerField, value: ScenarioValue): number {
    if (field.kind === 'duration') return Math.min(MAX_COOLDOWN_SECONDS, Math.max(1, Math.trunc(Number(value)))) * 1000;
    const key = timerKey(field);
    if (value) return (timerDrafts[key] ?? field.initialSeconds) * 1000;
    timerDrafts[key] = timerSeconds(field);
    return 0;
  }

  function update(field: ScenarioInputField, value: ScenarioValue): void {
    if (!('seed' in field)) return onChange({ [field.key]: value });
    const state = scenario.state;
    if (field.seed === 'points') return onChange({ state: { ...state, points: Math.max(0, Math.trunc(Number(value))) } });
    const amount = field.seed === 'counters' ? Math.trunc(Number(value)) : timerSeed(field, value);
    onChange({ state: { ...state, [field.seed]: { ...state?.[field.seed], [field.name]: amount } } });
  }

  function updateNumber(field: ScenarioInputField, input: HTMLInputElement): void {
    if ('seed' in field && !Number.isFinite(input.valueAsNumber)) return;
    update(field, Number(input.value));
  }
</script>

<div class="inputs">
  {#each fields as field (field.key)}
    {@const id = `${uid}-${field.key}`}
    {@const fieldLabel = scenarioInputLabel(t, field)}
    {@const value = inputValue(field)}
    {@const inactive = field.kind === 'duration' && timerSeconds(field) <= 0}
    <div class="input" class:timer-running={'seed' in field && field.kind === 'toggle'} class:inactive inert={inactive}>
      <Label htmlFor={id} class="bb-fit-box"><bb-fit>{fieldLabel}</bb-fit></Label>
      {#if field.kind === 'toggle'}
        <div class="toggle">
          <Switch {id} label={fieldLabel} checked={Boolean(value)} onCheckedChange={(checked) => update(field, checked)} />
          <span class="toggle-status bb-fit-box"><bb-fit>{field.key === 'live' ? t(scenario.live ? 'flows.test.live' : 'flows.test.streamOffline') : t(value ? 'flows.test.yes' : 'flows.test.no')}</bb-fit></span>
        </div>
      {:else if 'options' in field && field.options}
        <Select {id} fill label={fieldLabel} value={String(value)} options={options(field)} onchange={(event) => update(field, event.currentTarget.value)} />
      {:else if field.kind === 'duration'}
        <Input {id} fill aria-label={fieldLabel} type="number" min={1} max={MAX_COOLDOWN_SECONDS} inputmode="numeric" value={Number(value)} aria-describedby={`${id}-unit`} oninput={(event) => updateNumber(field, event.currentTarget)}>
          {#snippet trailing()}<span id={`${id}-unit`} class="unit bb-fit-box"><bb-fit>{t('common.unit.seconds')}</bb-fit></span>{/snippet}
        </Input>
      {:else if field.kind === 'number'}
        <Input {id} fill aria-label={fieldLabel} type="number" min={'seed' in field && field.seed === 'counters' ? undefined : 0} value={Number(value)} oninput={(event) => updateNumber(field, event.currentTarget)} />
      {:else}
        <Input {id} fill aria-label={fieldLabel} value={String(value)} oninput={(event) => update(field, event.currentTarget.value)} />
      {/if}
    </div>
  {/each}
</div>

<style>
  .inputs { display: grid; flex: none; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: var(--bb-space-3) var(--bb-space-2); }
  .input { display: grid; grid-template-rows: var(--bb-space-5) calc(var(--bb-space-6) + var(--bb-space-3)); gap: var(--bb-space-1); min-width: 0; }
  .timer-running { grid-column: 1; }
  .inactive { opacity: 0.4; }
  .unit { flex: none; width: var(--bb-space-7); font-size: var(--bb-text-xs); color: var(--bb-muted-light); }
  .toggle { display: grid; grid-template-columns: auto minmax(0, 1fr); align-items: center; gap: var(--bb-space-2); font-size: var(--bb-text-sm); }
  .toggle-status { overflow: hidden; }

  @media (max-width: 760px), (pointer: coarse) and (max-height: 520px) {
    .inputs { grid-template-columns: minmax(0, 1fr); }
  }
</style>
