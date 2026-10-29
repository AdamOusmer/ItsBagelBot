<script lang="ts" module>
  import type { Perm } from '@bagel/kit';

  export const COMMAND_LEVELS: readonly Perm[] = ['sub', 'vip', 'mod', 'lead_mod', 'broadcaster'];

  export function commandLevel(saved: string | undefined, fallback: Perm = 'mod'): Perm {
    return COMMAND_LEVELS.includes(saved as Perm) ? (saved as Perm) : fallback;
  }
</script>

<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import AlertBanner from '@bagel/ui/svelte/AlertBanner.svelte';
  import Field from '@bagel/ui/svelte/Field.svelte';
  import SaveStatus, { type SaveState } from '@bagel/ui/svelte/SaveStatus.svelte';
  import Select from '@bagel/ui/svelte/Select.svelte';
  import { COMMAND_SAMPLES, getI18n, tModuleCommandPreview, tPerm, type ModuleCommandInfo } from '@bagel/kit';
  import ChatPreview from '#lib/components/commands/ChatPreview.svelte';

  let {
    moduleId,
    command,
    role,
    status = 'idle',
    disabled = false,
    onRole
  }: {
    moduleId: string;
    command: ModuleCommandInfo;
    role?: Perm;
    status?: SaveState;
    disabled?: boolean;
    onRole: (role: Perm) => void;
  } = $props();

  const { t } = getI18n();

  const name = $derived(command.trigger.split(' ')[0]);
  const lowTrust = $derived(role === 'sub' || role === 'vip');
  const samples = {
    user: COMMAND_SAMPLES.user,
    command: 'hype',
    role: 'vip',
    channel: 'Streamer',
    url: 'https://commands.itsbagelbot.com/user/streamer'
  };
</script>

<div class="editor">
  {#if role && command.permKey}
    <div class="access">
      <Field label={t('modules.commandAccess')} hint={t('modules.commandAccessHint')}>
        <Select
          fill
          {disabled}
          value={role}
          onchange={(e) => onRole(commandLevel(e.currentTarget.value))}
          options={COMMAND_LEVELS.map((p) => ({ value: p, label: tPerm(t, p) }))}
        />
      </Field>
      <SaveStatus state={status} />
    </div>
    {#if lowTrust}
      <AlertBanner tone="warning" role="status">{t('modules.commandAccessWarn', { level: tPerm(t, role) })}</AlertBanner>
    {/if}
  {/if}

  <ChatPreview
    kind="reply"
    {name}
    args={command.previewArgs ?? ''}
    {samples}
    response={tModuleCommandPreview(t, moduleId, command)}
  />
</div>

<style>
  .editor { display: flex; flex-direction: column; gap: 16px; padding: 4px 2px 2px; }
  .access { display: flex; align-items: flex-end; gap: 10px; }
  .access :global(> :first-child) { flex: 1; min-width: 0; }
</style>
