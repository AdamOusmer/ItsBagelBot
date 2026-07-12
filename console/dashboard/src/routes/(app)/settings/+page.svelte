<script lang="ts">
  import { Icon, Button, ButtonLink, PageHead, ConfirmDialog, EmptyState, toast, getI18n, type Locale } from '@bagel/shared';
  import { page } from '$app/state';
  import { enhance } from '$app/forms';
  import LangSwitch from '$lib/components/LangSwitch.svelte';
  import SettingsNav from '$lib/components/settings/SettingsNav.svelte';
  import SectionPicker from '$lib/components/settings/SectionPicker.svelte';
  import type { DelegationGrant, NotificationWire } from '$lib/server/services';

  let { data, form } = $props();

  const { t } = getI18n();

  const notifications = $derived((data.notifications ?? []) as NotificationWire[]);
  const savedLocale = $derived((data.savedLocale ?? 'en') as Locale);
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
  const grantable = $derived(
    (data.grantableSections ?? ['commands', 'modules', 'channelpoints', 'billing']) as string[]
  );
  function sectionLabel(sec: string): string {
    switch (sec) {
      case 'modules':
        return t('settings.modules');
      case 'channelpoints':
        return t('nav.channelpoints');
      case 'timers':
        return t('nav.timers');
      case 'billing':
        return t('settings.billing');
      default:
        return t('settings.commands');
    }
  }
  const pickerOptions = (checkedFor: (sec: string) => boolean) =>
    grantable.map((sec) => ({ value: sec, label: sectionLabel(sec), checked: checkedFor(sec) }));

  // The in-page section nav. Anchors, not ARIA tabs — each targets a
  // <section tabindex=-1> below.
  const navItems = $derived([
    { href: '#account', label: t('settings.account') },
    { href: '#access', label: t('settings.sharedAccess') },
    { href: '#notifications', label: t('settings.notifications') },
    { href: '#preferences', label: t('settings.preferences') },
    { href: '#danger-zone', label: t('settings.dangerZone') }
  ]);

  // Which grant's access is being edited inline (add/remove sections).
  let editingToken = $state<string | null>(null);
  function openEdit(token: string) {
    editError = '';
    editingToken = token;
  }
  function closeEdit() {
    editingToken = null;
  }

  // Client-side "pick at least one section" guard for the share-link forms. The
  // server validates the same thing; this just surfaces it as inline text before
  // the round-trip. Both messages name the affected control (the section group).
  let createError = $state('');
  let editError = $state('');
  function hasSelection(formData: FormData): boolean {
    return grantable.some((sec) => formData.get(sec) === 'on');
  }

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

  // Delete: a destructive ConfirmDialog that spells out the consequence and
  // opens with Cancel focused. It submits a hidden form to ?/delete (the server
  // contract is unchanged; the action redirects to /goodbye on success).
  let deleteOpen = $state(false);
  let deleting = $state(false);
  let deleteForm = $state<HTMLFormElement | null>(null);
</script>

