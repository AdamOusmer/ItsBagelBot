<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { getI18n } from '@bagel/kit';
  import type { RehearsalScenario, ScenarioField, ScenarioValue } from '#lib/flow/rehearse';
  import { scenarioFieldLabel, scenarioRoleLabel } from '#lib/flow/rehearse';
  import Disclosure from '@bagel/ui/svelte/Disclosure.svelte';
  import Field from '@bagel/ui/svelte/Field.svelte';
  import Input from '@bagel/ui/svelte/Input.svelte';
  import Select from '@bagel/ui/svelte/Select.svelte';
  import Switch from '@bagel/ui/svelte/Switch.svelte';

  let { fields, scenario, onChange }: { fields: readonly ScenarioField[]; scenario: RehearsalScenario; onChange: (patch: Partial<RehearsalScenario>) => void } = $props();
  const { t } = getI18n();
  const uid = $props.id();
  const label = (key: ScenarioField['key']) => scenarioFieldLabel(t, key);
  const update = (field: ScenarioField, value: ScenarioValue) => onChange({ [field.key]: value });
  const options = (field: ScenarioField) => (field.options ?? []).map((value) => ({ value, label: field.kind === 'role' ? scenarioRoleLabel(t, value) : value.replace('000', '') }));
</script>

{#snippet inputs(list: readonly ScenarioField[])}
  <div class="inputs">
    {#each list as field (field.key)}
      <div class="input" class:wide={field.kind === 'text' && !['user', 'userId'].includes(field.key)}>
        <Field label={label(field.key)} for="{uid}-{field.key}">
          {#if field.kind === 'toggle'}
            <div class="toggle">
              <Switch id="{uid}-{field.key}" label={label(field.key)} checked={Boolean(scenario[field.key])} onCheckedChange={(value) => update(field, value)} />
              <span>{field.key === 'live' ? t(scenario.live ? 'flows.test.live' : 'flows.test.streamOffline') : t(scenario[field.key] ? 'flows.test.yes' : 'flows.test.no')}</span>
            </div>
          {:else if field.options}
            <Select id="{uid}-{field.key}" fill label={label(field.key)} value={String(scenario[field.key] ?? field.default)} options={options(field)} onchange={(event) => update(field, event.currentTarget.value)} />
          {:else}
            <Input id="{uid}-{field.key}" fill type={field.kind === 'number' ? 'number' : 'text'} min={field.kind === 'number' ? 0 : undefined} value={String(scenario[field.key] ?? '')} oninput={(event) => update(field, field.kind === 'number' ? Number(event.currentTarget.value) : event.currentTarget.value)} />
          {/if}
        </Field>
      </div>
    {/each}
  </div>
{/snippet}

{@render inputs(fields.filter((field) => !field.more))}
<Disclosure summary={t('flows.test.more')} size="sm">
  {@render inputs(fields.filter((field) => field.more))}
</Disclosure>

<style>
  .inputs { display: flex; flex-wrap: wrap; gap: 12px; }
  .input { flex: 1 1 120px; min-width: 0; }
  .input.wide { flex-basis: 100%; }
  .toggle { display: flex; align-items: center; gap: 8px; min-height: 40px; font-size: 14px; }
</style>
