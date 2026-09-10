<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  // The staff inspector, in both of its modes. `creating` swaps the identity
  // fields in for the identity facts; everything below the fold -- role, console
  // access, history -- is the same control set, because "add a member" and
  // "change a member" differ only in whether the id is already known.
  //
  // The <form> wraps the fields AND the EditorFooter (the footer's Save is this
  // form's submit button), and the footer is a sibling after the scroll area so
  // it never scrolls out of view.
  import type { SubmitFunction } from '@sveltejs/kit';
  import { enhance } from '$app/forms';
  import Bolota from '@bagel/kit/components/Bolota.svelte';
  import RadioGroup from '@bagel/kit/components/RadioGroup.svelte';
  import Switch from '@bagel/kit/components/Switch.svelte';
  import Field from '@bagel/kit/components/Field.svelte';
  import Scroller from '@bagel/kit/components/Scroller.svelte';
  import EditorFooter from '@bagel/kit/components/EditorFooter.svelte';
  import type { InspectorStatus } from '@bagel/kit';
  import { statusTone } from '@bagel/kit/status-tone';
  import { ago } from '@bagel/kit';
  import { getI18n } from '@bagel/kit/i18n/context';
  import type { AdminRole } from '$lib/access';
  import type { AdminAcct, AuditEntry } from '$lib/server/services';
  import StatusDot from '../StatusDot.svelte';
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
    /** The committed row, absent while creating. */
    member: AdminAcct | null;
    creating: boolean;
    /** Roles this operator may grant, from canManage. */
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

  <Scroller fill padding="18px" data-lenis-prevent>
    <div class="body">
      {#if creating}
        <Field label={t('admin.staff.fieldUserId')}>
          <input
            class="text-input"
            type="text"
            inputmode="numeric"
            pattern="[0-9]+"
            placeholder={t('admin.staff.fieldUserIdPlaceholder')}
            bind:value={draft.userId}
          />
        </Field>
        <Field label={t('admin.staff.fieldLogin')}>
          <input
            class="text-input"
            type="text"
            placeholder={t('admin.staff.fieldLoginPlaceholder')}
            bind:value={draft.login}
          />
        </Field>
        <Field label={t('admin.staff.fieldDisplayName')}>
          <input
            class="text-input"
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
        <!-- Named `role`: these are real radio inputs, so the role posts with
             the form and the editor still works without JS. -->
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
        <!-- The roster has no hard delete: "remove" IS deactivate (the users
             service soft-removes and keeps the audit trail), so access and
             removal are one control rather than two buttons posting the same
             action. Turning it off routes through the confirmation the caller
             raises; turning it back on re-upserts the row at its current role. -->
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
