<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { Icon, Bolota, Button, ButtonLink, Card, PageHead, ConfirmDialog, EmptyState, toast, getI18n, type Locale } from '@bagel/shared';
  import { page } from '$app/state';
  import { enhance, deserialize } from '$app/forms';
  import FetchKeyManager from '$lib/components/commands/fetches/FetchKeyManager.svelte';
  import LangSwitch from '$lib/components/LangSwitch.svelte';
  import CursorSwitch from '$lib/components/CursorSwitch.svelte';
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
  // The two halves of a share link's life. They read as different objects — one
  // names a person, the other is a URL still waiting for one — so they get their
  // own lists instead of a single list carrying a stage strip per row.
  const inUse = $derived(given.filter((g) => g.consumed));
  const pending = $derived(given.filter((g) => !g.consumed));
  const received = $derived(
    (data.received ?? []) as { owner_user_id: string; owner_login: string; sections: string[] }[]
  );
  const origin = $derived(page.url.origin);
  const unreadIds = $derived(notifications.filter((n) => !n.read).map((n) => n.id));

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
    { href: '#api-keys', label: t('fetches.keysTitle') },
    { href: '#import', label: t('settings.importNav') },
    { href: '#danger-zone', label: t('settings.dangerZone') }
  ]);

  // API keys for data sources. They sit in Settings rather than beside the
  // commands that spend them because they are account-level secrets, and this
  // page is already owner-only — a delegate with 'commands' access can use a
  // key through a data source but never read, rotate or destroy one.
  // Seeded once, then owned locally so a seal/rotate/delete can swap in the
  // server's refreshed list without a full page invalidation.
  // svelte-ignore state_referenced_locally
  let fetchKeys = $state(data.fetchKeys ?? []);
  // svelte-ignore state_referenced_locally
  let fetchKeysSeed = data.fetchKeys;
  $effect(() => {
    if (data.fetchKeys !== fetchKeysSeed) {
      fetchKeysSeed = data.fetchKeys;
      fetchKeys = data.fetchKeys ?? [];
    }
  });
  let keyBusy = $state(false);

  async function postKeyAction(action: string, body: FormData) {
    keyBusy = true;
    try {
      const res = await fetch(`/settings?/${action}`, { method: 'POST', body });
      const r = deserialize(await res.text());
      const d = (r.type === 'success' || r.type === 'failure' ? r.data : undefined) as
        | { ok?: boolean; name?: string; fetchKeys?: typeof fetchKeys; error?: string }
        | undefined;
      if (d?.ok && d.fetchKeys) {
        fetchKeys = d.fetchKeys;
        return d;
      }
      toast('err', d?.error ?? t('fetches.keySaveFailed'));
    } catch {
      toast('err', t('fetches.keySaveFailed'));
    } finally {
      keyBusy = false;
    }
    return undefined;
  }

  async function handleSetKey(label: string, value: string) {
    const body = new FormData();
    body.set('label', label);
    body.set('value', value);
    const d = await postKeyAction('setfetchkey', body);
    if (d) toast('ok', t('fetches.keySavedToast', { label, last4: fetchKeys.find((k) => k.label === label)?.last4 ?? '' }));
  }

  async function handleDeleteKey(label: string) {
    const body = new FormData();
    body.set('label', label);
    const d = await postKeyAction('delfetchkey', body);
    if (d) toast('ok', t('fetches.keyDeletedToast', { label }));
  }

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
  // The section picker is a disclosure behind the header's "New share link"
  // button: creating a link is the rarer act than reading who already has one.
  let creating = $state(false);
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

  // Surface action results as toasts (replaces the old inline banners). A table
  // rather than an if-chain: every new action is one row, not another branch.
  const actionToast: Record<string, () => string> = {
    created: () => t('settings.toastCreated'),
    updated: () => t('settings.toastAccessUpdated'),
    revoked: () => t('settings.toastRevoked'),
    opted_out: () => t('settings.toastOptedOut'),
    all_read: () => t('settings.toastAllRead')
  };
  // svelte-ignore state_referenced_locally
  let lastForm: unknown = form;
  $effect(() => {
    if (form === lastForm) return;
    lastForm = form;
    if (!form) return;
    if (form.error) {
      toast('err', String(form.error));
      return;
    }
    const message = form.ok ? actionToast[String(form.action)] : undefined;
    if (message) toast('ok', message());
  });

  // Revoke is irreversible (tokens are single-use), so it gets a confirm
  // dialog rather than optimistic apply + undo.
  let revokeTarget = $state<DelegationGrant | null>(null);
  let revokeForm = $state<HTMLFormElement | null>(null);

  // Leaving a shared dashboard removes this user's access. Confirm the choice
  // before submitting the existing opt-out action.
  let leaveTarget = $state<{ owner_user_id: string; owner_login: string } | null>(null);
  let leaveForm = $state<HTMLFormElement | null>(null);

  // Delete: a destructive ConfirmDialog that spells out the consequence and
  // opens with Cancel focused. It submits a hidden form to ?/delete (the server
  // contract is unchanged; the action redirects to /goodbye on success).
  let deleteOpen = $state(false);
  let deleting = $state(false);
  let deleteForm = $state<HTMLFormElement | null>(null);

  // Sign out everywhere: same destructive-confirm shape as delete. The action
  // revokes every session server-side, this one included, so it ends on the
  // login page rather than back here.
  let signOutOpen = $state(false);
  let signingOut = $state(false);
  let signOutForm = $state<HTMLFormElement | null>(null);
