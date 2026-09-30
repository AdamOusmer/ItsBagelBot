<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { copyText } from '@bagel/ui/lib/clipboard';
  import {
    Bolota,
    getI18n,
    toastFailure,
    type Locale
  } from '@bagel/kit';
  import {
    Button,
    ButtonLink,
    Card,
    Code,
    Eyebrow,
    Heading,
    PageHead,
    Text,
    SectionNav,
    ConfirmDialog,
    EmptyState,
    toast,
    StatusDot,
    Tag,
    Switch
  } from '@bagel/ui/svelte';
  import { page } from '$app/state';
  import { enhance, deserialize } from '$app/forms';
  import FetchKeyManager from '$lib/components/commands/fetches/FetchKeyManager.svelte';
  import LangSwitch from '$lib/components/LangSwitch.svelte';
  import CursorSwitch from '$lib/components/CursorSwitch.svelte';
  import SectionPicker from '$lib/components/settings/SectionPicker.svelte';
  import DeleteAccountDialog from '$lib/components/settings/DeleteAccountDialog.svelte';
  import { busyEnhance } from '$lib/components/settings/busy-enhance';
  import type { SubmitFunction } from '@sveltejs/kit';
  import { commandsHref } from '@bagel/kit/site-links';
  import type { DelegationGrant, NotificationWire } from '$lib/server/services';

  let { data, form } = $props();

  const { t, locale } = getI18n();
  const failed = toastFailure(toast, t);

  const notifications = $derived((data.notifications ?? []) as NotificationWire[]);
  const savedLocale = $derived((data.savedLocale ?? 'en') as Locale);
  const commandsPageUrl = $derived(commandsHref((data.login ?? '').toLowerCase()));
  const commandsPageDelayed = $derived(form?.action === 'commands_page' && !!form?.edgeDelayed);
  const LEVEL_LABEL = {
    info: 'settings.level.info',
    success: 'settings.level.success',
    warning: 'settings.level.warning',
    critical: 'settings.level.critical'
  } as const;
  const levelLabel = (l: string) => t(LEVEL_LABEL[l as keyof typeof LEVEL_LABEL] ?? LEVEL_LABEL.info);
  const stampFormat = $derived(new Intl.DateTimeFormat(locale, { dateStyle: 'medium', timeStyle: 'short' }));
  const NOTIFICATION_PAGE = 20;
  let showAllNotifications = $state(false);
  const shownNotifications = $derived(showAllNotifications ? notifications : notifications.slice(0, NOTIFICATION_PAGE));
  const hiddenNotificationCount = $derived(Math.max(0, notifications.length - NOTIFICATION_PAGE));
  const twitchConnected = $derived((data.twitchConnected ?? null) as boolean | null);
  const boardTwitchKey = $derived(
    twitchConnected === null
      ? 'settings.boardTwitchUnknown'
      : twitchConnected
        ? 'settings.boardTwitch'
        : 'settings.boardTwitchOff'
  );
  const LEVEL_TONE: Record<string, 'quiet' | 'live' | 'pre' | 'danger'> = {
    info: 'quiet',
    success: 'live',
    warning: 'pre',
    critical: 'danger'
  };

  const createdGrant = $derived(form?.createdGrant as DelegationGrant | undefined);
  const given = $derived.by<DelegationGrant[]>(() => {
    const grants = (data.given ?? []) as DelegationGrant[];
    if (!createdGrant || grants.some((g) => g.token === createdGrant.token)) return grants;
    return [createdGrant, ...grants];
  });
  const inUse = $derived(given.filter((g) => g.consumed));
  const pending = $derived(given.filter((g) => !g.consumed));
  const received = $derived(
    (data.received ?? []) as { owner_user_id: string; owner_login: string; sections: string[] }[]
  );
  const origin = $derived(page.url.origin);
  const unreadIds = $derived(notifications.filter((n) => !n.read).map((n) => n.id));

  const grantable = $derived(
    (data.grantableSections ?? ['commands', 'modules', 'discord', 'channelpoints', 'billing']) as string[]
  );
  function sectionLabel(sec: string): string {
    switch (sec) {
      case 'modules':
        return t('settings.modules');
      case 'discord':
        return t('nav.discord');
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

  const navItems = $derived([
    { href: '#account', label: t('settings.account') },
    { href: '#access', label: t('settings.sharedAccess') },
    { href: '#notifications', label: t('settings.notifications') },
    { href: '#preferences', label: t('settings.preferences') },
    { href: '#public-pages', label: t('settings.publicPages') },
    { href: '#api-keys', label: t('fetches.keysTitle') },
    { href: '#import', label: t('settings.importNav') },
    { href: '#danger-zone', label: t('settings.dangerZone') }
  ]);

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
      failed(d, 'fetches.keySaveFailed');
    } catch {
      toast('danger', t('fetches.keySaveFailed'));
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
    if (d) toast('success', t('fetches.keySavedToast', { label, last4: fetchKeys.find((k) => k.label === label)?.last4 ?? '' }));
  }

  async function handleDeleteKey(label: string) {
    const body = new FormData();
    body.set('label', label);
    const d = await postKeyAction('delfetchkey', body);
    if (d) toast('success', t('fetches.keyDeletedToast', { label }));
  }

  let editingToken = $state<string | null>(null);
  function openEdit(token: string) {
    editError = '';
    editingToken = token;
  }
  function closeEdit() {
    editingToken = null;
  }

  let createError = $state('');
  let editError = $state('');
  let creating = $state(false);
  function hasSelection(formData: FormData): boolean {
    return grantable.some((sec) => formData.get(sec) === 'on');
  }

  function linkFor(token: string): string {
    return `${origin}/delegate/accept?t=${token}`;
  }

  let copied = $state<Record<string, boolean>>({});
  async function copy(token: string) {
    if (await copyText(linkFor(token))) {
      copied = { ...copied, [token]: true };
      toast('success', t('settings.toastInviteCopied'));
      setTimeout(() => (copied = { ...copied, [token]: false }), 4000);
    } else {
      toast('danger', t('settings.toastClipboardBlocked'));
    }
  }

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
      toast('danger', String(form.error));
      return;
    }
    const message = form.ok ? actionToast[String(form.action)] : undefined;
    if (message) toast('success', message());
  });

  let commandsPageBusy = $state(false);
  const commandsPageSubmit: SubmitFunction = ({ formData }) => {
    commandsPageBusy = true;
    const enabling = formData.get('enabled') === 'on';
    return async ({ result, update }) => {
      await update();
      commandsPageBusy = false;
      if (result.type === 'success') {
        toast('success', t(enabling ? 'settings.toastCommandsPageOn' : 'settings.toastCommandsPageOff'));
      } else if (result.type === 'error') {
        toast('danger', t('serverErrors.updateRetry'));
      }
    };
  };

  let revokeTarget = $state<DelegationGrant | null>(null);
  let revokeForm = $state<HTMLFormElement | null>(null);
  let revoking = $state(false);

  let leaveTarget = $state<{ owner_user_id: string; owner_login: string } | null>(null);
  let leaveForm = $state<HTMLFormElement | null>(null);
  let leaving = $state(false);

  let deleteOpen = $state(false);
  let deleting = $state(false);
  let deleteForm = $state<HTMLFormElement | null>(null);

  let signOutOpen = $state(false);
  let signingOut = $state(false);
  let signOutForm = $state<HTMLFormElement | null>(null);