<section class="screen active">
  <PageHead eyebrow={t('settings.eyebrow')} description={t('settings.description')}>{t('settings.titlePre')}<em>{t('settings.titleEm')}</em></PageHead>

  <SettingsNav label={t('settings.navSections')} items={navItems} />

  <!-- ACCOUNT -->
  <section id="account" class="settings-section" tabindex="-1" aria-labelledby="h-account">
    <h2 id="h-account">{t('settings.account')}</h2>
    <div class="row">
      <div>
        <b>{t('settings.reconnectTwitch')}</b>
        <p class="hint">{t('settings.reconnectTwitchHint')}</p>
      </div>
      <ButtonLink href="/auth/login" variant="ghost" icon="power">{t('common.reconnect')}</ButtonLink>
    </div>
  </section>

  <!-- SHARED ACCESS: links you granted + dashboards shared with you. -->
  <section id="access" class="settings-section" tabindex="-1" aria-labelledby="h-access">
    <h2 id="h-access">{t('settings.sharedAccess')}</h2>

    <h3>{t('settings.accessGranted')}</h3>
    <p class="hint">{t('settings.accessGrantedHint')}</p>

    {#if given.length === 0}
      <EmptyState icon="link" title={t('settings.noShareLinks')} body={t('settings.noShareLinksBody')} />
    {:else}
      <ul class="grants">
        {#each given as g (g.token)}
          <li class="grant {g.consumed ? 'consumed' : 'pending'}">
            <div class="grant-top">
              <span class="lifecycle">
                <span class="stage done">{t('settings.stageCreated')}</span>
                <span class="sep" aria-hidden="true">→</span>
                <span class="stage {g.consumed || copied[g.token] ? 'done' : ''}">{t('settings.stageLinkShared')}</span>
                <span class="sep" aria-hidden="true">→</span>
                <span class="stage {g.consumed ? 'done live' : ''}">
                  {g.consumed ? t('settings.stageInUse', { login: g.delegate_login || t('settings.unknown') }) : t('settings.stageWaiting')}
                </span>
              </span>
              <Button variant="destructive" class="sm" onclick={() => (revokeTarget = g)}>{t('common.revoke')}</Button>
            </div>
            {#if editingToken === g.token}
              <form
                method="POST"
                action="?/updateSections"
                class="grant-edit"
                use:enhance={({ formData, cancel }) => {
                  if (!hasSelection(formData)) {
                    editError = t('settings.pickSectionError');
                    cancel();
                    return;
                  }
                  editError = '';
                  return async ({ result, update }) => {
                    await update();
                    if (result.type === 'success') closeEdit();
                  };
                }}
              >
                <input type="hidden" name="token" value={g.token} />
                <SectionPicker
                  legend={t('settings.sectionsLegend')}
                  options={pickerOptions((sec) => g.sections.includes(sec))}
                  error={editError}
                  errorId={`edit-error-${g.token}`}
                  compact
                />
                <div class="grant-edit-actions">
                  <Button variant="ghost" class="sm" onclick={closeEdit}>{t('common.cancel')}</Button>
                  <Button type="submit" variant="primary" class="sm" icon="check">{t('common.save')}</Button>
                </div>
              </form>
            {:else}
              <div class="grant-sections">
                {#each g.sections as s (s)}<span class="section-chip">{sectionLabel(s)}</span>{/each}
                <Button variant="ghost" class="sm" onclick={() => openEdit(g.token)}>{t('settings.editAccess')}</Button>
              </div>
            {/if}
            {#if !g.consumed}
              <div class="grant-link">
                <code>{linkFor(g.token)}</code>
                <Button
                  variant="ghost"
                  class="sm"
                  icon={copied[g.token] ? 'check' : 'link'}
                  onclick={() => copy(g.token)}
                  aria-label={t('settings.copyLinkAria')}
                >
                  {copied[g.token] ? t('common.copied') : t('common.copy')}
                </Button>
              </div>
            {/if}
          </li>
        {/each}
      </ul>
    {/if}

    <form
      method="POST"
      action="?/create"
      class="create"
      use:enhance={({ formData, cancel }) => {
        if (!hasSelection(formData)) {
          createError = t('settings.pickSectionError');
          cancel();
          return;
        }
        createError = '';
        return async ({ update }) => {
          await update();
        };
      }}
    >
      <h3>{t('settings.newShareLink')}</h3>
      <p class="hint">{t('settings.newShareLinkHint')}</p>
      <SectionPicker
        legend={t('settings.sectionsLegend')}
        options={pickerOptions((sec) => sec === 'commands')}
        error={createError}
        errorId="create-error"
      />
      <Button type="submit" variant="primary" icon="link">{t('common.generate')}</Button>
    </form>

    <h3 class="sub">{t('settings.sharedWithYou')}</h3>
    {#if received.length === 0}
      <EmptyState icon="overview" title={t('settings.nothingShared')} body={t('settings.nothingSharedBody')} />
    {:else}
      <ul class="grants">
        {#each received as r (r.owner_user_id)}
          <li class="grant consumed">
            <div class="grant-top">
              <span class="owner"><Icon name="overview" size={14} /> {r.owner_login}</span>
              <span class="actions">
                <ButtonLink href={`/delegate/enter?owner=${r.owner_user_id}`} variant="ghost" class="sm">{t('common.open')}</ButtonLink>
                <form method="POST" action="?/optOut" use:enhance>
                  <input type="hidden" name="owner_user_id" value={r.owner_user_id} />
                  <Button type="submit" variant="destructive" class="sm" aria-label={t('settings.leaveDashboardAria', { login: r.owner_login })}>{t('common.leave')}</Button>
                </form>
              </span>
            </div>
            <div class="grant-sections">
              {#each r.sections as s (s)}<span class="section-chip">{sectionLabel(s)}</span>{/each}
            </div>
          </li>
        {/each}
      </ul>
    {/if}
  </section>

  <!-- NOTIFICATIONS: the bell dropdown's "view all" target (/settings#notifications). -->
  <section id="notifications" class="settings-section" tabindex="-1" aria-labelledby="h-notifications">
    <h2 id="h-notifications">{t('settings.notifications')}</h2>
    {#if notifications.length === 0}
      <p class="hint">{t('settings.notificationsEmpty')}</p>
    {:else}
      <ul class="notif-list">
        {#each notifications as n (n.id)}
          <li class="notif-item" class:unread={!n.read}>
            <span class="level {n.level}">{levelLabel(n.level)}</span>
            <div class="notif-text">
              <b>{n.title}</b>
              <p>{n.body}</p>
              <span class="notif-meta">{new Date(n.created_at).toLocaleString()}</span>
            </div>
            {#if !n.read}
              <form method="POST" action="?/markRead" use:enhance>
                <input type="hidden" name="id" value={n.id} />
                <Button type="submit" variant="ghost" class="sm" icon="check">{t('common.read')}</Button>
              </form>
            {/if}
          </li>
        {/each}
      </ul>
    {/if}
  </section>

  <!-- PREFERENCES -->
  <section id="preferences" class="settings-section" tabindex="-1" aria-labelledby="h-preferences">
    <h2 id="h-preferences">{t('settings.preferences')}</h2>
    <div class="row">
      <div>
        <span class="pref-label" id="lang-label">{t('settings.language')}</span>
        <p class="hint">{t('settings.languageHint')}</p>
      </div>
      <LangSwitch selected={savedLocale} />
    </div>
  </section>

  <!-- DANGER ZONE: visually separated, last. -->
  <section id="danger-zone" class="settings-section danger-section" tabindex="-1" aria-labelledby="h-danger">
    <h2 id="h-danger">{t('settings.dangerZone')}</h2>
    <p class="hint">{t('settings.dangerZoneHint')}</p>
    <div class="row">
      <div>
        <b>{t('settings.deleteAccount')}</b>
        <p class="hint">{t('settings.deleteAccountHint')}</p>
      </div>
      <Button variant="destructive" onclick={() => (deleteOpen = true)}>{t('settings.deleteAccount')}</Button>
    </div>
  </section>
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

<!-- Delete confirm: destructive, names the consequence, Cancel focused. -->
<ConfirmDialog
  open={deleteOpen}
  title={t('settings.deleteTitle')}
  body={t('settings.deleteBody')}
  confirmLabel={t('settings.deleteAccount')}
  cancelLabel={t('common.cancel')}
  danger
  busy={deleting}
  onCancel={() => (deleteOpen = false)}
  onConfirm={() => {
    deleting = true;
    deleteForm?.requestSubmit();
  }}
/>
{#if deleteOpen}
  <form method="POST" action="?/delete" use:enhance bind:this={deleteForm} hidden></form>
{/if}

<style>
  .settings-section {
    margin-top: 18px;
    padding: var(--card-pad, 22px);
    background: var(--bb-card-bg);
    border: 1px solid var(--bb-border);
    border-radius: 8px;
    /* Anchor + programmatic focus land below the sticky topbar. */
    scroll-margin-top: calc(80px + env(safe-area-inset-top, 0px));
  }
  .settings-section:focus { outline: none; }

  h2 { margin: 0 0 6px; font-size: 16px; }
  h3 { margin: 18px 0 6px; font-size: 14px; }
  h3:first-of-type { margin-top: 6px; }
  h3.sub {
    margin-top: 26px;
    padding-top: 18px;
    border-top: 1px solid var(--bb-line, rgba(255, 255, 255, 0.06));
  }
  .hint { color: var(--bb-muted, #998f82); font-size: 13px; margin: 0 0 12px; }
  .row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 16px;
    padding: 12px 0 0;
  }
  .row b, .pref-label { font-size: 14px; color: var(--bb-white); font-family: var(--bb-font-body); }
  .row .hint { margin: 4px 0 0; }
  .create { margin-top: 22px; padding-top: 18px; border-top: 1px solid var(--bb-line, rgba(255, 255, 255, 0.06)); }
  .create :global(.btn) { margin-top: 14px; }

  /* --- Grant lifecycle cards --- */
  .grants { display: flex; flex-direction: column; gap: 10px; list-style: none; margin: 0; padding: 0; }
  .grant {
    border: 1px solid var(--glass-border);
    border-radius: 8px;
    padding: 12px 14px;
    background: rgba(255, 255, 255, 0.02);
  }
  .grant.pending { border-color: rgba(201, 168, 124, 0.3); }
  .grant.consumed { border-color: rgba(82, 183, 136, 0.25); }

  .grant-top { display: flex; align-items: center; justify-content: space-between; gap: 12px; flex-wrap: wrap; }
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

  .grant-sections { display: flex; gap: 8px; flex-wrap: wrap; align-items: center; margin-top: 10px; }
  .grant-edit { margin-top: 12px; display: flex; flex-direction: column; gap: 12px; }
  .grant-edit-actions { display: flex; gap: 10px; justify-content: flex-end; }
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
    gap: 12px;
    margin-top: 12px;
    padding: 8px 10px;
    border: 1px dashed var(--glass-border);
    border-radius: 8px;
    background: rgba(255, 255, 255, 0.02);
  }
  .grant-link code { font-size: 12px; word-break: break-all; flex: 1; min-width: 0; color: var(--bb-muted); }

  .actions { display: flex; gap: 10px; align-items: center; flex-wrap: wrap; }
  .actions form { margin: 0; }
  /* Standalone actions get a full 44px target; the dense inline "sm" buttons stay
     compact but keep a 36px target (well above the 24px AA floor) and 8px+ gaps. */
  :global(.settings-section .btn) { min-height: 44px; }
  :global(.settings-section .btn.sm) { min-height: 36px; padding: 8px 14px; }

  /* --- notifications section --- */
  .notif-list { display: flex; flex-direction: column; gap: 10px; list-style: none; margin: 0; padding: 0; }
  .notif-item {
    display: flex; align-items: flex-start; gap: 12px;
    border: 1px solid var(--bb-border); border-radius: 8px;
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

  /* --- danger zone --- */
  .danger-section {
    margin-top: 28px;
    border-color: var(--bb-status-error-border, rgba(176, 90, 70, 0.4));
    background: var(--bb-status-error-bg, rgba(176, 90, 70, 0.06));
  }

  @media (max-width: 760px) {
    .row { flex-direction: column; align-items: stretch; }
    .grant-top { flex-direction: column; align-items: flex-start; }
    .grant-link { flex-direction: column; align-items: stretch; }
    .grant-link :global(.btn) { justify-content: center; min-height: 44px; }
  }
</style>