</script>

{#snippet sectionChips(sections: string[])}
  {#each sections as s (s)}<span class="section-chip">{sectionLabel(s)}</span>{/each}
{/snippet}

{#snippet editSections(g: DelegationGrant)}
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
{/snippet}

<section class="screen active">
  <PageHead eyebrow={t('settings.eyebrow')} description={t('settings.description')}>{t('settings.titlePre')}<em>{t('settings.titleEm')}</em></PageHead>

  <div class="layout">
    <!-- Section rail. SectionNav stacks itself into a hairline rail once its
         container is narrow (container query, not a viewport hide), so the
         same markup is the chip row on a phone. -->
    <aside class="rail">
      <SettingsNav label={t('settings.navSections')} items={navItems} />
      <Card class="board">
        <span class="board-title">{t('settings.boardState')}</span>
        <span class="board-row"><i class="dot live" aria-hidden="true"></i>{t('settings.boardTwitch')}</span>
        <span class="board-row"><i class="dot tan" aria-hidden="true"></i>{t('settings.boardAccess', { n: inUse.length })}</span>
        <span class="board-row"><i class="dot" aria-hidden="true"></i>{t('settings.boardShared', { n: received.length })}</span>
      </Card>
    </aside>

    <div class="stack">
  <!-- ACCOUNT -->
  <Card as="section" id="account" class="settings-section" tabindex="-1" aria-labelledby="h-account">
    <h2 id="h-account">{t('settings.account')}</h2>
    <p class="hint">{t('settings.accountHint')}</p>
    <div class="identity">
      <span class="identity-face"><Bolota name={data.login ?? ''} size={44} /></span>
      <div class="identity-main">
        <div class="identity-line">
          <b>{data.displayName || data.login}</b>
          <span class="pill ok"><Icon name="check" size={12} /> {t('settings.connectedPill')}</span>
        </div>
        <span class="identity-meta">{t('settings.reconnectTwitchHint')}</span>
      </div>
      <ButtonLink href="/auth/login?reauth=1" variant="ghost" icon="power">{t('common.reconnect')}</ButtonLink>
    </div>
  </Card>

  <!-- SHARED ACCESS: links you granted + dashboards shared with you. -->
  <Card as="section" id="access" class="settings-section" tabindex="-1" aria-labelledby="h-access">
    <div class="sec-head">
      <div>
        <h2 id="h-access">{t('settings.sharedAccess')}</h2>
        <p class="hint">{t('settings.sharedAccessHint')}</p>
      </div>
      <Button variant="primary" icon="link" aria-expanded={creating} onclick={() => (creating = !creating)}>
        {t('settings.newShareLink')}
      </Button>
    </div>

    {#if creating}
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
          return async ({ result, update }) => {
            await update();
            if (result.type === 'success') creating = false;
          };
        }}
      >
        <p class="hint">{t('settings.newShareLinkHint')}</p>
        <SectionPicker
          legend={t('settings.sectionsLegend')}
          options={pickerOptions((sec) => sec === 'commands')}
          error={createError}
          errorId="create-error"
        />
        <div class="create-actions">
          <Button variant="ghost" onclick={() => (creating = false)}>{t('common.cancel')}</Button>
          <Button type="submit" variant="primary" icon="link">{t('common.generate')}</Button>
        </div>
      </form>
    {/if}

    {#if given.length === 0}
      <EmptyState icon="link" title={t('settings.noShareLinks')} body={t('settings.noShareLinksBody')} />
    {/if}

    {#if inUse.length > 0}
      <span class="group-label">{t('settings.inUseCount', { n: inUse.length })}</span>
      <ul class="grants">
        {#each inUse as g (g.token)}
          <li class="grant consumed">
            <span class="face"><Bolota name={g.delegate_login} size={30} /></span>
            <div class="grant-main">
              <b class="grant-name">{g.delegate_login}</b>
              <div class="grant-sections">{@render sectionChips(g.sections)}</div>
            </div>
            <div class="actions">
              <Button variant="ghost" class="sm" onclick={() => openEdit(g.token)}>{t('settings.editAccess')}</Button>
              <Button variant="destructive" class="sm" onclick={() => (revokeTarget = g)}>{t('common.revoke')}</Button>
            </div>
            {#if editingToken === g.token}{@render editSections(g)}{/if}
          </li>
        {/each}
      </ul>
    {/if}

    {#if pending.length > 0}
      <span class="group-label">{t('settings.inviteLinks')}</span>
      <ul class="grants">
        {#each pending as g (g.token)}
          <li class="grant pending">
            <span class="pill warn">{t('settings.stageWaiting')}</span>
            <code class="grant-link">{linkFor(g.token)}</code>
            <div class="actions">
              <Button
                variant="ghost"
                class="sm"
                icon={copied[g.token] ? 'check' : 'link'}
                onclick={() => copy(g.token)}
                aria-label={t('settings.copyLinkAria')}
              >
                {copied[g.token] ? t('common.copied') : t('common.copy')}
              </Button>
              <Button variant="ghost" class="sm" onclick={() => openEdit(g.token)}>{t('settings.editAccess')}</Button>
              <Button variant="destructive" class="sm" onclick={() => (revokeTarget = g)}>{t('common.revoke')}</Button>
            </div>
            <div class="grant-sections">{@render sectionChips(g.sections)}</div>
            {#if editingToken === g.token}{@render editSections(g)}{/if}
          </li>
        {/each}
      </ul>
    {/if}

    <div class="sub-block">
      <span class="group-label">{t('settings.sharedWithYou')}</span>
      {#if received.length === 0}
        <EmptyState icon="overview" title={t('settings.nothingShared')} body={t('settings.nothingSharedBody')} />
      {:else}
        <ul class="grants">
          {#each received as r (r.owner_user_id)}
            <li class="grant consumed">
              <span class="face"><Bolota name={r.owner_login} size={30} /></span>
              <div class="grant-main">
                <b class="grant-name">{r.owner_login}</b>
                <div class="grant-sections">{@render sectionChips(r.sections)}</div>
              </div>
              <div class="actions">
                <ButtonLink href={`/delegate/enter?owner=${r.owner_user_id}`} variant="ghost" class="sm">{t('common.open')}</ButtonLink>
                <Button
                  type="button"
                  variant="destructive"
                  class="sm"
                  aria-label={t('settings.leaveDashboardAria', { login: r.owner_login })}
                  onclick={() => (leaveTarget = r)}
                >{t('common.leave')}</Button>
              </div>
            </li>
          {/each}
        </ul>
      {/if}
    </div>
  </Card>

  <!-- NOTIFICATIONS: the bell dropdown's "view all" target (/settings#notifications). -->
  <Card as="section" id="notifications" class="settings-section" tabindex="-1" aria-labelledby="h-notifications">
    <div class="sec-head">
      <h2 id="h-notifications">{t('settings.notifications')}</h2>
      {#if unreadIds.length > 0}
        <form method="POST" action="?/markAllRead" use:enhance>
          <input type="hidden" name="ids" value={unreadIds.join(',')} />
          <Button type="submit" variant="ghost" class="sm" icon="check">{t('settings.markAllRead')}</Button>
        </form>
      {/if}
    </div>
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
  </Card>

  <!-- PREFERENCES -->
  <Card as="section" id="preferences" class="settings-section" tabindex="-1" aria-labelledby="h-preferences">
    <h2 id="h-preferences">{t('settings.preferences')}</h2>
    <div class="row">
      <div>
        <span class="pref-label" id="lang-label">{t('settings.language')}</span>
        <p class="hint">{t('settings.languageHint')}</p>
      </div>
      <LangSwitch selected={savedLocale} />
    </div>
    <div class="row">
      <div>
        <span class="pref-label" id="cursor-label">{t('settings.customCursor')}</span>
        <p class="hint" id="cursor-hint">{t('settings.customCursorHint')}</p>
      </div>
      <CursorSwitch describedby="cursor-hint" />
    </div>
  </Card>

  <!-- API KEYS: account-level secrets for data sources. Owner-only, like the
       rest of this page. Data sources themselves are created inside the command
       editor; only the keys they spend are managed here. -->
  <Card as="section" id="api-keys" class="settings-section" tabindex="-1" aria-labelledby="h-api-keys">
    <h2 id="h-api-keys">{t('fetches.keysTitle')}</h2>
    <p class="hint">{t('settings.keysHint')}</p>
    <FetchKeyManager
      keys={fetchKeys}
      references={data.fetchKeyRefs ?? {}}
      busy={keyBusy}
      onSetKey={handleSetKey}
      onDeleteKey={handleDeleteKey}
    />
  </Card>

  <!-- IMPORT: the doorway to /settings/import, which is a settings section that
       needs a page of its own. -->
  <Card as="section" id="import" class="settings-section" tabindex="-1" aria-labelledby="h-import">
    <div class="sec-head">
      <div>
        <h2 id="h-import">{t('settings.importSetup')}</h2>
        <p class="hint">{t('settings.importSetupHint')}</p>
      </div>
      <ButtonLink href="/settings/import" variant="secondary" icon="send">{t('settings.importSetupCta')}</ButtonLink>
    </div>
  </Card>

  <!-- DANGER ZONE: visually separated, last. -->
  <Card as="section" id="danger-zone" class="settings-section danger-section" tabindex="-1" aria-labelledby="h-danger">
    <h2 id="h-danger">{t('settings.dangerZone')}</h2>
    <p class="hint">{t('settings.dangerZoneHint')}</p>
    <div class="row">
      <div>
        <b>{t('settings.signOutEverywhere')}</b>
        <p class="hint">{t('settings.signOutEverywhereHint')}</p>
      </div>
      <Button variant="destructive" onclick={() => (signOutOpen = true)}>{t('settings.signOutEverywhere')}</Button>
    </div>
    <div class="row">
      <div>
        <b>{t('settings.deleteAccount')}</b>
        <p class="hint">{t('settings.deleteAccountHint')}</p>
      </div>
      <Button variant="destructive" onclick={() => (deleteOpen = true)}>{t('settings.deleteAccount')}</Button>
    </div>
  </Card>
    </div>
  </div>
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

<!-- Leave shared dashboard confirm -->
<ConfirmDialog
  open={leaveTarget !== null}
  title={t('settings.leaveTitle', { login: leaveTarget?.owner_login ?? '' })}
  body={t('settings.leaveBody')}
  confirmLabel={t('common.leave')}
  cancelLabel={t('common.cancel')}
  danger
  onCancel={() => (leaveTarget = null)}
  onConfirm={() => {
    leaveForm?.requestSubmit();
    leaveTarget = null;
  }}
/>
{#if leaveTarget}
  <form method="POST" action="?/optOut" use:enhance bind:this={leaveForm} hidden>
    <input type="hidden" name="owner_user_id" value={leaveTarget.owner_user_id} />
  </form>
{/if}

<!-- Sign out everywhere: destructive, names the consequence, Cancel focused. -->
<ConfirmDialog
  open={signOutOpen}
  title={t('settings.signOutEverywhereTitle')}
  body={t('settings.signOutEverywhereBody')}
  confirmLabel={t('settings.signOutEverywhere')}
  cancelLabel={t('common.cancel')}
  danger
  busy={signingOut}
  onCancel={() => (signOutOpen = false)}
  onConfirm={() => {
    signingOut = true;
    signOutForm?.requestSubmit();
  }}
/>
{#if signOutOpen}
  <form method="POST" action="?/signOutEverywhere" use:enhance bind:this={signOutForm} hidden></form>
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
  /* Surface (bg/border/radius/padding) comes from <Card>; only the layout and
     scroll-anchoring live here. :global because the class rides a component
     root, which the parent's scoping hash never reaches. */
  :global(.settings-section) {
    /* Anchor + programmatic focus land below the sticky topbar. */
    scroll-margin-top: calc(80px + env(safe-area-inset-top, 0px));
  }
  :global(.settings-section:focus) { outline: none; }

  /* Rail + column, same shape as the modules index: one column on a phone
     (SectionNav renders its chip row), two once there is room for a ~12rem
     rail. The rail is a container query inside SectionNav, so nothing here
     hides anything. */
  .layout {
    display: grid;
    gap: 18px 40px;
    --section-nav-sticky-top: calc(58px + env(safe-area-inset-top, 0px) + 68px);
  }
  @media (min-width: 761px) {
    .layout { grid-template-columns: 12rem minmax(0, 1fr); align-items: start; }
  }
  .rail { display: flex; flex-direction: column; gap: 22px; min-width: 0; }
  /* The board card only makes sense beside the rail; in the phone's single
     column it would sit between the chip row and the first section. */
  .rail :global(.board) { display: none; }
  @media (min-width: 761px) {
    .rail :global(.board) {
      display: flex;
      flex-direction: column;
      gap: 10px;
      padding: 14px 16px;
    }
  }
  .board-title {
    font-family: var(--bb-font-mono);
    font-size: 9.5px;
    letter-spacing: 0.18em;
    text-transform: uppercase;
    color: var(--bb-tan);
  }
  .board-row { display: flex; align-items: center; gap: 8px; font-size: 12.5px; color: var(--bb-muted); }
  .dot { width: 6px; height: 6px; border-radius: 50%; background: var(--bb-muted); flex: none; }
  .dot.live { background: var(--bb-green-glow, #52b788); box-shadow: 0 0 8px var(--bb-green-glow, #52b788); }
  .dot.tan { background: var(--bb-tan); }

  .stack { display: flex; flex-direction: column; gap: 18px; min-width: 0; }

  h2 { margin: 0 0 6px; font-size: 16px; }
  .hint { color: var(--bb-muted, #998f82); font-size: 13px; margin: 0 0 12px; }

  /* Card header: title block left, its one primary action right. */
  .sec-head {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 16px;
    flex-wrap: wrap;
  }
  .sec-head h2 { margin-bottom: 0; }
  .sec-head .hint { margin: 4px 0 0; }
  .sec-head + :global(*) { margin-top: 18px; }

  .group-label {
    display: block;
    font-family: var(--bb-font-mono);
    font-size: 9.5px;
    letter-spacing: 0.18em;
    text-transform: uppercase;
    color: var(--bb-tan);
    margin: 22px 0 10px;
  }
  .sub-block {
    margin-top: 26px;
    padding-top: 18px;
    border-top: 1px solid var(--bb-line, rgba(255, 255, 255, 0.06));
  }
  .sub-block .group-label { margin-top: 0; }

  /* --- account identity --- */
  .identity {
    display: flex;
    align-items: center;
    gap: 18px;
    padding: 16px;
    border: 1px solid rgba(82, 183, 136, 0.25);
    border-radius: 8px;
    background: rgba(82, 183, 136, 0.04);
  }
  .identity-face { flex: none; display: flex; }
  .identity-main { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 6px; }
  .identity-line { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
  .identity-line b { font-size: 15px; color: var(--bb-white); }
  .identity-meta { font-size: 12.5px; color: var(--bb-muted); }
  .pill {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    font-family: var(--bb-font-mono);
    font-size: 10px;
    letter-spacing: 0.1em;
    text-transform: uppercase;
    border-radius: var(--bb-radius-pill, 100px);
    padding: 3px 10px;
    white-space: nowrap;
  }
  .pill.ok { color: var(--bb-green-glow); background: rgba(82, 183, 136, 0.12); border: 1px solid rgba(82, 183, 136, 0.3); }
  .pill.warn { color: var(--bb-tan-light); background: rgba(201, 168, 124, 0.12); border: 1px solid rgba(201, 168, 124, 0.3); }
  .row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 16px;
    padding: 12px 0 0;
  }
  .row b, .pref-label { font-size: 14px; color: var(--bb-white); font-family: var(--bb-font-body); }
  /* Connected pill mirrors the songqueue Spotify pill so both integrations
     read as the same "linked account" state. */
  .ok-pill { display: inline-flex; align-items: center; gap: 6px; color: var(--bb-green-glow); font-family: var(--bb-font-body); font-size: 13px; font-weight: 600; }
  .row .hint { margin: 4px 0 0; }
  .create {
    margin-top: 18px;
    padding: 16px;
    border: 1px dashed var(--bb-border);
    border-radius: 8px;
    background: rgba(255, 255, 255, 0.02);
  }
  .create-actions { display: flex; gap: 10px; justify-content: flex-end; margin-top: 14px; }

  /* --- Grant rows --- */
  .grants { display: flex; flex-direction: column; gap: 8px; list-style: none; margin: 0; padding: 0; }
  .grant {
    display: grid;
    grid-template-columns: auto minmax(0, 1fr) auto;
    align-items: center;
    gap: 14px;
    border: 1px solid var(--glass-border);
    border-radius: 8px;
    padding: 14px 16px;
    background: rgba(255, 255, 255, 0.02);
  }
  .grant.pending { border-color: rgba(201, 168, 124, 0.3); }
  .grant.consumed { border-color: rgba(82, 183, 136, 0.25); }

  .face { flex: none; display: flex; }
  .grant-main { min-width: 0; display: flex; flex-direction: column; gap: 7px; }
  .grant-name { font-size: 14px; color: var(--bb-white); }
  .grant-link {
    font-family: var(--bb-font-mono);
    font-size: 12px;
    color: var(--bb-muted);
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .grant-sections { display: flex; gap: 6px; flex-wrap: wrap; align-items: center; }
  /* A pending row's chips and its open editor both sit under the whole row. */
  .grant.pending .grant-sections { grid-column: 1 / -1; }
  .grant-edit { grid-column: 1 / -1; margin-top: 4px; display: flex; flex-direction: column; gap: 12px; }
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

  .actions { display: flex; gap: 8px; align-items: center; flex-wrap: wrap; }
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
  /* Same pill language as the connected/waiting states above, so a page full of
     status markers reads as one set rather than two. */
  .level {
    font-family: var(--bb-font-mono); font-size: 10px;
    letter-spacing: 0.1em; text-transform: uppercase;
    padding: 3px 10px; border-radius: var(--bb-radius-pill, 100px); border: 1px solid transparent; white-space: nowrap;
  }
  .level.info { background: rgba(255,255,255,0.04); color: var(--bb-muted); border-color: var(--bb-border); }
  .level.success { background: rgba(82,183,136,0.12); color: var(--bb-green-glow); border-color: rgba(82,183,136,0.3); }
  .level.warning { background: rgba(201,168,124,0.12); color: var(--bb-tan-light); border-color: rgba(201,168,124,0.3); }
  .level.critical { background: rgba(176,90,70,0.15); color: #cf8a78; border-color: rgba(176,90,70,0.4); }

  /* --- danger zone --- */
  :global(.danger-section) {
    margin-top: 28px;
    border-color: var(--bb-status-error-border, rgba(176, 90, 70, 0.4));
    background: var(--bb-status-error-bg, rgba(176, 90, 70, 0.06));
  }

  @media (max-width: 760px) {
    .row, .identity { flex-direction: column; align-items: stretch; }
    .sec-head :global(.btn) { width: 100%; justify-content: center; }
    /* A phone has no room for three grid tracks; every part of a row stacks
       and the link wraps instead of ellipsing. */
    .grant { grid-template-columns: minmax(0, 1fr); }
    .grant .actions :global(.btn) { flex: 1; justify-content: center; }
    .grant-link { white-space: normal; word-break: break-all; }
    /* Level pill and Read button share the first line; the message gets the
       full width rather than a column three words wide. */
    .notif-item { flex-wrap: wrap; }
    .notif-text { flex-basis: 100%; }
  }
  @media (max-width: 900px) {
    .layout { --section-nav-sticky-top: calc(52px + env(safe-area-inset-top, 0px) + 68px); }
  }
</style>