</script>

{#snippet sectionChips(sections: string[])}
  {#each sections as s (s)}<Tag tone="quiet">{sectionLabel(s)}</Tag>{/each}
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
      <Button variant="ghost" size="sm" onclick={closeEdit}>{t('common.cancel')}</Button>
      <Button type="submit" variant="primary" size="sm">{t('common.save')}</Button>
    </div>
  </form>
{/snippet}

<section class="screen active">
  <PageHead eyebrow={t('settings.eyebrow')} description={t('settings.description')}>{t('settings.titlePre')}<em>{t('settings.titleEm')}</em></PageHead>

  <div class="layout">
    <aside class="rail" data-lenis-prevent>
      <SectionNav label={t('settings.navSections')} items={navItems} />
      <div class="board">
        <Card flush>
          <div class="board-body">
            <Eyebrow>{t('settings.boardState')}</Eyebrow>
            <span class="board-row"><StatusDot tone={twitchConnected === true ? 'success' : 'neutral'} /><Text as="span" size="xs" tone="muted">{t(boardTwitchKey)}</Text></span>
            <span class="board-row"><StatusDot tone="warning" /><Text as="span" size="xs" tone="muted">{t('settings.boardAccess', { n: inUse.length })}</Text></span>
            <span class="board-row"><StatusDot tone="neutral" /><Text as="span" size="xs" tone="muted">{t('settings.boardShared', { n: received.length })}</Text></span>
          </div>
        </Card>
      </div>
    </aside>

    <div class="stack">
  <section id="account" class="settings-section" tabindex="-1" aria-labelledby="h-account">
  <Card>
    <div class="sec-title"><Heading level={6} as="h2" id="h-account">{t('settings.account')}</Heading></div>
    <div class="hint"><Text size="sm" tone="muted">{t('settings.accountHint')}</Text></div>
    <div class="identity">
      <span class="identity-face"><Bolota name={data.login ?? ''} size={44} active /></span>
      <div class="identity-main">
        <div class="identity-line">
          <Text as="span"><b>{data.displayName || data.login}</b></Text>
          {#if twitchConnected === true}
            <Tag tone="live" mark="solid">{t('settings.connectedPill')}</Tag>
          {:else if twitchConnected === false}
            <Tag tone="alpha" mark="dash">{t('settings.notConnectedPill')}</Tag>
          {/if}
        </div>
        <Text as="span" size="xs" tone="muted">{t('settings.reconnectTwitchHint')}</Text>
      </div>
      <ButtonLink href="/auth/login?reauth=1" variant="ghost">{t('common.reconnect')}</ButtonLink>
    </div>
  </Card>
  </section>

  <section id="access" class="settings-section" tabindex="-1" aria-labelledby="h-access">
  <Card>
    <div class="sec-head">
      <div class="sec-intro">
        <Heading level={6} as="h2" id="h-access">{t('settings.sharedAccess')}</Heading>
        <Text size="sm" tone="muted">{t('settings.sharedAccessHint')}</Text>
      </div>
      <Button variant="primary" aria-expanded={creating} onclick={() => (creating = !creating)}>
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
        <div class="hint"><Text size="sm" tone="muted">{t('settings.newShareLinkHint')}</Text></div>
        <SectionPicker
          legend={t('settings.sectionsLegend')}
          options={pickerOptions((sec) => sec === 'commands')}
          error={createError}
          errorId="create-error"
        />
        <div class="create-actions">
          <Button variant="ghost" onclick={() => (creating = false)}>{t('common.cancel')}</Button>
          <Button type="submit" variant="primary">{t('common.generate')}</Button>
        </div>
      </form>
    {/if}

    {#if given.length === 0}
      <EmptyState title={t('settings.noShareLinks')} body={t('settings.noShareLinksBody')} />
    {/if}

    {#if inUse.length > 0}
      <div class="group-label"><Eyebrow>{t('settings.inUseCount', { n: inUse.length })}</Eyebrow></div>
      <ul class="grants">
        {#each inUse as g (g.token)}
          <li class="grant consumed">
            <span class="face"><Bolota name={g.delegate_login} size={30} /></span>
            <div class="grant-main">
              <Text as="span" size="sm"><b>{g.delegate_login}</b></Text>
              <div class="grant-sections">{@render sectionChips(g.sections)}</div>
            </div>
            <div class="actions">
              <Button variant="ghost" size="sm" onclick={() => openEdit(g.token)}>{t('settings.editAccess')}</Button>
              <Button size="sm" onclick={() => (revokeTarget = g)} tone="danger">{t('common.revoke')}</Button>
            </div>
            {#if editingToken === g.token}{@render editSections(g)}{/if}
          </li>
        {/each}
      </ul>
    {/if}

    {#if pending.length > 0}
      <div class="group-label"><Eyebrow>{t('settings.inviteLinks')}</Eyebrow></div>
      <ul class="grants">
        {#each pending as g (g.token)}
          <li class="grant pending">
            <Tag tone="alpha" mark="dash">{t('settings.stageWaiting')}</Tag>
            <div class="grant-link"><Code>{linkFor(g.token)}</Code></div>
            <div class="actions">
              <Button
                variant="ghost"
                size="sm"
                onclick={() => copy(g.token)}
                aria-label={t('settings.copyLinkAria')}
              >
                {copied[g.token] ? t('common.copied') : t('common.copy')}
              </Button>
              <Button variant="ghost" size="sm" onclick={() => openEdit(g.token)}>{t('settings.editAccess')}</Button>
              <Button size="sm" onclick={() => (revokeTarget = g)} tone="danger">{t('common.revoke')}</Button>
            </div>
            <div class="grant-sections">{@render sectionChips(g.sections)}</div>
            {#if editingToken === g.token}{@render editSections(g)}{/if}
          </li>
        {/each}
      </ul>
    {/if}

    <div class="sub-block">
      <div class="group-label"><Eyebrow>{t('settings.sharedWithYou')}</Eyebrow></div>
      {#if received.length === 0}
        <EmptyState title={t('settings.nothingShared')} body={t('settings.nothingSharedBody')} />
      {:else}
        <ul class="grants">
          {#each received as r (r.owner_user_id)}
            <li class="grant consumed">
              <span class="face"><Bolota name={r.owner_login} size={30} /></span>
              <div class="grant-main">
                <Text as="span" size="sm"><b>{r.owner_login}</b></Text>
                <div class="grant-sections">{@render sectionChips(r.sections)}</div>
              </div>
              <div class="actions">
                <ButtonLink href={`/delegate/enter?owner=${r.owner_user_id}`} variant="ghost" size="sm">{t('common.open')}</ButtonLink>
                <Button
                  type="button"
                  size="sm"
                  aria-label={t('settings.leaveDashboardAria', { login: r.owner_login })}
                  onclick={() => (leaveTarget = r)}
                  tone="danger"
                >{t('common.leave')}</Button>
              </div>
            </li>
          {/each}
        </ul>
      {/if}
    </div>
  </Card>
  </section>

  <section id="notifications" class="settings-section" tabindex="-1" aria-labelledby="h-notifications">
  <Card>
    <div class="sec-head">
      <Heading level={6} as="h2" id="h-notifications">{t('settings.notifications')}</Heading>
      {#if unreadIds.length > 0}
        <form method="POST" action="?/markAllRead" use:enhance>
          <input type="hidden" name="ids" value={unreadIds.join(',')} />
          <Button type="submit" variant="ghost" size="sm">{t('settings.markAllRead')}</Button>
        </form>
      {/if}
    </div>
    {#if notifications.length === 0}
      <div class="hint"><Text size="sm" tone="muted">{t('settings.notificationsEmpty')}</Text></div>
    {:else}
      <ul class="notif-list">
        {#each shownNotifications as n (n.id)}
          <li class="notif-item" class:unread={!n.read}>
            <Tag tone={LEVEL_TONE[n.level] ?? 'quiet'}>{levelLabel(n.level)}</Tag>
            <div class="notif-text">
              <Text as="span" size="sm"><b>{n.title}</b></Text>
              <Text size="sm" tone="muted">{n.body}</Text>
              <Text as="span" size="xs" tone="muted">{stampFormat.format(new Date(n.created_at))}</Text>
            </div>
            {#if !n.read}
              <form method="POST" action="?/markRead" use:enhance>
                <input type="hidden" name="id" value={n.id} />
                <Button type="submit" variant="ghost" size="sm">{t('common.read')}</Button>
              </form>
            {/if}
          </li>
        {/each}
      </ul>
      {#if hiddenNotificationCount > 0}
        <div class="notif-more">
          <Button variant="ghost" size="sm" aria-expanded={showAllNotifications} onclick={() => (showAllNotifications = !showAllNotifications)}>
            {showAllNotifications ? t('settings.showFewer') : t('settings.showOlder', { n: hiddenNotificationCount })}
          </Button>
        </div>
      {/if}
    {/if}
  </Card>
  </section>

  <section id="preferences" class="settings-section" tabindex="-1" aria-labelledby="h-preferences">
  <Card>
    <div class="sec-title"><Heading level={6} as="h2" id="h-preferences">{t('settings.preferences')}</Heading></div>
    <div class="row">
      <div class="row-text">
        <Text as="span" size="sm" id="lang-label">{t('settings.language')}</Text>
        <Text size="sm" tone="muted">{t('settings.languageHint')}</Text>
      </div>
      <LangSwitch selected={savedLocale} />
    </div>
    <div class="row">
      <div class="row-text">
        <Text as="span" size="sm" id="cursor-label">{t('settings.customCursor')}</Text>
        <Text size="sm" tone="muted" id="cursor-hint">{t('settings.customCursorHint')}</Text>
      </div>
      <CursorSwitch describedby="cursor-hint" />
    </div>
  </Card>
  </section>

  <section id="public-pages" class="settings-section" tabindex="-1" aria-labelledby="h-public-pages">
  <Card>
    <div class="sec-title"><Heading level={6} as="h2" id="h-public-pages">{t('settings.publicPages')}</Heading></div>
    <div class="row">
      <div class="row-text">
        <Text as="span" size="sm" id="commands-page-label">{t('settings.commandsPage')}</Text>
        <Text size="sm" tone="muted" id="commands-page-hint">{t('settings.commandsPageHint', { url: commandsPageUrl })}</Text>
        {#if commandsPageDelayed}
          <Text size="sm" tone="muted">{t('settings.commandsPageDelayed')}</Text>
        {/if}
      </div>
      <form method="POST" action="?/setCommandsPage" use:enhance={commandsPageSubmit}>
        <input type="hidden" name="enabled" value={data.commandsPage ? '' : 'on'} />
        <Switch type="submit" checked={!!data.commandsPage} pending={commandsPageBusy} label={t('settings.commandsPage')} describedby="commands-page-hint" />
      </form>
    </div>
  </Card>
  </section>

  <section id="api-keys" class="settings-section" tabindex="-1" aria-labelledby="h-api-keys">
  <Card>
    <div class="sec-title"><Heading level={6} as="h2" id="h-api-keys">{t('fetches.keysTitle')}</Heading></div>
    <div class="hint"><Text size="sm" tone="muted">{t('settings.keysHint')}</Text></div>
    <FetchKeyManager
      keys={fetchKeys}
      references={data.fetchKeyRefs ?? {}}
      busy={keyBusy}
      onSetKey={handleSetKey}
      onDeleteKey={handleDeleteKey}
    />
  </Card>
  </section>

  <section id="import" class="settings-section" tabindex="-1" aria-labelledby="h-import">
  <Card>
    <div class="sec-head">
      <div class="sec-intro">
        <Heading level={6} as="h2" id="h-import">{t('settings.importSetup')}</Heading>
        <Text size="sm" tone="muted">{t('settings.importSetupHint')}</Text>
      </div>
      <ButtonLink href="/settings/import" variant="secondary">{t('settings.importSetupCta')}</ButtonLink>
    </div>
  </Card>
  </section>

  <section id="danger-zone" class="settings-section danger-section" tabindex="-1" aria-labelledby="h-danger">
  <Card tone="danger">
    <div class="sec-title"><Heading level={6} as="h2" id="h-danger">{t('settings.dangerZone')}</Heading></div>
    <div class="hint"><Text size="sm" tone="muted">{t('settings.dangerZoneHint')}</Text></div>
    <div class="row">
      <div class="row-text">
        <Text as="span" size="sm"><b>{t('settings.signOutEverywhere')}</b></Text>
        <Text size="sm" tone="muted">{t('settings.signOutEverywhereHint')}</Text>
      </div>
      <Button onclick={() => (signOutOpen = true)} tone="danger">{t('settings.signOutEverywhere')}</Button>
    </div>
    <div class="row">
      <div class="row-text">
        <Text as="span" size="sm"><b>{t('settings.deleteAccount')}</b></Text>
        <Text size="sm" tone="muted">{t('settings.deleteAccountHint')}</Text>
      </div>
      <Button onclick={() => (deleteOpen = true)} tone="danger">{t('settings.deleteAccount')}</Button>
    </div>
  </Card>
  </section>
    </div>
  </div>
</section>

<ConfirmDialog
  open={revokeTarget !== null}
  title={t('settings.revokeTitle')}
  body={revokeTarget?.consumed
    ? t('settings.revokeBodyConsumed', { login: revokeTarget.delegate_login || t('settings.revokeBodyDelegate') })
    : t('settings.revokeBodyPending')}
  confirmLabel={t('common.revoke')}
  cancelLabel={t('common.cancel')}
  busyLabel={t('settings.working')}
  danger
  busy={revoking}
  onCancel={() => (revokeTarget = null)}
  onConfirm={() => revokeForm?.requestSubmit()}
/>
{#if revokeTarget}
  <form
    method="POST"
    action="?/revoke"
    use:enhance={busyEnhance((b) => (revoking = b), (type) => type === 'success' && (revokeTarget = null))}
    bind:this={revokeForm}
    hidden
  >
    <input type="hidden" name="token" value={revokeTarget.token} />
  </form>
{/if}

<ConfirmDialog
  open={leaveTarget !== null}
  title={t('settings.leaveTitle', { login: leaveTarget?.owner_login ?? '' })}
  body={t('settings.leaveBody')}
  confirmLabel={t('common.leave')}
  cancelLabel={t('common.cancel')}
  busyLabel={t('settings.working')}
  danger
  busy={leaving}
  onCancel={() => (leaveTarget = null)}
  onConfirm={() => leaveForm?.requestSubmit()}
/>
{#if leaveTarget}
  <form
    method="POST"
    action="?/optOut"
    use:enhance={busyEnhance((b) => (leaving = b), (type) => type === 'success' && (leaveTarget = null))}
    bind:this={leaveForm}
    hidden
  >
    <input type="hidden" name="owner_user_id" value={leaveTarget.owner_user_id} />
  </form>
{/if}

<ConfirmDialog
  open={signOutOpen}
  title={t('settings.signOutEverywhereTitle')}
  body={t('settings.signOutEverywhereBody')}
  confirmLabel={t('settings.signOutEverywhere')}
  cancelLabel={t('common.cancel')}
  busyLabel={t('settings.working')}
  danger
  busy={signingOut}
  onCancel={() => (signOutOpen = false)}
  onConfirm={() => signOutForm?.requestSubmit()}
/>
{#if signOutOpen}
  <form
    method="POST"
    action="?/signOutEverywhere"
    use:enhance={busyEnhance((b) => (signingOut = b), () => {})}
    bind:this={signOutForm}
    hidden
  ></form>
{/if}

<DeleteAccountDialog
  open={deleteOpen}
  login={data.login ?? ''}
  busy={deleting}
  onCancel={() => (deleteOpen = false)}
  onConfirm={() => deleteForm?.requestSubmit()}
/>
{#if deleteOpen}
  <form
    method="POST"
    action="?/delete"
    use:enhance={busyEnhance((b) => (deleting = b), () => {})}
    bind:this={deleteForm}
    hidden
  ></form>
{/if}

<style>
  .settings-section {
    --btn-min-h: 44px;
    scroll-margin-top: calc(80px + env(safe-area-inset-top, 0px));
  }
  .settings-section:focus { outline: none; }

  .layout {
    display: grid;
    gap: 18px 40px;
    --bb-tabs-sticky-top: calc(var(--bb-topbar-height) + 68px);
  }
  @media (min-width: 761px) {
    .layout { grid-template-columns: 12rem minmax(0, 1fr); align-items: start; }
    /* The menu's wrapper is only as tall as its links. Stick the whole rail
       to the content grid so the board and section links travel together. */
    .rail {
      position: sticky;
      top: var(--bb-tabs-sticky-top);
      max-height: calc(100dvh - var(--bb-tabs-sticky-top) - 64px);
      overflow-y: auto;
      overscroll-behavior-y: contain;
      --bb-tabs-position: static;
      --bb-tabs-max-height: none;
    }
  }
  .rail { display: grid; align-content: start; gap: 22px; min-width: 0; }
  .board { display: none; }
  @media (min-width: 761px) {
    .board { display: block; }
  }
  .board-body {
    display: flex;
    flex-direction: column;
    gap: 10px;
    padding: 14px 16px;
  }
  .board-row { display: flex; align-items: center; gap: 8px; }

  .stack { display: flex; flex-direction: column; gap: 18px; min-width: 0; }

  .sec-title { margin-bottom: 6px; }
  .hint { margin: 0 0 12px; }

  .sec-head {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 16px;
    flex-wrap: wrap;
  }
  .sec-head:not(:last-child) { margin-bottom: 18px; }
  .sec-intro { display: grid; gap: 4px; }

  .group-label { margin: 22px 0 10px; }
  .sub-block {
    margin-top: 26px;
    padding-top: 18px;
    border-top: 1px solid var(--bb-border);
  }
  .sub-block .group-label { margin-top: 0; }

  .identity {
    display: flex;
    align-items: center;
    gap: 18px;
    padding: 16px;
    border: 1px solid rgba(var(--bb-green-glow-rgb), 0.25);
    border-radius: var(--bb-radius-md);
    background: rgba(var(--bb-green-glow-rgb), 0.04);
  }
  .identity-face { flex: none; display: flex; }
  .identity-main { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 6px; }
  .identity-line { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
  .row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 16px;
    padding: 12px 0 0;
  }
  .row-text { display: grid; gap: 4px; }
  .create {
    margin-top: 18px;
    padding: 16px;
    border: 1px dashed var(--bb-border);
    border-radius: var(--bb-radius-md);
    background: rgba(var(--bb-white-pure-rgb), 0.02);
  }
  .create-actions { display: flex; gap: 10px; justify-content: flex-end; margin-top: 14px; }

  .grants { display: flex; flex-direction: column; gap: 8px; list-style: none; margin: 0; padding: 0; }
  .grant {
    display: grid;
    grid-template-columns: auto minmax(0, 1fr) auto;
    align-items: center;
    gap: 14px;
    border: 1px solid var(--bb-glass-border);
    border-radius: var(--bb-radius-md);
    padding: 14px 16px;
    background: rgba(var(--bb-white-pure-rgb), 0.02);
  }
  .grant.pending { border-color: rgba(var(--bb-tan-rgb), 0.3); }
  .grant.consumed { border-color: rgba(var(--bb-green-glow-rgb), 0.25); }

  .face { flex: none; display: flex; }
  .grant-main { min-width: 0; display: flex; flex-direction: column; gap: 7px; }
  .grant-link {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: var(--bb-text-xs);
  }

  .grant-sections { display: flex; gap: 6px; flex-wrap: wrap; align-items: center; }
  .grant.pending .grant-sections { grid-column: 1 / -1; }
  .grant-edit { grid-column: 1 / -1; margin-top: 4px; display: flex; flex-direction: column; gap: 12px; }
  .grant-edit-actions { display: flex; gap: 10px; justify-content: flex-end; }

  .actions { display: flex; gap: 8px; align-items: center; flex-wrap: wrap; }

  .notif-list { display: flex; flex-direction: column; gap: 10px; list-style: none; margin: 0; padding: 0; }
  .notif-item {
    display: flex; align-items: flex-start; gap: 12px;
    border: 1px solid var(--bb-border); border-radius: var(--bb-radius-sm);
    padding: 12px 14px; background: rgba(var(--bb-white-pure-rgb), 0.02);
  }
  .notif-item.unread { border-color: rgba(var(--bb-tan-rgb), 0.3); background: rgba(var(--bb-tan-rgb), 0.05); }
  .notif-text { flex: 1; min-width: 0; display: grid; gap: 4px; }
  .notif-more { margin-top: 12px; }

  .danger-section { margin-top: 28px; }

  @media (max-width: 760px) {
    .row, .identity { flex-direction: column; align-items: stretch; }
    .sec-head { --btn-w: 100%; --btn-justify: center; }
    .grant { grid-template-columns: minmax(0, 1fr); }
    .grant .actions { --btn-w: 100%; --btn-justify: center; }
    .grant-link { white-space: normal; word-break: break-all; }
    .notif-item { flex-wrap: wrap; }
    .notif-text { flex-basis: 100%; }
  }
</style>
