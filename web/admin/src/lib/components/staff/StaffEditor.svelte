<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import Input from '@bagel/ui/svelte/Input.svelte';
  import type { SubmitFunction } from '@sveltejs/kit';
  import { enhance } from '$app/forms';
  import Bolota from '@bagel/kit/components/Bolota.svelte';
  import RadioGroup from '@bagel/ui/svelte/RadioGroup.svelte';
  import Switch from '@bagel/ui/svelte/Switch.svelte';
  import Field from '@bagel/ui/svelte/Field.svelte';
  import Scroller from '@bagel/ui/svelte/Scroller.svelte';
  import EditorFooter from '@bagel/ui/svelte/EditorFooter.svelte';
  import Cluster from '@bagel/ui/svelte/Cluster.svelte';
  import Heading from '@bagel/ui/svelte/Heading.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import type { InspectorStatus } from '@bagel/ui/lib/inspector-machine';
  import { statusTone } from '@bagel/kit/status-tone';
  import { ago } from '@bagel/kit';
  import { getI18n } from '@bagel/kit/i18n/context';
  import type { AdminRole } from '$lib/access';
  import type { AdminAcct, AuditEntry } from '$lib/server/services';
  import StatusDot from '@bagel/ui/svelte/StatusDot.svelte';
  import { ROLE_LABEL, type StaffDraft } from './staff-roles';

  let {
    draft = $bindable(),
    member,
    creating,
    roles,
    canToggleAccess,
    status,
    dirty,
    canSave,
    busy,
    history,
    historyError,
    onCancel,
    onSubmit,
    onAccess
  }: {
    draft: StaffDraft;
    member: AdminAcct | null;
    creating: boolean;
    roles: readonly AdminRole[];
    canToggleAccess: boolean;
    status: InspectorStatus;
    dirty: boolean;
    canSave: boolean;
    busy: boolean;
    history: AuditEntry[] | null;
    historyError: string;
    onCancel: () => void;
    onSubmit: SubmitFunction;
    onAccess: (next: boolean) => void;
  } = $props();

  const { t } = getI18n();

  const roleOptions = $derived(roles.map((r) => ({ value: r, label: t(ROLE_LABEL[r]) })));
</script>

<form class="editor" method="POST" action="?/upsert" use:enhance={onSubmit}>
  <input type="hidden" name="user_id" value={draft.userId} />
  <input type="hidden" name="login" value={draft.login} />
  <input type="hidden" name="display_name" value={draft.displayName} />

  <Scroller fill padding="18px" smooth>
    <div class="body">
      {#if creating}
        <Field label={t('admin.staff.fieldUserId')}>
          <Input
            fill mono
            type="text"
            inputmode="numeric"
            pattern="[0-9]+"
            placeholder={t('admin.staff.fieldUserIdPlaceholder')}
            bind:value={draft.userId}
          />
        </Field>
        <Field label={t('admin.staff.fieldLogin')}>
          <Input
            fill mono
            type="text"
            placeholder={t('admin.staff.fieldLoginPlaceholder')}
            bind:value={draft.login}
          />
        </Field>
        <Field label={t('admin.staff.fieldDisplayName')}>
          <Input
            fill mono
            type="text"
            placeholder={t('admin.staff.fieldDisplayNamePlaceholder')}
            bind:value={draft.displayName}
          />
        </Field>
      {:else if member}
        <Cluster gap={3} nowrap>
          <Bolota name={member.display_name || member.login} size={44} active />
          <div>
            <Heading level={5} as="div" variant="title">{member.display_name || member.login}</Heading>
            <Text as="div" size="xs" mono tone="muted">
              {t('admin.staff.rowMeta', {
                login: member.login,
                id: String(member.id),
                when: ago(member.created_at)
              })}
            </Text>
          </div>
        </Cluster>
      {/if}

      <section class="block">
        <Heading level={3} variant="label">{t('admin.staff.roleLabel')}</Heading>
        <RadioGroup
          name="role"
          options={roleOptions}
          label={t('admin.staff.roleLabel')}
          bind:value={
            () => draft.role,
            (v) => (draft = { ...draft, role: v as AdminRole })
          }
        />
      </section>

      {#if member && canToggleAccess}
        <section class="block">
          <Heading level={3} variant="label">{t('admin.staff.activeLabel')}</Heading>
          <Switch
            checked={member.active}
            label={t('admin.staff.activeLabel')}
            describedby="staff-access-hint"
            disabled={busy}
            onchange={onAccess}
          />
          <Text size="xs" tone="muted" id="staff-access-hint">{t('admin.staff.activeHint')}</Text>
        </section>
      {/if}

      {#if member}
        <section class="block">
          <Heading level={3} variant="label">{t('admin.staff.historyTitle')}</Heading>
          {#if history === null}
            <Text size="xs" tone="muted">{t('admin.staff.historyLoading')}</Text>
          {:else if historyError}
            <Text size="xs" tone="danger">{historyError}</Text>
          {:else if history.length === 0}
            <Text size="xs" tone="muted">{t('admin.staff.historyEmpty')}</Text>
          {:else}
            <ul class="bb-list hist">
              {#each history as e (e.id)}
                <li class="hist-row">
                  <StatusDot tone={statusTone(e.ok ? 'online' : 'degraded')} />
                  <span class="hist-body">
                    <Text as="span" size="xs" mono truncate>{e.action}{e.target ? ` → ${e.target}` : ''}</Text>
                    {#if e.detail}<Text as="span" size="xs" mono tone="muted">{e.detail}</Text>{/if}
                    {#if !e.ok && e.error}<Text as="span" size="xs" mono tone="danger">{e.error}</Text>{/if}
                  </span>
                  <span class="hist-when"><Text as="span" size="xs" mono tone="muted">{ago(e.created_at)}</Text></span>
                </li>
              {/each}
            </ul>
          {/if}
        </section>
      {/if}
    </div>
  </Scroller>

  <EditorFooter
    {status}
    {dirty}
    {canSave}
    saveLabel={creating ? t('admin.staff.addCta') : t('common.save')}
    cancelLabel={t('common.cancel')}
    savingLabel={t('admin.saving')}
    savedLabel={t('admin.saved')}
    dirtyLabel={t('admin.unsaved')}
    errorLabel={t('admin.saveFailed')}
    {onCancel}
  />
</form>

<style>
  .editor {
    display: flex;
    flex-direction: column;
    min-height: 0;
    max-height: 100%;
  }
  .body {
    display: flex;
    flex-direction: column;
    gap: 18px;
  }

  .block {
    display: flex;
    flex-direction: column;
    gap: var(--bb-space-2);
  }

  .hist {
    display: flex;
    flex-direction: column;
    gap: var(--bb-space-2);
  }
  .hist-row {
    display: flex;
    align-items: flex-start;
    gap: var(--bb-space-2);
    min-width: 0;
  }
  .hist-body {
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-width: 0;
    flex: 1;
    overflow-wrap: anywhere;
  }
  .hist-when {
    white-space: nowrap;
  }
</style>
