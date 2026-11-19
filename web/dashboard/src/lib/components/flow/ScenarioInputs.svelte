<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { getI18n } from '@bagel/kit';
  import type { RehearsalScenario, ScenarioField, ScenarioValue } from '#lib/flow/rehearse';
  import { scenarioInputLabel, scenarioRoleLabel } from '#lib/flow/rehearse';
  import Input from '@bagel/ui/svelte/Input.svelte';
  import Label from '@bagel/ui/svelte/Label.svelte';
  import Select from '@bagel/ui/svelte/Select.svelte';
  import Switch from '@bagel/ui/svelte/Switch.svelte';

  let { fields, scenario, onChange }: { fields: readonly ScenarioField[]; scenario: RehearsalScenario; onChange: (patch: Partial<RehearsalScenario>) => void } = $props();
  const { t } = getI18n();
  const uid = $props.id();
  const label = (key: ScenarioField['key']) => scenarioInputLabel(t, key);
  const update = (field: ScenarioField, value: ScenarioValue) => onChange({ [field.key]: value });
  const options = (field: ScenarioField) => (field.options ?? []).map((value) => ({ value, label: field.kind === 'role' ? scenarioRoleLabel(t, value) : value.replace('000', '') }));
</script>

<div class="inputs">
  {#each fields as field (field.key)}
    {@const id = `${uid}-${field.key}`}
    {@const fieldLabel = label(field.key)}
    <div class="input">
      <Label htmlFor={id} class="bb-fit-box"><bb-fit>{fieldLabel}</bb-fit></Label>
      {#if field.kind === 'toggle'}
        <div class="toggle">
          <Switch {id} label={fieldLabel} checked={Boolean(scenario[field.key])} onCheckedChange={(value) => update(field, value)} />
          <span class="toggle-status bb-fit-box"><bb-fit>{field.key === 'live' ? t(scenario.live ? 'flows.test.live' : 'flows.test.streamOffline') : t(scenario[field.key] ? 'flows.test.yes' : 'flows.test.no')}</bb-fit></span>
        </div>
      {:else if field.options}
        <Select {id} fill label={fieldLabel} value={String(scenario[field.key] ?? field.default)} options={options(field)} onchange={(event) => update(field, event.currentTarget.value)} />
      {:else}
        <Input {id} fill type={field.kind === 'number' ? 'number' : 'text'} min={field.kind === 'number' ? 0 : undefined} value={String(scenario[field.key] ?? '')} oninput={(event) => update(field, field.kind === 'number' ? Number(event.currentTarget.value) : event.currentTarget.value)} />
      {/if}
    </div>
  {/each}
</div>

<style>
  .inputs { display: grid; flex: none; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 12px; }
  .input { display: grid; grid-template-rows: 20px 44px; gap: 4px; min-width: 0; }
  .toggle { display: grid; grid-template-columns: auto minmax(0, 1fr); align-items: center; gap: 8px; font-size: 14px; }
  .toggle-status { overflow: hidden; }

  @media (max-width: 760px), (pointer: coarse) and (max-height: 520px) {
    .inputs { grid-template-columns: minmax(0, 1fr); }
  }
</style>
