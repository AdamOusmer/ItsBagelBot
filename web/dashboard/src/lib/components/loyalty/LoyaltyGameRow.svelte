<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { invalidateAll } from '$app/navigation';
  import { untrack } from 'svelte';
  import Disclosure from '@bagel/ui/svelte/Disclosure.svelte';
  import SwitchRow from '@bagel/ui/svelte/SwitchRow.svelte';
  import { toast } from '@bagel/ui/svelte/toast';
  import type { SaveState } from '@bagel/ui/svelte/SaveStatus.svelte';
  import { getI18n, namespaceReplyTemplate, tModuleLabel, tModuleTagline, type ActionOk, type ModuleDef, type ModuleField, type ModuleReply } from '@bagel/kit';
  import { postAction } from '#lib/client-actions.svelte';
  import ModuleSettingsForm from '#lib/components/modules/ModuleSettingsForm.svelte';
  import ReplyEditor from '#lib/components/modules/ReplyEditor.svelte';
  import ReplyRow from '#lib/components/modules/ReplyRow.svelte';

  let { def, settings, loyaltyOn }: {
    def: ModuleDef;
    settings: { enabled: boolean; config: Record<string, string>; revision: number };
    loyaltyOn: boolean;
  } = $props();

  const { t } = getI18n();
  const label = $derived(tModuleLabel(t, def));
  let config = $state({ ...untrack(() => settings.config) });
  let rev = $state(untrack(() => settings.revision));
  let enabled = $state(untrack(() => settings.enabled));
  let activeSave = $state<string | null>(null);
  let statuses = $state<Record<string, SaveState>>({});
  let selectedReply = $state<ModuleReply | null>(null);
  let message = $state('');
  const on = $derived(loyaltyOn && enabled);
  const disabled = $derived(activeSave !== null);

  function syncSettings(incoming: typeof settings) {
    config = { ...incoming.config };
    rev = incoming.revision;
    enabled = incoming.enabled;
  }

  $effect(() => {
    const incoming = settings;
    if (!activeSave && incoming.revision > rev) syncSettings(incoming);
  });

  async function save(key: string, partial: Record<string, string>, nextEnabled = on) {
    if (disabled) return false;
    const form = new FormData();
    form.set('name', def.id);
    form.set('expected_rev', String(rev));
    form.set('is_enabled', nextEnabled ? 'on' : '');
    form.set('partial', JSON.stringify(partial));
    activeSave = key;
    statuses[key] = 'saving';
    try {
      const result = await postAction<ActionOk & { rev?: number; conflict?: boolean }>('gameSettings', form);
      if (result?.ok) {
        rev = result.rev ?? rev;
        enabled = nextEnabled;
        config = { ...config, ...partial };
        statuses[key] = 'saved';
        return true;
      }
      if (result?.conflict) {
        await invalidateAll();
        syncSettings(settings);
      }
      statuses[key] = 'error';
      toast('danger', t(result?.conflict ? 'modules.patchConflict' : 'modules.saveFailed'));
      return false;
    } finally {
      activeSave = null;
    }
  }

  function saveSetting(field: ModuleField, raw: string) {
    const value = raw.trim();
    if (value === (config[field.key] ?? '')) return;
    void save(`setting:${field.key}`, { [field.key]: value });
  }

  function replyOn(reply: ModuleReply): boolean {
    const value = config[reply.enableKey ?? ''];
    if (value === 'on') return true;
    if (value === 'off') return false;
    return !reply.defaultOff;
  }

  function openReply(reply: ModuleReply) {
    if (activeSave) return;
    message = namespaceReplyTemplate(def.id, reply, config[reply.messageKey] ?? '');
    selectedReply = selectedReply === reply ? null : reply;
  }

  async function saveReply(reply: ModuleReply) {
    const value = namespaceReplyTemplate(def.id, reply, message);
    if (await save(reply.key, { [reply.messageKey]: value })) selectedReply = null;
  }
</script>

<Disclosure summary={label} id={def.id}>
  <SwitchRow
    control="end"
    fitCopy
    label={on ? t('modules.disableAria', { label }) : t('modules.enableAria', { label })}
    hint={tModuleTagline(t, def)}
    hintId={`${def.id}-game-hint`}
    checked={on}
    disabled={!loyaltyOn || disabled}
    busy={activeSave === 'module'}
    onCheckedChange={(checked) => save('module', {}, checked)}
  />
  <ModuleSettingsForm
    {def} {config}
    modStatus={statuses}
    locked={disabled}
    {saveSetting}
  />
  {#each def.replies as reply (reply.key)}
    <ReplyRow
      moduleId={def.id}
      {reply}
      message={config[reply.messageKey] ?? ''}
      status={statuses[reply.key] ?? 'idle'}
      expanded={selectedReply === reply}
      enabled={reply.enableKey ? replyOn(reply) : undefined}
      {disabled}
      onExpand={() => openReply(reply)}
      onToggle={() => reply.enableKey && save(reply.key, { [reply.enableKey]: replyOn(reply) ? 'off' : 'on' })}
    />
    {#if selectedReply === reply}
      <ReplyEditor moduleId={def.id} {reply} bind:message busy={activeSave === reply.key}
        onCancel={() => (selectedReply = null)} onSave={() => saveReply(reply)} />
    {/if}
  {/each}
</Disclosure>
