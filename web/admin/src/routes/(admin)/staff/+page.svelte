<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  // The staff roster on the shared deck + inspector. One inspector serves both
  // "add a member" and "edit a member": they differ only in whether the Twitch
  // id is already known, and the previous page's separate add-card duplicated
  // the role picker and its grantable-role rule.
  //
  // No client-side last-owner rule. The users service owns that invariant and
  // answers with its own refusal; surfacing the server's message is the only
  // way the console cannot disagree with it about who the last owner is.
  import { untrack } from 'svelte';
  import { enhance } from '$app/forms';
  import type { SubmitFunction } from '@sveltejs/kit';
  import PageHead from '@bagel/ui/svelte/PageHead.svelte';
  import PageToolbar from '@bagel/ui/svelte/PageToolbar.svelte';
  import DeckList from '@bagel/ui/svelte/DeckList.svelte';
  import InspectorSurface from '@bagel/ui/svelte/InspectorSurface.svelte';
  import AlertBanner from '@bagel/ui/svelte/AlertBanner.svelte';
  import EmptyState from '@bagel/ui/svelte/EmptyState.svelte';
  import ConfirmDialog from '@bagel/ui/svelte/ConfirmDialog.svelte';
  import SkeletonStack from '@bagel/ui/svelte/SkeletonStack.svelte';
  import Skeleton from '@bagel/ui/svelte/Skeleton.svelte';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import { createInspector } from '@bagel/ui/svelte/inspector';
  import { createDiscardGuard } from '@bagel/ui/svelte/discard-guard';
  import { toast } from '@bagel/ui/svelte/toast';
  import { actionPayload, adminToastFailure } from '@bagel/kit';
  import { getI18n } from '@bagel/kit/i18n/context';
  import { canManage, grantableRoles, type AdminRole } from '$lib/access';
  import type { AdminAcct, AuditEntry } from '$lib/server/services';
  import StaffRow from '$lib/components/staff/StaffRow.svelte';
  import StaffEditor from '$lib/components/staff/StaffEditor.svelte';
  import {
    NEW_MEMBER,
    blankDraft,
    draftComplete,
    type StaffDraft
  } from '$lib/components/staff/staff-roles';
  import { STAFF_RANK } from '@bagel/kit/staff-role';

  let { data } = $props();

  const { t } = getI18n();
  const failed = adminToastFailure(toast);

  // Streamed roster -> local state, so mutations reconcile against the
  // authoritative roster echoed by the users service (never a local guess).
  let staff = $state<AdminAcct[]>([]);
  let loaded = $state(false);
  let degraded = $state(false);
  $effect(() => {
    let alive = true;
    loaded = false;
    data.roster.then((r) => {
      if (!alive) return;
      staff = r.staff;
      degraded = r.degraded;
      loaded = true;
    });
    return () => {
      alive = false;
    };
  });

  const me = $derived(data.me);
  const roles = $derived(grantableRoles(me.role));
  const roster = $derived(
    [...staff].sort(
      (a, b) => (STAFF_RANK[b.role] ?? 0) - (STAFF_RANK[a.role] ?? 0) || a.login.localeCompare(b.login)
    )
  );

  function manageable(member: AdminAcct): boolean {
    return canManage(me.role, member.role) && member.id !== Number(me.id);
  }

  // ── Inspector (shared controller over the pure state machine) ──────────────
  const inspector = createInspector<StaffDraft>();
  let draft = $state<StaffDraft | null>(null);
  let busy = $state(false);

  // Push editor changes into the machine for dirty tracking. The spread reads
  // each field so the effect re-runs on any field mutation; the edit itself is
  // untracked because it both reads and writes the machine's state, which would
  // otherwise make the effect depend on state it also mutates (an unsafe cycle).
  $effect(() => {
    const snap = draft ? { ...draft } : null;
    if (snap) untrack(() => inspector.edit(snap));
  });

  const creating = $derived(inspector.selectedId === NEW_MEMBER);
  const selected = $derived(
    creating ? null : (staff.find((s) => String(s.id) === inspector.selectedId) ?? null)
  );
  const canSave = $derived(inspector.dirty && !!draft && draftComplete(draft));

  const discard = createDiscardGuard(
    () => inspector.dirty,
    () => {
      inspector.reset();
      draft = null;
    }
  );

  function draftOf(m: AdminAcct): StaffDraft {
    return { userId: String(m.id), login: m.login, displayName: m.display_name, role: m.role };
  }

  function openNew() {
    discard.guard(() => {
      const blank = blankDraft(roles[0] ?? 'moderator');
      inspector.open(NEW_MEMBER, blank);
      draft = { ...blank };
    });
  }

  function openMember(m: AdminAcct) {
    if (inspector.selectedId === String(m.id)) {
      close();
      return;
    }
    discard.guard(() => {
      inspector.open(String(m.id), draftOf(m));
      draft = draftOf(m);
      loadHistory(m.id);
    });
  }

  function close() {
    discard.guard(() => {
      inspector.reset();
      draft = null;
    });
  }

  // ── Per-member history (lazy) ─────────────────────────────────────────────
  // Fetched on open so the roster page never ships the whole audit log.
  let history = $state<AuditEntry[] | null>(null);
  let historyError = $state('');

  async function loadHistory(id: number) {
    history = null;
    historyError = '';
    try {
      const res = await fetch(`/staff/history?actor_id=${id}`);
      if (!res.ok) throw new Error(`history fetch failed (${res.status})`);
      const body = (await res.json()) as { entries?: AuditEntry[]; error?: string };
      if (body.error) throw new Error(body.error);
      history = body.entries ?? [];
    } catch (e) {
      historyError = (e as Error).message;
      history = [];
    }
  }

  type ActionPayload = {
    action?: { ok: boolean; notice: string };
    staff?: AdminAcct[];
    error?: string;
  };

  // ── Save: immutable snapshot + request id; a late response can't cross rows ─
  const saveSubmit: SubmitFunction = () => {
    const requestId = inspector.beginSave()?.requestId;
    const wasCreating = creating;
    const before = staff.map((s) => ({ ...s }));
    busy = true;
    return async ({ result }) => {
      busy = false;
      const p = actionPayload<ActionPayload>(result);
      const ok = result.type === 'success' && p?.action?.ok === true;
      // applied is false when the selection moved on during the request, so a
      // late response for one member never mutates another's editor.
      const applied = requestId ? inspector.resolved(requestId, { type: ok ? 'success' : 'error' }) : false;
      if (!ok) {
        staff = before;
        failed(p, t('admin.staff.saveFailed'));
        return;
      }
      toast('ok', p!.action!.notice);
      if (p?.staff) staff = p.staff;
      // A create has no row to keep editing, so it closes; an edit stays open
      // and clean (Save does not close the inspector).
      if (wasCreating && applied) {
        inspector.reset();
        draft = null;
      }
    };
  };

  // ── Console access ────────────────────────────────────────────────────────
  // Off is the soft-remove and gets a confirmation; on re-upserts the row.
  let accessTarget = $state<AdminAcct | null>(null);
  let removeForm = $state<HTMLFormElement | null>(null);
  let restoreForm = $state<HTMLFormElement | null>(null);

  function setAccess(next: boolean) {
    if (!selected) return;
    if (next) {
      restoreForm?.requestSubmit();
      return;
    }
    accessTarget = selected;
  }

  const rosterSubmit = (after?: () => void): SubmitFunction => {
    return () => {
      busy = true;
      const before = staff.map((s) => ({ ...s }));
      return async ({ result }) => {
        busy = false;
        after?.();
        const p = actionPayload<ActionPayload>(result);
        if (result.type === 'success' && p?.action?.ok) {
          toast('ok', p.action.notice);
          if (p.staff) staff = p.staff;
          return;
        }
        staff = before;
        failed(p, t('admin.staff.saveFailed'));
      };
    };
  };

  const removeSubmit = rosterSubmit(() => (accessTarget = null));
  const restoreSubmit = rosterSubmit();
