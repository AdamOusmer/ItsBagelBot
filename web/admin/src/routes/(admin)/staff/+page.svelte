<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import { getAbortSignal, tick, untrack } from 'svelte';
  import { MediaQuery } from 'svelte/reactivity';
  import { enhance, type SubmitFunction } from '$app/forms';
  import { invalidate } from '$app/navigation';
  import PageHead from '@bagel/ui/svelte/PageHead.svelte';
  import DeckLayout from '@bagel/ui/svelte/DeckLayout.svelte';
  import DeckList from '@bagel/ui/svelte/DeckList.svelte';
  import InspectorSurface from '@bagel/ui/svelte/InspectorSurface.svelte';
  import AlertBanner from '@bagel/ui/svelte/AlertBanner.svelte';
  import EmptyState from '@bagel/ui/svelte/EmptyState.svelte';
  import ConfirmDialog from '@bagel/ui/svelte/ConfirmDialog.svelte';
  import SkeletonStack from '@bagel/ui/svelte/SkeletonStack.svelte';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import { createInspector } from '@bagel/ui/svelte/inspector';
  import { createDiscardGuard } from '@bagel/ui/svelte/discard-guard';
  import { toast } from '@bagel/ui/svelte/toast';
  import { actionPayload, adminToastFailure } from '@bagel/kit';
  import { getI18n } from '@bagel/kit/i18n/context';
  import { STAFF_RANK } from '@bagel/kit/staff-role';
  import { canManage, grantableRoles } from '#lib/access';
  import type { AdminAcct, AuditEntry } from '#lib/server/services';
  import { applyStreamed } from '#lib/streamed-load';
  import StaffRow from '#lib/components/staff/StaffRow.svelte';
  import StaffEditor from '#lib/components/staff/StaffEditor.svelte';
  import StaffToolbar from '#lib/components/staff/StaffToolbar.svelte';
  import type { StaffHistoryResult } from '#lib/components/staff/StaffHistory.svelte';
  import {
    NEW_MEMBER,
    STAFF_ROSTER,
    blankDraft,
    draftComplete,
    type AccessFilter,
    type StaffDraft
  } from '#lib/components/staff/staff-roles';
  import type { RosterBundle } from './+page.server';

  let { data } = $props();

  const { t, locale } = getI18n();
  const collator = new Intl.Collator(locale);
  const failed = adminToastFailure(toast);
  const sheet = new MediaQuery('(max-width: 1079px)');
  const SKELETON_ROWS = 5;

  let roster = $state.raw<RosterBundle>();
  let rosterRevision = 0;
  let search = $state('');
  let access = $state<AccessFilter>('all');
  const searchKey = $derived(search.trim().replace(/^[@#]/, '').toLocaleLowerCase(locale));
  const ordered = $derived(
    (roster?.staff ?? []).toSorted((a, b) =>
      STAFF_RANK[b.role] - STAFF_RANK[a.role] || collator.compare(a.login, b.login)
    )
  );
  const matches = $derived(searchKey ? ordered.filter(matchesSearch) : ordered);
  const rows = $derived(access === 'all' ? matches : matches.filter((member) => member.active === (access === 'active')));
  const counts = $derived.by(() => {
    if (roster?.degraded !== false) return;
    const active = matches.filter((member) => member.active).length;
    return { all: matches.length, active, inactive: matches.length - active };
  });

  $effect(() => {
    const revision = rosterRevision;
    applyStreamed(data.roster, (result) => {
      if (revision === rosterRevision) roster = result;
    });
  });

  function matchesSearch(member: AdminAcct): boolean {
    return [member.login, member.display_name, String(member.id)].some((value) =>
      value.toLocaleLowerCase(locale).includes(searchKey)
    );
  }

  function isSelf(member: AdminAcct): boolean {
    return String(member.id) === data.me.id;
  }

  function manageable(member: AdminAcct): boolean {
    const actor = data.me.role;
    const peerAdmin = actor === 'admin' && member.role === 'admin';
    return canManage(actor, member.role) && !peerAdmin && !isSelf(member);
  }

  function retryRoster() {
    document.getElementById('staff-roster')?.focus();
    roster = undefined;
    void invalidate(STAFF_ROSTER);
  }

  const roles = $derived(grantableRoles(data.me.role));
  const inspector = createInspector<StaffDraft>();
  let draft = $state<StaffDraft | null>(null);
  let busy = $state(false);
  let opener: HTMLElement | null = null;

  $effect(() => {
    const snapshot = draft ? { ...draft } : null;
    if (snapshot) untrack(() => inspector.edit(snapshot));
  });

  const creating = $derived(inspector.selectedId === NEW_MEMBER);
  const selected = $derived((roster?.staff ?? []).find((member) => String(member.id) === inspector.selectedId));
  const canManageSelected = $derived(selected ? manageable(selected) : false);
  const canEditRole = $derived(creating || (canManageSelected && selected?.role !== 'owner'));
  const canToggleAccess = $derived(canManageSelected && (selected?.active || selected?.role !== 'owner'));
  const validDraft = $derived(draft !== null && draftComplete(draft));
  const editableDraft = $derived(canEditRole && inspector.dirty && validDraft);
  const canSave = $derived(!busy && editableDraft);
  const discard = createDiscardGuard(() => inspector.dirty);

  function focusAfterClose() {
    const previous = opener;
    opener = null;
    previous?.focus();
    if (document.activeElement === previous && previous !== document.body) return;
    document.getElementById('staff-search')?.focus();
  }

  function resetEditor() {
    inspector.reset();
    draft = null;
    void tick().then(focusAfterClose);
  }

  function activeControl(): HTMLElement | null {
    return document.activeElement instanceof HTMLElement ? document.activeElement : null;
  }

  function openEditor(id: string, value: StaffDraft) {
    if (busy) return;
    const active = activeControl();
    discard.guard(() => {
      opener = active;
      inspector.open(id, value);
      draft = { ...value };
      if (!sheet.current) void tick().then(() => focusEditor(id));
    });
  }

  function focusEditor(id: string) {
    document.getElementById(id === NEW_MEMBER ? 'staff-user-id' : 'staff-member-name')?.focus();
  }

  function openNew() {
    openEditor(NEW_MEMBER, blankDraft(roles[0] ?? 'moderator'));
  }

  function openMember(member: AdminAcct) {
    const userId = String(member.id);
    if (inspector.selectedId === userId) {
      close();
      return;
    }
    openEditor(userId, { userId, login: member.login, displayName: member.display_name, role: member.role });
  }

  function close() {
    if (!busy) discard.guard(resetEditor);
  }

  let history = $state.raw<{ actorId: number; revision: number; result: Promise<StaffHistoryResult> }>();
  let historyRetry = $state(0);
  const actorId = $derived(selected?.id);

  $effect(() => {
    history = actorId === undefined
      ? undefined
      : { actorId, revision: historyRetry, result: readHistory(actorId, getAbortSignal()) };
  });

  async function readHistory(id: number, signal: AbortSignal): Promise<StaffHistoryResult> {
    try {
      const response = await fetch(`/staff/history?actor_id=${id}`, { signal });
      if (!response.ok) return { entries: [], error: true };
      const body = (await response.json()) as { entries?: AuditEntry[]; error?: string };
      return { entries: body.entries ?? [], error: Boolean(body.error) };
    } catch {
      return { entries: [], error: true };
    }
  }

  type ActionPayload = {
    action?: { ok: boolean; notice: string };
    staff?: AdminAcct[];
    error?: string;
  };

  function applyAction(result: { type: string }): boolean {
    const payload = actionPayload<ActionPayload>(result);
    if (result.type !== 'success' || !payload?.action?.ok) {
      failed(payload, t('admin.staff.saveFailed'));
      return false;
    }
    if (payload.staff) {
      ++rosterRevision;
      roster = { staff: payload.staff, degraded: false };
    }
    toast('success', payload.action.notice);
    return true;
  }

  const saveSubmit: SubmitFunction = ({ cancel }) => {
    if (!canSave) return cancel();
    const request = inspector.beginSave();
    if (!request) return cancel();
    busy = true;
    return ({ result }) => {
      busy = false;
      const ok = applyAction(result);
      const applied = inspector.resolved(request.requestId, { type: ok ? 'success' : 'error' });
      if (creating && ok && applied) resetEditor();
    };
  };

  let accessTarget = $state<AdminAcct | null>(null);
  let removeForm: HTMLFormElement;
  let restoreForm: HTMLFormElement;
  const confirming = $derived(accessTarget !== null || discard.open);
  const inspecting = $derived(creating || selected !== undefined);
  const showInspector = $derived(inspecting && !(sheet.current && confirming));

  function setAccess(next: boolean) {
    if (!selected || !canToggleAccess || busy) return;
    if (next) restoreForm.requestSubmit();
    else accessTarget = selected;
  }

  const accessSubmit: SubmitFunction = ({ cancel }) => {
    if (busy) return cancel();
    busy = true;
    return ({ result }) => {
      busy = false;
      accessTarget = null;
      applyAction(result);
    };
  };
</script>

<section class="screen active">
  <PageHead description={t('admin.staff.description')}>
    {t('admin.staff.titlePre')}<em>{t('admin.staff.titleEm')}</em>
  </PageHead>

  <StaffToolbar
    bind:search
    bind:access
    {counts}
    {busy}
    onAdd={openNew}
  />

  <DeckLayout {inspecting} width="380px">
    <DeckList>
      <div id="staff-roster" class="staff-roster" aria-busy={!roster}>
        {#if !roster}
          <div class="roster-loading" role="status" aria-label={t('common.loading')}>
            <SkeletonStack rows={SKELETON_ROWS} height="calc(var(--staff-row-height) - 10px)" />
          </div>
        {:else if roster.degraded}
          <div class="roster-error">
            <AlertBanner>
              {t('admin.staff.degraded')}
              {#snippet actions()}
                <Button variant="ghost" onclick={retryRoster}>{t('common.retry')}</Button>
              {/snippet}
            </AlertBanner>
          </div>
        {:else if rows.length}
          <ul class="roster-list" aria-label={t('admin.staff.listLabel')}>
            {#each rows as member (member.id)}
              {@const selectedMember = inspector.selectedId === String(member.id)}
              <StaffRow
                {member}
                isSelf={isSelf(member)}
                selected={selectedMember}
                expanded={selectedMember && showInspector}
                onselect={() => openMember(member)}
              />
            {/each}
          </ul>
        {:else if searchKey || access !== 'all'}
          <EmptyState title={t('admin.staff.emptyMatch')} body={t('admin.staff.emptyMatchBody')} />
        {:else}
          <EmptyState title={t('admin.staff.empty')} body={t('admin.staff.emptyBody')} />
        {/if}
      </div>
    </DeckList>

    <InspectorSurface
      open={showInspector}
      title={creating ? t('admin.staff.newTitle') : `@${selected?.login ?? ''}`}
      controls="staff-inspector"
      closeLabel={t('admin.close')}
      onClose={close}
    >
      {#if draft}
        {#key inspector.selectedId}
          <StaffEditor
            bind:draft={
              () => draft!,
              (value) => (draft = value)
            }
            member={selected}
            {roles}
            {canEditRole}
            {canToggleAccess}
            status={inspector.status}
            dirty={inspector.dirty}
            {canSave}
            {busy}
            history={history?.actorId === actorId && history?.revision === historyRetry ? history?.result : undefined}
            onRetryHistory={() => ++historyRetry}
            onCancel={close}
            onSubmit={saveSubmit}
            onAccess={setAccess}
          />
        {/key}
      {/if}
    </InspectorSurface>
  </DeckLayout>
</section>

<ConfirmDialog
  open={accessTarget !== null}
  title={t('admin.staff.confirmDeactivateTitle')}
  body={accessTarget ? t('admin.staff.confirmDeactivateBody', { login: accessTarget.login }) : undefined}
  confirmLabel={t('admin.staff.remove')}
  cancelLabel={t('common.cancel')}
  {busy}
  onCancel={() => (accessTarget = null)}
  onConfirm={() => removeForm.requestSubmit()}
  tone="danger"
/>
<form method="POST" action="?/remove" use:enhance={accessSubmit} bind:this={removeForm} hidden>
  <input type="hidden" name="user_id" value={accessTarget?.id ?? ''} />
  <input type="hidden" name="target_role" value={accessTarget?.role ?? ''} />
</form>

<form method="POST" action="?/upsert" use:enhance={accessSubmit} bind:this={restoreForm} hidden>
  <input type="hidden" name="user_id" value={selected?.id ?? ''} />
  <input type="hidden" name="login" value={selected?.login ?? ''} />
  <input type="hidden" name="display_name" value={selected?.display_name ?? ''} />
  <input type="hidden" name="role" value={selected?.role ?? ''} />
</form>

<ConfirmDialog
  open={discard.open}
  title={t('admin.unsaved')}
  confirmLabel={t('admin.staff.discard')}
  cancelLabel={t('common.cancel')}
  onConfirm={discard.confirm}
  onCancel={discard.cancel}
/>

<style>
  .screen :global(.staff-roster) {
    --staff-row-height: 4rem;
  }

  .roster-loading {
    padding-block: 5px;
  }

  .roster-list {
    margin: 0;
    padding: 0;
    list-style: none;
  }

  .roster-error {
    padding: var(--bb-space-3);
  }
</style>
