<script lang="ts">
  import { Icon, Modal, PageHead, Card, ConfirmDialog, EmptyState, toast, getI18n } from '@bagel/shared';
  import { page } from '$app/state';
  import { enhance } from '$app/forms';
  import CheckButton from '$lib/components/CheckButton.svelte';
  import LangSwitch from '$lib/components/LangSwitch.svelte';
  import type { DelegationGrant, NotificationWire } from '$lib/server/services';

  let { data, form } = $props();

  const { t } = getI18n();

  const notifications = $derived((data.notifications ?? []) as NotificationWire[]);
  const levelLabel = (l: string) => l.charAt(0).toUpperCase() + l.slice(1);

  const createdGrant = $derived(form?.createdGrant as DelegationGrant | undefined);
  const given = $derived.by<DelegationGrant[]>(() => {
    const grants = (data.given ?? []) as DelegationGrant[];
    if (!createdGrant || grants.some((g) => g.token === createdGrant.token)) return grants;
    return [createdGrant, ...grants];
  });
  const received = $derived(
    (data.received ?? []) as { owner_user_id: string; owner_login: string; sections: string[] }[]
  );
  const origin = $derived(page.url.origin);

  // Sections an owner can grant (from the server so it stays in one place).
  const grantable = $derived((data.grantableSections ?? ['commands', 'modules', 'billing']) as string[]);
  function sectionLabel(sec: string): string {
    return sec === 'modules' ? t('settings.modules') : sec === 'billing' ? t('settings.billing') : t('settings.commands');
  }

  // Which grant's access is being edited inline (add/remove sections).
  let editingToken = $state<string | null>(null);

  function linkFor(token: string): string {
    return `${origin}/delegate/accept?t=${token}`;
  }

  // One-tap copy with per-grant "copied" feedback (lifecycle: created -> link
  // copied -> consumed).
  let copied = $state<Record<string, boolean>>({});
  async function copy(token: string) {
    try {
      await navigator.clipboard.writeText(linkFor(token));
      copied = { ...copied, [token]: true };
      toast('ok', t('settings.toastInviteCopied'));
      setTimeout(() => (copied = { ...copied, [token]: false }), 4000);
    } catch {
      toast('err', t('settings.toastClipboardBlocked'));
    }
  }

  // Surface action results as toasts (replaces the old inline banners).
  // svelte-ignore state_referenced_locally
  let lastForm: unknown = form;
  $effect(() => {
    if (form === lastForm) return;
    lastForm = form;
    if (!form) return;
    if (form.error) toast('err', String(form.error));
    else if (form.ok && form.action === 'created') toast('ok', t('settings.toastCreated'));
    else if (form.ok && form.action === 'updated') toast('ok', t('settings.toastAccessUpdated'));
    else if (form.ok && form.action === 'revoked') toast('ok', t('settings.toastRevoked'));
    else if (form.ok && form.action === 'opted_out') toast('ok', t('settings.toastOptedOut'));
  });

  // Revoke is irreversible (tokens are single-use), so it gets a confirm
  // dialog rather than optimistic apply + undo.
  let revokeTarget = $state<DelegationGrant | null>(null);
  let revokeForm = $state<HTMLFormElement | null>(null);

  // Delete confirm modal: the box must be checked before Delete enables.
  let deleteOpen = $state(false);
  let ack = $state(false);
  const openDelete = () => {
    ack = false;
    deleteOpen = true;
  };
  const closeDelete = () => (deleteOpen = false);
</script>

