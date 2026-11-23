<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import { enhance, type SubmitFunction } from '$app/forms';
  import '@bagel/ui/styles/elements/collapse.css';
  import Input from '@bagel/ui/svelte/Input.svelte';
  import Field from '@bagel/ui/svelte/Field.svelte';
  import Select from '@bagel/ui/svelte/Select.svelte';
  import Switch from '@bagel/ui/svelte/Switch.svelte';
  import Scroller from '@bagel/ui/svelte/Scroller.svelte';
  import EditorFooter from '@bagel/ui/svelte/EditorFooter.svelte';
  import Cluster from '@bagel/ui/svelte/Cluster.svelte';
  import Stack from '@bagel/ui/svelte/Stack.svelte';
  import Heading from '@bagel/ui/svelte/Heading.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import Bolota from '@bagel/ui/svelte/Bolota.svelte';
  import type { InspectorStatus } from '@bagel/ui/lib/inspector-machine';
  import { getI18n } from '@bagel/kit/i18n/context';
  import type { AdminRole } from '#lib/access';
  import type { AdminAcct } from '#lib/server/services';
  import StaffHistory, { type StaffHistoryResult } from './StaffHistory.svelte';
  import { ROLE_LABEL, type StaffDraft } from './staff-roles';

  let {
    draft = $bindable(),
    member,
    roles,
    canEditRole,
    canToggleAccess,
    status,
    dirty,
    canSave,
    busy,
    history,
    onRetryHistory,
    onCancel,
    onSubmit,
    onAccess
  }: {
    draft: StaffDraft;
    member: AdminAcct | undefined;
    roles: readonly AdminRole[];
    canEditRole: boolean;
    canToggleAccess: boolean;
    status: InspectorStatus;
    dirty: boolean;
    canSave: boolean;
    busy: boolean;
    history?: Promise<StaffHistoryResult>;
    onRetryHistory: () => void;
    onCancel: () => void;
    onSubmit: SubmitFunction;
    onAccess: (next: boolean) => void;
  } = $props();

  const { t, locale } = getI18n();
  const dateFormat = new Intl.DateTimeFormat(locale, { dateStyle: 'medium', timeZone: 'UTC' });
  const visibleRoles = $derived(member && !canEditRole ? [member.role] : roles);
  const roleOptions = $derived(visibleRoles.map((role) => ({ value: role, label: t(ROLE_LABEL[role]) })));
</script>

<form class="editor" method="POST" action="?/upsert" use:enhance={onSubmit}>
  <Scroller fill padding="var(--bb-space-4)">
    <Stack gap={4}>
      {#if member}
        <input type="hidden" name="user_id" value={draft.userId} />
        <input type="hidden" name="login" value={draft.login} />
        <input type="hidden" name="display_name" value={draft.displayName} />
        <Cluster gap={3} nowrap>
          <Bolota name={member.display_name || member.login} size={40} active cycle={false} />
          <div class="identity">
            <Heading level={2} variant="title" id="staff-member-name" tabindex={-1}>{member.display_name || member.login}</Heading>
            <Text as="div" size="xs" mono tone="muted" truncate>
              {t('admin.staff.rowMeta', { login: member.login, id: String(member.id) })}
            </Text>
            <Text as="div" size="xs" tone="muted">
              {t('admin.staff.added', { date: dateFormat.format(new Date(member.created_at)) })}
            </Text>
          </div>
        </Cluster>
      {:else}
        <Field label={t('admin.staff.fieldUserId')} fit>
          <Input id="staff-user-id" name="user_id" fill mono inputmode="numeric" pattern="[0-9]+" required
            placeholder={t('admin.staff.fieldUserIdPlaceholder')} bind:value={draft.userId} disabled={busy} />
        </Field>
        <Field label={t('admin.staff.fieldLogin')} fit>
          <Input name="login" fill required placeholder={t('admin.staff.fieldLoginPlaceholder')}
            bind:value={draft.login} disabled={busy} />
        </Field>
        <Field label={t('admin.staff.fieldDisplayName')} fit>
          <Input name="display_name" fill placeholder={t('admin.staff.fieldDisplayNamePlaceholder')}
            bind:value={draft.displayName} disabled={busy} />
        </Field>
      {/if}

      <Stack gap={1}>
        <Field label={t('admin.staff.roleLabel')} fit>
          <Select name="role" label={t('admin.staff.roleLabel')} options={roleOptions} fill required
            disabled={busy || !canEditRole} aria-describedby={canEditRole ? undefined : 'staff-role-hint'}
            bind:value={() => canEditRole ? draft.role : member?.role ?? draft.role,
              (value) => (draft = { ...draft, role: value as AdminRole })} />
        </Field>
        {#if member}
          <div class="bb-collapse" style="--bb-collapse-gap: var(--bb-space-1)" class:is-open={!canEditRole} aria-hidden={canEditRole} inert={canEditRole} id="staff-role-hint">
            <div>
              <div class="role-hint bb-fit-box">
                <Text as="span" size="xs" tone="muted"><bb-fit><span data-fit>{t('admin.staff.roleRestricted')}</span></bb-fit></Text>
              </div>
            </div>
          </div>
        {/if}
      </Stack>

      {#if member}
        <Stack as="section" gap={2} aria-labelledby="staff-access-title">
          <Cluster justify="between" nowrap>
            <Heading id="staff-access-title" level={3} variant="label">{t('admin.staff.activeLabel')}</Heading>
            <Switch
              bind:checked={() => member?.active ?? false, onAccess}
              label={t('admin.staff.activeLabel')}
              describedby="staff-access-hint"
              disabled={busy || !canToggleAccess}
            />
          </Cluster>
          <div class="access-hint bb-fit-box" id="staff-access-hint">
            <Text as="div" size="xs" tone="muted">
              <bb-fit mode="block"><span data-fit>{t(canToggleAccess ? 'admin.staff.activeHint' : 'admin.staff.accessRestricted')}</span></bb-fit>
            </Text>
          </div>
        </Stack>

        <StaffHistory pending={history} onRetry={onRetryHistory} />
      {/if}
    </Stack>
  </Scroller>

  <EditorFooter
    {status}
    {dirty}
    {canSave}
    saveLabel={member ? t('common.save') : t('admin.staff.addCta')}
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
    min-block-size: 0;
    max-block-size: 100%;
  }

  .identity {
    min-inline-size: 0;
    overflow-wrap: anywhere;
  }

  .access-hint {
    block-size: 2.25rem;
  }
</style>
