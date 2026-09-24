<script lang="ts">
  import Input from '@bagel/ui/svelte/Input.svelte';
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import type { SubmitFunction } from '@sveltejs/kit';
  import { enhance } from '$app/forms';
  import Bolota from '@bagel/kit/components/Bolota.svelte';
  import RadioGroup from '@bagel/ui/svelte/RadioGroup.svelte';
  import Switch from '@bagel/ui/svelte/Switch.svelte';
  import Field from '@bagel/ui/svelte/Field.svelte';
  import Scroller from '@bagel/ui/svelte/Scroller.svelte';
  import EditorFooter from '@bagel/ui/svelte/EditorFooter.svelte';
  import type { InspectorStatus } from '@bagel/kit';
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
        <div class="ident">
          <Bolota name={member.display_name || member.login} size={44} active />
          <div>
            <div class="ident-name">{member.display_name || member.login}</div>
            <div class="ident-meta">
              {t('admin.staff.rowMeta', {
                login: member.login,
                id: String(member.id),
                when: ago(member.created_at)
              })}
            </div>
          </div>
        </div>
      {/if}

      <section class="block">
        <h3 class="block-label">{t('admin.staff.roleLabel')}</h3>
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
          <h3 class="block-label">{t('admin.staff.activeLabel')}</h3>
          <Switch
            checked={member.active}
            label={t('admin.staff.activeLabel')}
            describedby="staff-access-hint"
            disabled={busy}
            onchange={onAccess}
          />
          <p class="note" id="staff-access-hint">{t('admin.staff.activeHint')}</p>
        </section>
      {/if}

      {#if member}
        <section class="block">
          <h3 class="block-label">{t('admin.staff.historyTitle')}</h3>
          {#if history === null}
            <p class="note">{t('admin.staff.historyLoading')}</p>
          {:else if historyError}
            <p class="note err">{historyError}</p>
          {:else if history.length === 0}
            <p class="note">{t('admin.staff.historyEmpty')}</p>
          {:else}
            <ul class="bb-list hist">
              {#each history as e (e.id)}
                <li class="hist-row">
                  <StatusDot tone={statusTone(e.ok ? 'online' : 'degraded')} />
                  <span class="hist-body">
                    <span class="hist-act">{e.action}{e.target ? ` → ${e.target}` : ''}</span>
                    {#if e.detail}<span class="hist-detail">{e.detail}</span>{/if}
                    {#if !e.ok && e.error}<span class="hist-detail err">{e.error}</span>{/if}
                  </span>
                  <span class="hist-when">{ago(e.created_at)}</span>
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

  .ident {
    display: flex;
    align-items: center;
    gap: 12px;
  }
  .ident-name {
    font-family: var(--bb-font-display);
    font-weight: 700;
    font-size: 16px;
    color: var(--bb-white);
  }
  .ident-meta {
    font-family: var(--bb-font-mono);
    font-size: 11.5px;
    color: var(--bb-muted);
    margin-top: 2px;
  }

  .block {
    display: flex;
    flex-direction: column;
    gap: 9px;
  }
  .block-label {
    font-family: var(--bb-font-mono);
    font-size: 10px;
    font-weight: 400;
    letter-spacing: 0.12em;
    text-transform: uppercase;
    color: var(--bb-muted);
    margin: 0;
  }
  .note {
    font-family: var(--bb-font-body);
    font-size: 12px;
    color: var(--bb-muted);
    margin: 0;
  }
  .note.err {
    color: var(--bb-status-error);
  }

  .hist {
    display: flex;
    flex-direction: column;
    gap: 9px;
  }
  .hist-row {
    display: flex;
    align-items: flex-start;
    gap: 9px;
    min-width: 0;
  }
  .hist-body {
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-width: 0;
    flex: 1;
  }
  .hist-act {
    font-family: var(--bb-font-mono);
    font-size: 11.5px;
    color: var(--bb-white);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .hist-detail {
    font-family: var(--bb-font-mono);
    font-size: 10.5px;
    color: var(--bb-muted);
    word-break: break-word;
  }
  .hist-detail.err {
    color: var(--bb-status-error);
  }
  .hist-when {
    font-family: var(--bb-font-mono);
    font-size: 10px;
    color: var(--bb-muted);
    white-space: nowrap;
  }
</style>
