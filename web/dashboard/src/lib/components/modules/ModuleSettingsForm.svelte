<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import AlertBanner from '@bagel/ui/svelte/AlertBanner.svelte';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import ButtonLink from '@bagel/ui/svelte/ButtonLink.svelte';
  import Divider from '@bagel/ui/svelte/Divider.svelte';
  import Input from '@bagel/ui/svelte/Input.svelte';
  import SaveStatus, { type SaveState } from '@bagel/ui/svelte/SaveStatus.svelte';
  import Select from '@bagel/ui/svelte/Select.svelte';
  import SwitchRow from '@bagel/ui/svelte/SwitchRow.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import Textarea from '@bagel/ui/svelte/Textarea.svelte';
  import { getI18n, automodToggleDefault, moduleDef, tModuleFieldOption, tModuleFieldPart, tModuleLabel, type ModuleDef, type ModuleField } from '@bagel/kit';
  import TimezonePicker from './TimezonePicker.svelte';

  let { def, config, modStatus, saveSetting, locked = false, requiresOff = [], browserZone = '', tzZones = [] }: {
    def: ModuleDef;
    config: Record<string, string>;
    modStatus: Record<string, SaveState>;
    saveSetting: (field: ModuleField, value: string) => void;
    locked?: boolean;
    requiresOff?: string[];
    browserZone?: string;
    tzZones?: string[];
  } = $props();

  const { t } = getI18n();

  function settingToggleOn(field: ModuleField): boolean {
    const v = config[field.key] ?? '';
    if (v === 'on') return true;
    if (v === 'off') return false;
    if (field.followsLevel) return automodToggleDefault(config['level'] || 'moderate', field.key);
    return field.defaultOn === true;
  }

  function requiredDef(field: ModuleField) {
    return field.requires && requiresOff.includes(field.requires) ? moduleDef(field.requires) : undefined;
  }

  function fieldCopy(field: ModuleField, part: 'label' | 'help' | 'placeholder'): string {
    return tModuleFieldPart(t, def.id, field, part);
  }

  function blurOnEnter(e: KeyboardEvent & { currentTarget: HTMLInputElement }) {
    if (e.key === 'Enter') e.currentTarget.blur();
  }
  function fieldId(field: ModuleField): string {
    return `mod-${def.id}-setting-${field.key}`;
  }
</script>

{#each (def.settings ?? []).filter((s) => !s.hidden) as field (field.key)}
  {@const needs = requiredDef(field)}
  <Divider flush />
  {#if field.type === 'toggle'}
    <div class="setting-row toggle">
      <SwitchRow
        control="end"
        label={fieldCopy(field, 'label')}
        hint={fieldCopy(field, 'help') || undefined}
        hintId={fieldId(field) + "-hint"}
        checked={settingToggleOn(field)}
        busy={modStatus[`setting:${field.key}`] === 'saving'}
        disabled={locked || !!needs}
        title={locked ? t('modules.lockedHint') : undefined}
        onCheckedChange={(v) => saveSetting(field, v ? 'on' : 'off')}
      >
        {#snippet status()}<SaveStatus state={modStatus[`setting:${field.key}`] ?? 'idle'} />{/snippet}
      </SwitchRow>
    </div>
  {:else if field.type === 'timezone'}
    <div class="setting-row">
      <label class="tr-text" for={fieldId(field)}>
        <Text as="span" size="sm">{fieldCopy(field, 'label')}</Text>
        {#if fieldCopy(field, 'help')}<Text as="span" size="xs" tone="muted">{fieldCopy(field, 'help')}</Text>{/if}
      </label>
      <SaveStatus state={modStatus[`setting:${field.key}`] ?? 'idle'} />
      <TimezonePicker id={fieldId(field)} value={config[field.key] ?? ''} zones={tzZones} disabled={locked} onPick={(tz) => saveSetting(field, tz)} />
    </div>
    {#if browserZone && (config[field.key] ?? '') !== browserZone}
      <div class="tz-suggest">
        <AlertBanner tone="success" role="status" stack>
          {t('modules.tzSuggested', { tz: browserZone })}
          {#snippet actions()}
            <Button variant="secondary" size="sm" disabled={locked} onclick={() => saveSetting(field, browserZone)} tone="success">{t('modules.tzApply', { tz: browserZone })}</Button>
          {/snippet}
        </AlertBanner>
      </div>
    {/if}
  {:else}
    <div class="setting-row {field.type === 'textarea' ? 'stacked' : ''}">
      <label class="tr-text" for={fieldId(field)}>
        <Text as="span" size="sm">{fieldCopy(field, 'label')}</Text>
        {#if fieldCopy(field, 'help')}<Text as="span" size="xs" tone="muted">{fieldCopy(field, 'help')}</Text>{/if}
      </label>
      <SaveStatus state={modStatus[`setting:${field.key}`] ?? 'idle'} />
      {#if field.type === 'select'}
        <div class="setting-picker">
          <Select
            fill
            disabled={locked}
            id={fieldId(field)}
            value={config[field.key] || field.placeholder || field.options?.[0]?.value || ''}
            onchange={(e) => saveSetting(field, e.currentTarget.value)}
            options={(field.options ?? []).map((opt) => ({ value: opt.value, label: tModuleFieldOption(t, def.id, field.key, opt) }))}
          />
        </div>
      {:else if field.type === 'textarea'}
        <Textarea
          id={fieldId(field)}
          fill
          rows={6}
          placeholder={fieldCopy(field, 'placeholder')}
          value={config[field.key] ?? ''}
          disabled={locked}
          onchange={(e: Event & { currentTarget: HTMLTextAreaElement }) => saveSetting(field, e.currentTarget.value)}
        />
      {:else}
        <Input
          id={fieldId(field)}
          type={field.type === 'number' ? 'number' : 'text'}
          disabled={locked || !!needs}
          placeholder={fieldCopy(field, 'placeholder')}
          value={config[field.key] ?? ''}
          onchange={(e: Event & { currentTarget: HTMLInputElement }) => saveSetting(field, e.currentTarget.value)}
          onkeydown={blurOnEnter}
        />
      {/if}
    </div>
  {/if}
  {#if needs}
    <Divider flush />
    <div class="disabled-note">
      <AlertBanner tone="warning" stack>
        {t('modules.settingNeeds', { module: tModuleLabel(t, needs) })}
        {#snippet actions()}
          <ButtonLink href={needs.href ?? `/modules/${needs.id}`} variant="ghost">{t('modules.nestedUnderLink', { parent: tModuleLabel(t, needs) })}</ButtonLink>
        {/snippet}
      </AlertBanner>
    </div>
  {/if}
{/each}

<style>
  .tr-text { display: flex; flex-direction: column; gap: 3px; margin-right: auto; min-width: 0; }

  .disabled-note {
    padding: 12px 18px 16px;
  }

  .setting-row {
    --input-w: min(260px, 44vw);
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 14px 18px;
  }
  .setting-picker { width: min(260px, 44vw); }

  .setting-row.toggle { display: block; }
  .setting-row.stacked { flex-direction: column; align-items: stretch; }
  .setting-row.stacked .tr-text { margin-right: 0; }
  @media (max-width: 560px) {
    .setting-row { --input-w: 100%; flex-wrap: wrap; }
    .setting-picker { width: 100%; }
  }

  .tz-suggest {
    padding: 0 18px;
  }

</style>
