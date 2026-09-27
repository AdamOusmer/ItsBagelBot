<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { getI18n, moduleDef, tModuleLabel, PickerPanel, Select } from '@bagel/kit';
  import { MODULE_VARIABLES } from '@bagel/kit/variables';

  const { t } = getI18n();
  let { onInsert }: { onInsert: (token: string) => void } = $props();
  let open = $state(false);
  let anchor = $state<HTMLButtonElement>();
  let moduleId = $state('valorant');
  const selected = $derived(MODULE_VARIABLES.find((v) => v.head === moduleId)!);
  const selectedName = $derived(t(`vars.${selected.id}.name`));
  const moduleLink = $derived(moduleDef(moduleId)?.href ?? (moduleDef(moduleId) ? `/modules/${moduleId}` : '/commands'));
  const parentId = $derived(moduleDef(moduleId)?.parent);
  const parentModule = $derived(parentId ? moduleDef(parentId) : undefined);

  function insert(token: string) {
    onInsert(token);
    open = false;
  }
</script>

<button type="button" class="picker" aria-haspopup="dialog" aria-expanded={open} bind:this={anchor} onclick={() => (open = !open)}>
  {t('commandEditor.pickModuleVariable')} <span aria-hidden="true">▾</span>
</button>

<PickerPanel {open} {anchor} label={t('commandEditor.pickModuleVariable')} width={340} maxHeight={420} onClose={() => (open = false)}>
  {#snippet children()}
    <label class="module-field">
      <span>{t('commandEditor.variableModule')}</span>
      <Select fill bind:value={moduleId} options={MODULE_VARIABLES.map((v) => ({ value: v.head, label: t(`vars.${v.id}.name`) }))} />
    </label>
    <p class="requirement">{t('commandEditor.moduleVariableRequires', { module: selectedName })} <a class="module-link" href={moduleLink}>{t('commandEditor.manageModules')}</a></p>
    {#if parentModule}<p class="requirement">{t('commandEditor.moduleVariableParentRequires', { module: tModuleLabel(t, parentModule) })}</p>{/if}
    <p class="context">{t('commandEditor.moduleVariableContext')}</p>
    <ul class="variables">
      {#each selected.forms as form (form.example)}
        <li><button type="button" class="variable" title={form.output} onclick={() => insert(form.example)}><code class="variable-code">{form.example}</code></button></li>
      {/each}
    </ul>
  {/snippet}
</PickerPanel>

<style>
  .picker { display: inline-flex; align-items: center; gap: 5px; font: 11.5px var(--bb-font-body); color: var(--bb-muted); background: transparent; border: 1px solid var(--rule, var(--bb-border)); border-radius: var(--bb-radius-pill); padding: 3px 10px; cursor: pointer; }
  .picker:hover, .picker[aria-expanded='true'] { color: var(--bb-white); border-color: var(--bb-border-strong); background: var(--glass-fill-2); }
  .module-field { display: flex; flex-direction: column; gap: 5px; font: 11px var(--bb-font-body); color: var(--bb-muted); }
  .requirement, .context { margin: 0; font: 12px/1.5 var(--bb-font-body); color: var(--bb-muted); }
  .module-link { color: var(--bb-green-glow); }
  .variables { list-style: none; margin: 0; padding: 0; display: grid; gap: 2px; }
  .variable { width: 100%; padding: 5px 8px; text-align: left; background: transparent; border: none; border-radius: var(--bb-radius-sm); color: var(--bb-white); cursor: pointer; }
  .variable:hover { background: var(--glass-fill-2); }
  .variable-code { font: 12px var(--bb-font-mono); overflow-wrap: anywhere; }
</style>