</script>

<section class="screen active">
  <PageHead eyebrow={t('admin.staff.eyebrow')} description={t('admin.staff.description')}>
    {t('admin.staff.titlePre')}<em>{t('admin.staff.titleEm')}</em>
  </PageHead>

  {#if degraded}
    <AlertBanner>{t('admin.staff.degraded')}</AlertBanner>
  {/if}

  <PageToolbar>
    {#snippet lead()}
      {#if loaded}
        <span class="count">
          {roster.length === 1
            ? t('admin.staff.countOne')
            : t('admin.staff.count', { n: String(roster.length) })}
        </span>
      {:else}
        <Skeleton variant="pill" width="110px" />
      {/if}
    {/snippet}
    {#snippet trail()}
      <Button variant="primary" onclick={openNew}>{t('admin.staff.add')}</Button>
    {/snippet}
  </PageToolbar>

  <div class="deck" class:inspecting={inspector.isOpen}>
    <DeckList>
      {#if !loaded}
        <SkeletonStack rows={3} height="60px" />
      {:else if roster.length}
        <ul class="bb-list" aria-label={t('admin.staff.listLabel')}>
          {#each roster as member (member.id)}
            <li>
              <StaffRow
                {member}
                isSelf={member.id === Number(me.id)}
                selected={inspector.selectedId === String(member.id)}
                controls="staff-inspector"
                onselect={() => openMember(member)}
              />
            </li>
          {/each}
        </ul>
      {:else}
        <EmptyState title={t('admin.staff.empty')} body={t('admin.staff.emptyBody')} />
      {/if}
    </DeckList>

    {#if inspector.isOpen && draft}
      <InspectorSurface
        open
        title={creating ? t('admin.staff.newTitle') : `@${selected?.login ?? ''}`}
        controls="staff-inspector"
        closeLabel={t('admin.close')}
        onClose={close}
      >
        <!-- Keyed on the selection so switching rows mounts a FRESH editor: it
             snapshots its draft at mount, so one reused instance would freeze
             the fields to the first member opened. -->
        {#key inspector.selectedId}
          <StaffEditor
            bind:draft={
              () => draft!,
              (v) => (draft = v)
            }
            member={selected}
            {creating}
            {roles}
            canToggleAccess={!!selected && manageable(selected)}
            status={inspector.status}
            dirty={inspector.dirty}
            {canSave}
            {busy}
            {history}
            {historyError}
            onCancel={close}
            onSubmit={saveSubmit}
            onAccess={setAccess}
          />
        {/key}
      </InspectorSurface>
    {/if}
  </div>
</section>

<ConfirmDialog
  open={accessTarget !== null}
  title={t('admin.staff.confirmDeactivateTitle')}
  body={accessTarget
    ? t('admin.staff.confirmDeactivateBody', { login: accessTarget.login })
    : undefined}
  confirmLabel={t('admin.staff.remove')}
  cancelLabel={t('common.cancel')}
  danger
  {busy}
  onCancel={() => (accessTarget = null)}
  onConfirm={() => removeForm?.requestSubmit()}
/>
<form method="POST" action="?/remove" use:enhance={removeSubmit} bind:this={removeForm} hidden>
  <input type="hidden" name="user_id" value={accessTarget?.id ?? ''} />
  <input type="hidden" name="target_role" value={accessTarget?.role ?? ''} />
</form>

<!-- Restoring access is an upsert at the member's committed role, not the
     draft's: turning the switch back on must not smuggle in an unsaved role
     change the operator has not pressed Save on. -->
<form method="POST" action="?/upsert" use:enhance={restoreSubmit} bind:this={restoreForm} hidden>
  <input type="hidden" name="user_id" value={selected?.id ?? ''} />
  <input type="hidden" name="login" value={selected?.login ?? ''} />
  <input type="hidden" name="display_name" value={selected?.display_name ?? ''} />
  <input type="hidden" name="role" value={selected?.role ?? ''} />
</form>

<ConfirmDialog
  open={discard.open}
  title={t('admin.unsaved')}
  confirmLabel={t('common.done')}
  cancelLabel={t('common.cancel')}
  onConfirm={discard.confirm}
  onCancel={discard.cancel}
/>

<style>
  .count {
    font-family: var(--bb-font-mono);
    font-size: 11.5px;
    color: var(--bb-muted);
  }

  .deck {
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    gap: 16px;
    align-items: start;
  }
  @media (min-width: 1080px) {
    .deck.inspecting {
      grid-template-columns: minmax(0, 1fr) 380px;
    }
  }
</style>