<section class="screen active">
  <PageHead eyebrow={t('settings.eyebrow')} description={t('settings.description')}>{t('settings.titlePre')}<em>{t('settings.titleEm')}</em></PageHead>

  <!-- ACCOUNT -->
  <Card class="settings-card">
    <h2>{t('settings.account')}</h2>
    <div class="row">
      <div>
        <b>{t('settings.language')}</b>
        <p class="hint">{t('settings.languageHint')}</p>
      </div>
      <LangSwitch />
    </div>
    <div class="row">
      <div>
        <b>{t('settings.reconnectTwitch')}</b>
        <p class="hint">{t('settings.reconnectTwitchHint')}</p>
      </div>
      <a class="btn ghost" href="/auth/login"><Icon name="power" size={14} /> {t('common.reconnect')}</a>
    </div>
    <div class="row">
      <div>
        <b>{t('settings.deleteAccount')}</b>
        <p class="hint">{t('settings.deleteAccountHint')}</p>
      </div>
      <button type="button" class="btn ghost danger" onclick={openDelete}>{t('settings.deleteAccount')}</button>
    </div>
  </Card>

  <!-- ACCESS YOU GRANTED -->
  <Card class="settings-card">
    <h2>{t('settings.accessGranted')}</h2>
    <p class="hint">
      {t('settings.accessGrantedHint')}
    </p>

    {#if given.length === 0}
      <EmptyState icon="link" title={t('settings.noShareLinks')} body={t('settings.noShareLinksBody')} />
    {:else}
      <div class="grants">
        {#each given as g (g.token)}
          <div class="grant {g.consumed ? 'consumed' : 'pending'}">
            <div class="grant-top">
              <span class="lifecycle">
                <span class="stage done">{t('settings.stageCreated')}</span>
                <span class="sep">→</span>
                <span class="stage {g.consumed || copied[g.token] ? 'done' : ''}">{t('settings.stageLinkShared')}</span>
                <span class="sep">→</span>
                <span class="stage {g.consumed ? 'done live' : ''}">
                  {g.consumed ? t('settings.stageInUse', { login: g.delegate_login || t('settings.unknown') }) : t('settings.stageWaiting')}
                </span>
              </span>
              <button type="button" class="btn ghost sm danger" onclick={() => (revokeTarget = g)}>{t('common.revoke')}</button>
            </div>
            {#if editingToken === g.token}
              <form
                method="POST"
                action="?/updateSections"
                class="grant-edit"
                use:enhance={() =>
                  async ({ result, update }) => {
                    await update();
                    if (result.type === 'success') editingToken = null;
                  }}
              >
                <input type="hidden" name="token" value={g.token} />
                <div class="section-picks compact">
                  {#each grantable as sec (sec)}
                    <CheckButton name={sec} checked={g.sections.includes(sec)} label={sectionLabel(sec)} />
                  {/each}
                </div>
                <div class="grant-edit-actions">
                  <button type="button" class="btn ghost sm" onclick={() => (editingToken = null)}>{t('common.cancel')}</button>
                  <button type="submit" class="btn primary sm"><Icon name="check" size={12} /> {t('common.save')}</button>
                </div>
              </form>
            {:else}
              <div class="grant-sections">
                {#each g.sections as s (s)}<span class="section-chip">{sectionLabel(s)}</span>{/each}
                <button type="button" class="btn ghost sm edit-access" onclick={() => (editingToken = g.token)}>{t('settings.editAccess')}</button>
              </div>
            {/if}
            {#if !g.consumed}
              <div class="grant-link">
                <code>{linkFor(g.token)}</code>
                <button type="button" class="btn ghost sm" onclick={() => copy(g.token)} aria-label={t('settings.copyLinkAria')}>
                  <Icon name={copied[g.token] ? 'check' : 'link'} size={12} />
                  {copied[g.token] ? t('common.copied') : t('common.copy')}
                </button>
              </div>
            {/if}
          </div>
        {/each}
      </div>
    {/if}

    <form method="POST" action="?/create" class="create" use:enhance>
      <h3>{t('settings.newShareLink')}</h3>
      <p class="hint">{t('settings.newShareLinkHint')}</p>
      <div class="section-picks">
        {#each grantable as sec (sec)}
          <CheckButton name={sec} checked={sec === 'commands'} label={sectionLabel(sec)} />
        {/each}
      </div>
      <button class="btn primary" type="submit"><Icon name="link" size={14} /> {t('common.generate')}</button>
    </form>
  </Card>

  <!-- NOTIFICATIONS: the bell dropdown's "view all" target — a compact history
       section rather than a dedicated page. -->
  <Card class="settings-card" id="notifications">
    <h2>{t('settings.notifications')}</h2>
    {#if notifications.length === 0}
      <p class="hint">{t('settings.notificationsEmpty')}</p>
    {:else}
      <div class="notif-list">
        {#each notifications as n (n.id)}
          <div class="notif-item" class:unread={!n.read}>
            <span class="level {n.level}">{levelLabel(n.level)}</span>
            <div class="notif-text">
              <b>{n.title}</b>
              <p>{n.body}</p>
              <span class="notif-meta">{new Date(n.created_at).toLocaleString()}</span>
            </div>
            {#if !n.read}
              <form method="POST" action="?/markRead" use:enhance>
                <input type="hidden" name="id" value={n.id} />
                <button type="submit" class="btn ghost sm"><Icon name="check" size={12} /> {t('common.read')}</button>
              </form>
            {/if}
          </div>
        {/each}
      </div>
    {/if}
  </Card>

  <!-- SHARED WITH YOU -->
  <Card class="settings-card">
    <h2>{t('settings.sharedWithYou')}</h2>
    {#if received.length === 0}
      <EmptyState icon="overview" title={t('settings.nothingShared')} body={t('settings.nothingSharedBody')} />
    {:else}
      <div class="grants">
        {#each received as r (r.owner_user_id)}
          <div class="grant consumed">
            <div class="grant-top">
              <span class="owner"><Icon name="overview" size={14} /> {r.owner_login}</span>
              <span class="actions">
                <a class="btn ghost sm" href={`/delegate/enter?owner=${r.owner_user_id}`}>{t('common.open')}</a>
                <form method="POST" action="?/optOut" use:enhance>
                  <input type="hidden" name="owner_user_id" value={r.owner_user_id} />
                  <button type="submit" class="btn ghost sm danger">{t('common.leave')}</button>
                </form>
              </span>
            </div>
            <div class="grant-sections">
              {#each r.sections as s (s)}<span class="section-chip">{sectionLabel(s)}</span>{/each}
            </div>
          </div>
        {/each}
      </div>
    {/if}
  </Card>
</section>

<!-- Revoke confirm -->
<ConfirmDialog
  open={revokeTarget !== null}
  title={t('settings.revokeTitle')}
  body={revokeTarget?.consumed
    ? t('settings.revokeBodyConsumed', { login: revokeTarget.delegate_login || t('settings.revokeBodyDelegate') })
    : t('settings.revokeBodyPending')}
  confirmLabel={t('common.revoke')}
  cancelLabel={t('common.cancel')}
  danger
  onCancel={() => (revokeTarget = null)}
  onConfirm={() => {
    revokeForm?.requestSubmit();
    revokeTarget = null;
  }}
/>
{#if revokeTarget}
  <form method="POST" action="?/revoke" use:enhance bind:this={revokeForm} hidden>
    <input type="hidden" name="token" value={revokeTarget.token} />
  </form>
{/if}

<!-- Delete confirm modal -->
<Modal open={deleteOpen} title={t('settings.deleteTitle')} closeModal={closeDelete}>
  <p class="modal-body">{t('settings.deleteBody')}</p>
  <div class="ack">
    <CheckButton bind:checked={ack} label={t('settings.deleteAck')} />
  </div>
  <form method="POST" action="?/delete" use:enhance class="modal-actions">
    <button type="button" class="btn ghost" onclick={closeDelete}>{t('common.cancel')}</button>
    <button type="submit" class="btn delete-btn" disabled={!ack}>{t('settings.deleteAccount')}</button>
  </form>
</Modal>

<style>
  :global(.settings-card) {
    margin-top: 18px;
  }
  h2 { margin: 0 0 6px; font-size: 16px; }
  h3 { margin: 0 0 6px; font-size: 14px; }
  .hint { color: var(--bb-muted, #998f82); font-size: 13px; margin: 0 0 12px; }
  .row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 16px;
    padding: 12px 0;
    border-top: 1px solid var(--bb-line, rgba(255, 255, 255, 0.06));
  }
  .row:first-of-type { border-top: none; }
  .row b { font-size: 14px; }
  .row .hint { margin: 4px 0 0; }
  .create { margin-top: 18px; padding-top: 16px; border-top: 1px solid var(--bb-line, rgba(255, 255, 255, 0.06)); }
  .create .btn { margin-top: 14px; }
  .section-picks { display: flex; flex-direction: column; gap: 10px; margin-top: 6px; }

  /* --- Grant lifecycle cards --- */
  .grants { display: flex; flex-direction: column; gap: 10px; }
  .grant {
    border: 1px solid var(--glass-border);
    border-radius: var(--bb-radius-md, 10px);
    padding: 12px 14px;
    background: rgba(255, 255, 255, 0.02);
  }
  .grant.pending { border-color: rgba(201, 168, 124, 0.3); }
  .grant.consumed { border-color: rgba(82, 183, 136, 0.25); }

  .grant-top { display: flex; align-items: center; justify-content: space-between; gap: 10px; flex-wrap: wrap; }
  .lifecycle {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    font-family: var(--bb-font-display);
    font-weight: 700;
    font-size: 10.5px;
    letter-spacing: 0.03em;
    text-transform: uppercase;
    color: var(--bb-muted);
    flex-wrap: wrap;
  }
  .stage { opacity: 0.55; }
  .stage.done { opacity: 1; color: var(--bb-tan-light); }
  .stage.done.live { color: var(--bb-green-glow, #52b788); }
  .sep { opacity: 0.4; }

  .owner {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    font-family: var(--bb-font-display);
    font-weight: 700;
    font-size: 14.5px;
    color: var(--bb-white);
  }

  .grant-sections { display: flex; gap: 6px; flex-wrap: wrap; align-items: center; margin-top: 8px; }
  .edit-access { margin-left: 2px; }
  .grant-edit { margin-top: 10px; display: flex; flex-direction: column; gap: 10px; }
  .grant-edit .section-picks.compact { display: flex; flex-direction: row; flex-wrap: wrap; gap: 8px 16px; }
  .grant-edit-actions { display: flex; gap: 8px; justify-content: flex-end; }
  .section-chip {
    font-family: var(--bb-font-mono);
    font-size: 11px;
    color: var(--bb-tan-light);
    background: rgba(201, 168, 124, 0.1);
    border: 1px solid rgba(201, 168, 124, 0.28);
    border-radius: 999px;
    padding: 2px 10px;
  }

  .grant-link {
    display: flex;
    align-items: center;
    gap: 10px;
    margin-top: 10px;
    padding: 8px 10px;
    border: 1px dashed var(--glass-border);
    border-radius: var(--bb-radius-sm, 8px);
    background: rgba(255, 255, 255, 0.02);
  }
  .grant-link code { font-size: 12px; word-break: break-all; flex: 1; color: var(--bb-muted); }

  .actions { display: flex; gap: 8px; align-items: center; }
  .actions form { margin: 0; }
  .btn.sm { padding: 4px 10px; font-size: 12px; }

  /* --- notifications section --- */
  .notif-list { display: flex; flex-direction: column; gap: 10px; }
  .notif-item {
    display: flex; align-items: flex-start; gap: 12px;
    border: 1px solid var(--bb-border); border-radius: var(--bb-radius-md, 10px);
    padding: 12px 14px; background: rgba(255, 255, 255, 0.02);
  }
  .notif-item.unread { border-color: rgba(201, 168, 124, 0.3); background: rgba(201, 168, 124, 0.05); }
  .notif-text { flex: 1; min-width: 0; }
  .notif-text b { font-size: 14px; color: var(--bb-white); }
  .notif-text p { margin: 4px 0; font-size: 13px; color: var(--bb-muted); }
  .notif-meta { font-family: var(--bb-font-body); font-size: 11px; color: var(--bb-muted); opacity: 0.8; }
  .level {
    font-family: var(--bb-font-body); font-weight: 600; font-size: 10.5px;
    padding: 3px 10px; border-radius: var(--bb-radius-pill, 100px); border: 1px solid transparent; white-space: nowrap;
  }
  .level.info { background: rgba(255,255,255,0.04); color: var(--bb-muted); border-color: var(--bb-border); }
  .level.success { background: rgba(82,183,136,0.12); color: var(--bb-green-glow); border-color: rgba(82,183,136,0.3); }
  .level.warning { background: rgba(201,168,124,0.12); color: var(--bb-tan-light); border-color: rgba(201,168,124,0.3); }
  .level.critical { background: rgba(176,90,70,0.15); color: #cf8a78; border-color: rgba(176,90,70,0.4); }
  .btn.danger { color: #e08f8f; }

  .delete-btn {
    background: rgba(220, 120, 120, 0.16);
    color: #e08f8f;
    border: 1px solid rgba(220, 120, 120, 0.4);
  }
  .delete-btn:disabled { opacity: 0.45; cursor: not-allowed; }

  @media (max-width: 760px) {
    .row { flex-direction: column; align-items: stretch; }
    .grant-top { flex-direction: column; align-items: flex-start; }
    .grant-link { flex-direction: column; align-items: stretch; }
    .grant-link .btn { justify-content: center; min-height: 40px; }
    .notif-item { flex-direction: column; align-items: flex-start; gap: 8px; }
    .modal-actions { flex-direction: column-reverse; }
  }
</style>
