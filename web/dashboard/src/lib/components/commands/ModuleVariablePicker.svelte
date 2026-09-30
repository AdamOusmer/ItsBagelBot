<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import Chip from '@bagel/ui/svelte/Chip.svelte';
  import Field from '@bagel/ui/svelte/Field.svelte';
  import Icon from '@bagel/ui/svelte/Icon.svelte';
  import PickerOption from '@bagel/ui/svelte/PickerOption.svelte';
  import PickerPanel from '@bagel/ui/svelte/PickerPanel.svelte';
  import Select from '@bagel/ui/svelte/Select.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import TextLink from '@bagel/ui/svelte/TextLink.svelte';
  import { getI18n, moduleDef, tModuleLabel } from '@bagel/kit';
  import { MODULE_VARIABLES } from '@bagel/kit/variables';

  const { t } = getI18n();
  let { onInsert }: { onInsert: (token: string) => void } = $props();
  let open = $state(false);
  let anchor = $state<HTMLElement>();
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

  function setAnchor(node: HTMLElement) {
    anchor = node;
  }
</script>

<Chip tone="muted" aria-haspopup="dialog" aria-expanded={open} onclick={() => (open = !open)} {@attach setAnchor}>
  {t('commandEditor.pickModuleVariable')} <Icon name="chevron" />
</Chip>

<PickerPanel {open} {anchor} label={t('commandEditor.pickModuleVariable')} width={340} maxHeight={420} onClose={() => (open = false)}>
  {#snippet children()}
    <div class="body">
      <Field label={t('commandEditor.variableModule')}>
        <Select fill bind:value={moduleId} options={MODULE_VARIABLES.map((v) => ({ value: v.head, label: t(`vars.${v.id}.name`) }))} />
      </Field>
      <Text size="xs" tone="muted">{t('commandEditor.moduleVariableRequires', { module: selectedName })} <TextLink variant="inline" href={moduleLink}>{t('commandEditor.manageModules')}</TextLink></Text>
      {#if parentModule}<Text size="xs" tone="muted">{t('commandEditor.moduleVariableParentRequires', { module: tModuleLabel(t, parentModule) })}</Text>{/if}
      <Text size="xs" tone="muted">{t('commandEditor.moduleVariableContext')}</Text>
      <ul class="variables">
        {#each selected.forms as form (form.example)}
          <PickerOption as="li" label={form.example} title={form.output} onclick={() => insert(form.example)} />
        {/each}
      </ul>
    </div>
  {/snippet}
</PickerPanel>

<style>
  .body { display: contents; --field-gap: 5px; --field-mb: 0; }
  .variables { list-style: none; margin: 0; padding: 0; display: grid; gap: 2px; }
</style>
