<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { enhance } from '$app/forms';
  import { beforeNavigate, goto, invalidateAll } from '$app/navigation';
  import type { SubmitFunction } from '@sveltejs/kit';
  import {
    AlertBanner,
    Button,
    ButtonLink,
    Card,
    Chip,
    ConfirmDialog,
    Icon,
    PageHead,
    PageToolbar,
    MasterToggle,
    SaveStatus,
    SegmentedControl,
    Switch,
    alertOff,
    alertOn,
    encodeIdList,
    encodeNameList,
    encodePinnedRoles,
    flagValue,
    getI18n,
    normalizeHex,
    parseIdList,
    parseNameList,
    parsePinnedRoles,
    ticketOpenLimitN,
    ticketPanelSpec,
    toast,
    CATEGORY_NAME_MAX,
    LIVE_COLOR_HEX,
    TICKET_PANEL_BODY_MAX,
    TICKET_PANEL_BUTTON_MAX,
    TICKET_PANEL_DEFAULTS,
    TICKET_PANEL_TITLE_MAX,
    type DiscordConfig,
    type PinnedSlot
  } from '@bagel/shared';
  import type { SaveState } from '@bagel/shared/components/SaveStatus.svelte';
  import type { DiscordEntry } from '$lib/server/discord-store';
  import DiscordEmbedPreview from './DiscordEmbedPreview.svelte';

  let { data } = $props();
  const { t } = getI18n();

  // ── local draft ─────────────────────────────────────────────────────────
  // One object is the whole payload: it is what the hidden `config` field
  // carries, what the dirty guard compares, and what every control writes to.
  // svelte-ignore state_referenced_locally
  let enabled = $state<boolean>(data.enabled ?? false);
  // svelte-ignore state_referenced_locally
  let config = $state<DiscordConfig>({ ...data.config });
  // svelte-ignore state_referenced_locally
  let seed = data;
  // svelte-ignore state_referenced_locally
  let baseline = $state(JSON.stringify(data.config));
  let busy = $state(false);
  $effect(() => {
    if (data !== seed) {
      seed = data;
      enabled = data.enabled ?? false;
      config = { ...data.config };
      baseline = JSON.stringify(data.config);
    }
  });

  const payload = $derived(JSON.stringify(config));
  const dirty = $derived(payload !== baseline);

  function set(field: keyof DiscordConfig, value: string) {
    config[field] = value;
  }

  function setFlag(field: keyof DiscordConfig, on: boolean) {
    config[field] = flagValue(on);
  }

  // ── save plumbing ───────────────────────────────────────────────────────
  let saveState = $state<SaveState>('idle');
  let saveTimer: ReturnType<typeof setTimeout> | undefined;
  function markSave(s: SaveState, resetAfter = 0) {
    clearTimeout(saveTimer);
    saveState = s;
    if (resetAfter) saveTimer = setTimeout(() => (saveState = 'idle'), resetAfter);
  }

  type ActionResult = { ok?: boolean; error?: string; code?: string; refused?: string };
  function payloadOf(result: unknown): ActionResult | undefined {
    const r = result as { type: string; data?: ActionResult };
    return r.type === 'success' || r.type === 'failure' ? r.data : undefined;
  }

  // The refusal contract: switch on `code`, fall back to the sentence outgress
  // sent while it is still the only thing an older deployment returns.
  const CODE_KEYS: Record<
    string,
    | 'discord.errBoundElsewhere'
    | 'discord.errNotBound'
    | 'discord.errUnavailable'
    | 'discord.errForbidden'
    | 'discord.errRateLimited'
    | 'discord.errInvalid'
  > = {
    bound_elsewhere: 'discord.errBoundElsewhere',
    not_bound: 'discord.errNotBound',
    discord_unavailable: 'discord.errUnavailable',
    forbidden: 'discord.errForbidden',
    rate_limited: 'discord.errRateLimited',
    invalid: 'discord.errInvalid'
  };

  const SLUG_KEYS: Record<
    string,
    | 'discord.errOauth'
    | 'discord.errUnconfigured'
    | 'discord.errSetup'
    | 'discord.errState'
    | 'discord.errBoundElsewhere'
    | 'discord.errNotBound'
    | 'discord.errUnavailable'
    | 'discord.errForbidden'
    | 'discord.errRateLimited'
    | 'discord.errInvalid'
  > = {
    oauth: 'discord.errOauth',
    unconfigured: 'discord.errUnconfigured',
    setup: 'discord.errSetup',
    state: 'discord.errState',
    ...CODE_KEYS
  };

  function refusalText(p: ActionResult | undefined, fallback: string): string {
    const key = p?.code ? CODE_KEYS[p.code] : undefined;
    if (key) return t(key);
    return p?.error ?? fallback;
  }

  function succeeded(result: { type: string }, p: ActionResult | undefined): boolean {
    return result.type === 'success' && p?.ok !== false;
  }

  const saveSubmit: SubmitFunction = () => {
    busy = true;
    markSave('saving');
    return async ({ result }) => {
      busy = false;
      const p = payloadOf(result);
      if (succeeded(result, p)) {
        markSave('saved', 4000);
        toast('ok', t('discord.toastSaved'));
        await invalidateAll();
        return;
      }
      markSave('error', 4000);
      toast('err', refusalText(p, t('discord.toastSaveFailed')));
    };
  };

  // One factory instead of three near-identical closures: each one-shot action
  // differs only in which two strings it toasts.
  function actionSubmit(okMsg: string, failMsg: string): SubmitFunction {
    return () => {
      busy = true;
      return async ({ result }) => {
        busy = false;
        const p = payloadOf(result);
        if (succeeded(result, p)) {
          toast('ok', okMsg);
          await invalidateAll();
          return;
        }
        toast('err', refusalText(p, failMsg));
      };
    };
  }

  const setupSubmit: SubmitFunction = () => {
    busy = true;
    return async ({ result }) => {
      busy = false;
      const p = payloadOf(result);
      if (succeeded(result, p)) {
        toast(p?.refused ? 'err' : 'ok', p?.refused ? t('discord.toastRefused') : t('discord.toastSetup'));
        await invalidateAll();
        return;
      }
      toast('err', refusalText(p, t('discord.toastSetupFailed')));
    };
  };

  const repostSubmit = $derived(actionSubmit(t('discord.toastReposted'), t('discord.toastRepostFailed')));
  const disconnectSubmit = $derived(
    actionSubmit(t('discord.toastDisconnected'), t('discord.toastDisconnectFailed'))
  );

  // ── dirty guard ─────────────────────────────────────────────────────────
  let pendingHref = $state('');
  let discardOpen = $state(false);
  beforeNavigate((nav) => {
    if (!dirty) return;
    if (!nav.to) return;
    if (nav.to.url.pathname === '/discord') return;
    if (pendingHref === nav.to.url.href) return;
    nav.cancel();
    pendingHref = nav.to.url.href;
    discardOpen = true;
  });
  function confirmDiscard() {
    discardOpen = false;
    // Baseline moves to the current draft so the second navigation is no
    // longer dirty and beforeNavigate lets it through.
    baseline = payload;
    if (pendingHref) goto(pendingHref);
  }
  function cancelDiscard() {
    discardOpen = false;
    pendingHref = '';
  }

  let disconnectOpen = $state(false);
  let disconnectForm = $state<HTMLFormElement | undefined>();

  // ── layout ──────────────────────────────────────────────────────────────
  // Discord channel types: 0 text, 2 voice. Categories arrive as their own
  // list from outgress rather than being sieved out of channels by type.
  const textChannels = $derived((data.layout?.channels ?? []).filter((c: DiscordEntry) => c.type === 0));
  const voiceChannels = $derived((data.layout?.channels ?? []).filter((c: DiscordEntry) => c.type === 2));
  const categories = $derived(data.layout?.categories ?? []);
  const roles = $derived((data.layout?.roles ?? []).filter((r: DiscordEntry) => r.name !== '@everyone'));
  // Each picker asks its own list, so a reply that carried channels but no
  // roles disables the role pickers alone instead of the whole page.
  const layoutDown = $derived(
    data.connected && textChannels.length === 0 && roles.length === 0 && categories.length === 0
  );

  const guild = $derived(data.status?.guildPresent ? data.status.guild : (data.layout?.guild ?? data.status.guild));
  const guildName = $derived(guild.name || t('discord.unknownServer'));
  const monogram = $derived(guildName.trim().slice(0, 2).toUpperCase());

  // Discord serves guild icons from its own CDN, and the console CSP is
  // img-src 'self' data: — an <img> pointed at cdn.discordapp.com renders as a
  // broken box with a console error and no way to fix it short of proxying
  // every guild icon through the dashboard. A monogram tile costs nothing,
  // never 404s and cannot leak the visit to Discord.
  const botOnline = $derived(data.status?.online === true && data.status?.guildPresent === true);
  const needsReauth = $derived(data.status?.needsReauth === true || data.layout?.needsReauth === true);

  // Only the fatal close codes get their own sentence: they are the ones the
  // streamer can act on. Everything else is a transient disconnect the
  // gateway retries on its own, so it reads as "reconnecting".
  const CLOSE_KEYS: Record<
    number,
    'discord.close4004' | 'discord.close4013' | 'discord.close4014' | 'discord.close4011'
  > = {
    4004: 'discord.close4004',
    4011: 'discord.close4011',
    4013: 'discord.close4013',
    4014: 'discord.close4014'
  };
  const closeKey = $derived(CLOSE_KEYS[data.status?.lastCloseCode ?? 0]);

  // now stays 0 until the browser sets it, so the server and the first client
  // render agree: an uptime rendered during SSR is stale by the time it lands
  // and hydration screams about the mismatch.
  let now = $state(0);
  $effect(() => {
    now = Date.now();
  });

  const SINCE_KEYS = {
    minutes: 'discord.sinceMinutes',
    hours: 'discord.sinceHours',
    days: 'discord.sinceDays'
  } as const;

  const uptime = $derived(sinceLabel(data.status?.sinceMs ?? 0));
  function sinceLabel(sinceMs: number): string {
    if (!now || !sinceMs) return '';
    const minutes = Math.max(0, Math.round((now - sinceMs) / 60000));
    if (minutes < 60) return t(SINCE_KEYS.minutes, { n: String(minutes) });
    if (minutes < 1440) return t(SINCE_KEYS.hours, { n: String(Math.round(minutes / 60)) });
    return t(SINCE_KEYS.days, { n: String(Math.round(minutes / 1440)) });
  }

  const memberCount = $derived(guild.memberCount > 0 ? guild.memberCount.toLocaleString() : '');

  // ── connect wizard ──────────────────────────────────────────────────────
  const WIZARD_STEPS = $derived([t('discord.stepInvite'), t('discord.stepLayout'), t('discord.stepDone')]);
  let wizardStep = $state('');
  const wizardValue = $derived(wizardStep || WIZARD_STEPS[0]);

  // ── roles ───────────────────────────────────────────────────────────────
  type RoleRow = { slot: PinnedSlot; field: keyof DiscordConfig; label: string; help: string };

  const staffRoles = $derived<RoleRow[]>([
    { slot: 'owner', field: 'ownerRoleId', label: t('discord.ownerRoleLabel'), help: t('discord.ownerRoleHelp') },
    { slot: 'leadMod', field: 'leadModRoleId', label: t('discord.leadModRoleLabel'), help: t('discord.leadModRoleHelp') },
    { slot: 'mods', field: 'modsRoleId', label: t('discord.modsRoleLabel'), help: t('discord.modsRoleHelp') }
  ]);
  const tierRoles = $derived<RoleRow[]>([
    { slot: 'vip', field: 'vipRoleId', label: t('discord.vipRoleLabel'), help: t('discord.vipRoleHelp') },
    { slot: 'subscriber', field: 'subscriberRoleId', label: t('discord.subscriberRoleLabel'), help: t('discord.subscriberRoleHelp') },
    { slot: 'regulars', field: 'regularsRoleId', label: t('discord.regularsRoleLabel'), help: t('discord.regularsRoleHelp') },
    { slot: 'member', field: 'memberRoleId', label: t('discord.memberRoleLabel'), help: t('discord.memberRoleHelp') }
  ]);

  const pins = $derived(parsePinnedRoles(config.pinnedRoles));

  function isPinned(row: RoleRow): boolean {
    const id = config[row.field];
    return id !== '' && pins[row.slot] === id;
  }

  // Pinning a slot tells setup to adopt the role that is selected right now
  // instead of creating (or renaming) one by name on the next fill.
  function togglePin(row: RoleRow) {
    const next = { ...pins };
    if (isPinned(row)) delete next[row.slot];
    else if (config[row.field]) next[row.slot] = config[row.field];
    set('pinnedRoles', encodePinnedRoles(next));
  }

  // ── data-declared switch loops ──────────────────────────────────────────
  type SwitchRow = { field: keyof DiscordConfig; label: string; help: string; defaultOn: boolean };

  const postSwitches = $derived<SwitchRow[]>([
    { field: 'liveEnabled', label: t('discord.liveLabel'), help: t('discord.liveHelp'), defaultOn: true },
    { field: 'clipsEnabled', label: t('discord.clipsLabel'), help: t('discord.clipsHelp'), defaultOn: true }
  ]);

  const communitySwitches = $derived<SwitchRow[]>([
    { field: 'welcomeEnabled', label: t('discord.welcomeLabel'), help: t('discord.welcomeHelp'), defaultOn: true },
    { field: 'goodbyeEnabled', label: t('discord.goodbyeLabel'), help: t('discord.goodbyeHelp'), defaultOn: false },
    { field: 'voiceEnabled', label: t('discord.voiceLabel'), help: t('discord.voiceHelp'), defaultOn: true },
    { field: 'logsEnabled', label: t('discord.logsLabel'), help: t('discord.logsHelp'), defaultOn: true },
    { field: 'levelsEnabled', label: t('discord.levelsLabel'), help: t('discord.levelsHelp'), defaultOn: true },
    { field: 'linkGuardEnabled', label: t('discord.linkGuardLabel'), help: t('discord.linkGuardHelp'), defaultOn: false },
    { field: 'subscribersEnabled', label: t('discord.subscribersLabel'), help: t('discord.subscribersHelp'), defaultOn: false }
  ]);

  function switchOn(row: SwitchRow): boolean {
    return row.defaultOn ? alertOn(config[row.field]) : alertOff(config[row.field]);
  }

  // ── posts: category chips ───────────────────────────────────────────────
  const allowList = $derived(parseNameList(config.categoryAllow));
  const denyList = $derived(parseNameList(config.categoryDeny));
  let allowDraft = $state('');
  let denyDraft = $state('');

  function addName(field: keyof DiscordConfig, raw: string): boolean {
    const value = raw.trim();
    if (value === '' || value.length > CATEGORY_NAME_MAX) return false;
    const next = encodeNameList([...parseNameList(config[field]), value]);
    if (next === config[field]) return false;
    set(field, next);
    return true;
  }

  function removeName(field: keyof DiscordConfig, name: string) {
    set(field, encodeNameList(parseNameList(config[field]).filter((n) => n !== name)));
  }

  function commitAllow() {
    if (addName('categoryAllow', allowDraft)) allowDraft = '';
  }
  function commitDeny() {
    if (addName('categoryDeny', denyDraft)) denyDraft = '';
  }

  // ── ticket desk ─────────────────────────────────────────────────────────
  const LIMIT_OPTIONS = ['1', '2', '3', '4', '5'] as const;
  const staffSelected = $derived(parseIdList(config.ticketStaffRoleIds));

  function toggleStaffRole(id: string, on: boolean) {
    const next = on ? [...staffSelected, id] : staffSelected.filter((r) => r !== id);
    set('ticketStaffRoleIds', encodeIdList(next));
  }

  const panel = $derived(ticketPanelSpec(config));
  // Six presets so the common case is one click and the picker stays for the
  // rest; the first is the colour every other Bagel embed already uses.
  const SWATCHES = [LIVE_COLOR_HEX, '#52b788', '#5865f2', '#dfe4e9', '#b05a46', '#8a7cc9'] as const;
  const panelColor = $derived(normalizeHex(config.ticketPanelColor));
  const ticketsOn = $derived(alertOn(config.ticketsEnabled));
</script>

{#snippet saveBar(label: string)}
  <div class="actions">
    <SaveStatus state={saveState} />
    <Button variant="primary" type="submit" icon="check" loading={busy}>{label}</Button>
  </div>
{/snippet}

{#snippet picker(field: keyof DiscordConfig, label: string, help: string, options: DiscordEntry[], prefix: string)}
  <div class="setting-row">
    <label class="tr-text" for="dc-{field}">
      <span class="tr-label">{label}</span>
      <span class="tr-help" id="dch-{field}">{help}</span>
    </label>
    <select
      id="dc-{field}"
      class="setting-input"
      aria-describedby="dch-{field}"
      disabled={options.length === 0}
      value={config[field]}
      onchange={(e) => set(field, e.currentTarget.value)}
    >
      <option value="">{t('discord.notSet')}</option>
      {#each options as opt (opt.id)}
        <option value={opt.id}>{prefix}{opt.name}</option>
      {/each}
    </select>
  </div>
{/snippet}

{#snippet switchRows(rows: SwitchRow[])}
  {#each rows as row (row.field)}
    <div class="setting-row">
      <span class="tr-text">
        <span class="tr-label">{row.label}</span>
        <span class="tr-help" id="dcs-{row.field}">{row.help}</span>
      </span>
      <Switch
        label={row.label}
        describedby="dcs-{row.field}"
        checked={switchOn(row)}
        onchange={(v) => setFlag(row.field, v)}
      />
    </div>
  {/each}
{/snippet}

{#snippet chipList(field: keyof DiscordConfig, names: string[])}
  <div class="chips">
    {#each names as name (name)}
      <Chip on onclick={() => removeName(field, name)} aria-label={t('discord.chipRemove', { name })}>
        {name}<span class="chip-x" aria-hidden="true">×</span>
      </Chip>
    {/each}
    {#if names.length === 0}
      <span class="tr-help">{t('discord.chipEmpty')}</span>
    {/if}
  </div>
{/snippet}

<section class="screen active">
  <PageHead eyebrow={t('discord.eyebrow')} description={t('discord.description')}>
    {t('discord.titlePre')} <em>{t('discord.titleEm')}</em>
  </PageHead>

  <!-- Discord is premium-only while in beta. The route guard lets this page
       load rather than bouncing to /modules, because Discord has no tile
       there any more and a silent redirect explains nothing. The panel is its
       own top-level block instead of wrapping the page in an else-branch, and
       every action refuses server-side regardless of what renders here. -->
  {#if data.locked}
    <section class="block reveal" style="--i:0" aria-labelledby="dc-locked-h">
      <h2 id="dc-locked-h" class="block-title">{t('modules.betaLocked')}</h2>
      <Card>
        <p class="lead"><Chip on>{t('modules.betaChip')}</Chip></p>
        <p class="hint">{t('modules.betaLockedBody')}</p>
        <div class="row">
          <ButtonLink variant="primary" href="/billing" icon="gem">{t('modules.betaUpgrade')}</ButtonLink>
        </div>
      </Card>
    </section>
  {/if}

  {#if !data.locked}
    {#if data.degraded}
      <AlertBanner>{t('discord.degraded')}</AlertBanner>
    {/if}

    {#if data.errorSlug && SLUG_KEYS[data.errorSlug]}
      <AlertBanner variant="warn" icon="ban">{t(SLUG_KEYS[data.errorSlug])}</AlertBanner>
    {/if}

    {#if data.justConnected && data.refused}
      <AlertBanner variant="warn" icon="server">{t('discord.connectedLivedIn')}</AlertBanner>
    {/if}

    <!-- One banner for the whole page rather than a raw-id input per picker:
         the ids are wire detail nobody should be asked to copy by hand, so a
         layout outage disables the controls and says why, once. -->
    {#if layoutDown}
      <AlertBanner variant="warn" icon="server">{t('discord.layoutUnavailable')}</AlertBanner>
    {/if}

    <!--
      Shown only when Discord actually refused the premium rename in this
      guild. Discord freezes a bot's permissions into its role at install, so a
      server that added Bagel before the bot asked for Change Nickname keeps the
      old grant forever: the premium avatar applies, the name does not, and
      nothing self-heals until the streamer re-authorizes.
    -->
    {#if needsReauth}
      <AlertBanner variant="warn" icon="power">
        {t('discord.reauthNeeded')}
        {#snippet action()}
          <ButtonLink variant="secondary" href="/discord/connect" data-sveltekit-reload>
            {t('discord.reauthCta')}
          </ButtonLink>
        {/snippet}
      </AlertBanner>
    {/if}

    <PageToolbar>
      {#snippet lead()}
        <MasterToggle
          action="?/toggle"
          bind:enabled
          label={t('discord.masterLabel')}
          hint={enabled ? t('discord.masterHintOn') : t('discord.masterHintOff')}
          ariaLabel={t('discord.masterAria')}
          failMessage={t('discord.masterFail')}
        />
      {/snippet}
      {#snippet trail()}
        {#if data.connected}
          <ButtonLink variant="ghost" icon="power" href="/discord/connect" data-sveltekit-reload>
            {t('discord.reconnectCta')}
          </ButtonLink>
          <Button variant="destructive" icon="ban" onclick={() => (disconnectOpen = true)}>
            {t('discord.disconnectCta')}
          </Button>
        {:else if data.configured}
          <ButtonLink variant="primary" icon="discord" href="/discord/connect" data-sveltekit-reload>
            {t('discord.connectCta')}
          </ButtonLink>
        {/if}
      {/snippet}
    </PageToolbar>

    <!-- 1) Server and bot status. -->
    <section class="block reveal" style="--i:1" aria-labelledby="dc-status-h">
      <h2 id="dc-status-h" class="block-title">{t('discord.statusTitle')}</h2>
      <Card>
        {#if data.connected}
          <div class="server">
            <span class="crest" aria-hidden="true">{monogram}</span>
            <span class="server-copy">
              <span class="server-name">{guildName}</span>
              <span class="tr-help">
                {#if memberCount}{t('discord.statusMembers', { n: memberCount })}{:else}{t('discord.statusNoMembers')}{/if}
              </span>
            </span>
            <span class="pill {botOnline ? 'on' : 'off'}">
              <Icon name={botOnline ? 'check' : 'ban'} size={13} />
              {botOnline ? t('discord.statusOnline') : t('discord.statusOffline')}
            </span>
          </div>

          <dl class="facts">
            <div class="fact">
              <dt>{t('discord.statusSince')}</dt>
              <dd>{uptime || t('discord.statusUnknown')}</dd>
            </div>
            <div class="fact">
              <dt>{t('discord.statusResumes')}</dt>
              <dd>{data.status?.sessionResumes ?? 0}</dd>
            </div>
          </dl>

          {#if !botOnline && closeKey}
            <p class="hint">{t(closeKey)}</p>
          {:else if !botOnline}
            <p class="hint">{t('discord.statusReconnecting')}</p>
          {/if}
        {:else}
          <p class="hint">{t('discord.statusNotConnected')}</p>
        {/if}
      </Card>
    </section>

    <!-- 2) Connect wizard: three steps, no ids, only while disconnected. -->
    {#if !data.connected}
      <section class="block reveal" style="--i:2" aria-labelledby="dc-connect-h">
        <h2 id="dc-connect-h" class="block-title">{t('discord.connectTitle')}</h2>
        <Card>
          <p class="hint">{t('discord.connectHelp')}</p>
          <SegmentedControl options={WIZARD_STEPS} bind:value={() => wizardValue, (v) => (wizardStep = v)} label={t('discord.wizardLabel')} />

          <div class="wizard">
            {#if wizardValue === WIZARD_STEPS[0]}
              <p class="hint">{t('discord.stepInviteBody')}</p>
              <div class="row">
                {#if data.templateURL}
                  <ButtonLink variant="secondary" icon="plus" href={data.templateURL} target="_blank" rel="noopener noreferrer">
                    {t('discord.createCta')}
                  </ButtonLink>
                {/if}
                {#if data.configured}
                  <ButtonLink variant="primary" icon="discord" href="/discord/connect" data-sveltekit-reload>
                    {t('discord.connectCta')}
                  </ButtonLink>
                {:else}
                  <Button variant="primary" type="button" disabled>{t('discord.connectCta')}</Button>
                {/if}
              </div>
              {#if !data.configured}
                <p class="hint">{t('discord.connectUnconfigured')}</p>
              {/if}
            {:else if wizardValue === WIZARD_STEPS[1]}
              <p class="hint">{t('discord.stepLayoutBody')}</p>
            {:else}
              <p class="hint">{t('discord.stepDoneBody')}</p>
            {/if}
          </div>
        </Card>
      </section>
    {/if}

    {#if data.connected}
      <!-- 3) Channels, grouped by what they are for. -->
      <section class="block reveal" style="--i:3" aria-labelledby="dc-channels-h">
        <h2 id="dc-channels-h" class="block-title">{t('discord.channelsTitle')}</h2>
        <Card>
          <form method="POST" action="?/save" use:enhance={saveSubmit} novalidate>
            <input type="hidden" name="config" value={payload} />
            <p class="hint">{t('discord.channelsHelp')}</p>

            <h3 class="group">{t('discord.groupAnnounce')}</h3>
            {@render picker('liveChannelId', t('discord.liveChannelLabel'), t('discord.liveChannelHelp'), textChannels, '#')}
            {@render picker('clipsChannelId', t('discord.clipsChannelLabel'), t('discord.clipsChannelHelp'), textChannels, '#')}

            <h3 class="group">{t('discord.groupCommunity')}</h3>
            {@render picker('welcomeChannelId', t('discord.welcomeChannelLabel'), t('discord.welcomeChannelHelp'), textChannels, '#')}
            {@render picker('voiceHubId', t('discord.voiceHubLabel'), t('discord.voiceHubHelp'), voiceChannels, '')}
            {@render picker('logChannelId', t('discord.logChannelLabel'), t('discord.logChannelHelp'), textChannels, '#')}

            <h3 class="group">{t('discord.groupSubs')}</h3>
            {@render picker('subsChannelId', t('discord.subsChannelLabel'), t('discord.subsChannelHelp'), textChannels, '#')}
            {@render picker('subsCategoryId', t('discord.subsCategoryLabel'), t('discord.subsCategoryHelp'), categories, '')}

            <h3 class="group">{t('discord.groupVip')}</h3>
            {@render picker('vipChannelId', t('discord.vipChannelLabel'), t('discord.vipChannelHelp'), textChannels, '#')}
            {@render picker('vipCategoryId', t('discord.vipCategoryLabel'), t('discord.vipCategoryHelp'), categories, '')}

            {@render saveBar(t('discord.save'))}
          </form>
        </Card>
      </section>

      <!-- 4) Roles: staff and tiers, each pinnable to a role you already have. -->
      <section class="block reveal" style="--i:4" aria-labelledby="dc-roles-h">
        <h2 id="dc-roles-h" class="block-title">{t('discord.rolesTitle')}</h2>
        <Card>
          <form method="POST" action="?/save" use:enhance={saveSubmit} novalidate>
            <input type="hidden" name="config" value={payload} />
            <p class="hint">{t('discord.rolesHelp')}</p>

            <h3 class="group">{t('discord.groupStaff')}</h3>
            {#each staffRoles as row (row.slot)}
              <div class="setting-row">
                <label class="tr-text" for="dc-{row.field}">
                  <span class="tr-label">{row.label}</span>
                  <span class="tr-help" id="dch-{row.field}">{row.help}</span>
                </label>
                <span class="role-controls">
                  <Chip
                    on={isPinned(row)}
                    onclick={() => togglePin(row)}
                    aria-pressed={isPinned(row)}
                    disabled={config[row.field] === ''}
                  >
                    {t('discord.pinnedChip')}
                  </Chip>
                  <select
                    id="dc-{row.field}"
                    class="setting-input"
                    aria-describedby="dch-{row.field}"
                    disabled={roles.length === 0}
                    value={config[row.field]}
                    onchange={(e) => set(row.field, e.currentTarget.value)}
                  >
                    <option value="">{t('discord.notSet')}</option>
                    {#each roles as opt (opt.id)}
                      <option value={opt.id}>@{opt.name}</option>
                    {/each}
                  </select>
                </span>
              </div>
            {/each}

            <h3 class="group">{t('discord.groupTiers')}</h3>
            {#each tierRoles as row (row.slot)}
              <div class="setting-row">
                <label class="tr-text" for="dc-{row.field}">
                  <span class="tr-label">{row.label}</span>
                  <span class="tr-help" id="dch-{row.field}">{row.help}</span>
                </label>
                <span class="role-controls">
                  <Chip
                    on={isPinned(row)}
                    onclick={() => togglePin(row)}
                    aria-pressed={isPinned(row)}
                    disabled={config[row.field] === ''}
                  >
                    {t('discord.pinnedChip')}
                  </Chip>
                  <select
                    id="dc-{row.field}"
                    class="setting-input"
                    aria-describedby="dch-{row.field}"
                    disabled={roles.length === 0}
                    value={config[row.field]}
                    onchange={(e) => set(row.field, e.currentTarget.value)}
                  >
                    <option value="">{t('discord.notSet')}</option>
                    {#each roles as opt (opt.id)}
                      <option value={opt.id}>@{opt.name}</option>
                    {/each}
                  </select>
                </span>
              </div>
            {/each}

            <div class="setting-row">
              <span class="tr-text">
                <span class="tr-label">{t('discord.autoRoleLabel')}</span>
                <span class="tr-help" id="dcs-autoRole">{t('discord.autoRoleHelp')}</span>
              </span>
              <Switch
                label={t('discord.autoRoleLabel')}
                describedby="dcs-autoRole"
                checked={alertOn(config.autoRoleEnabled)}
                onchange={(v) => setFlag('autoRoleEnabled', v)}
              />
            </div>

            <p class="hint">{t('discord.pinHelp')}</p>
            {@render saveBar(t('discord.save'))}
          </form>
        </Card>
      </section>

      <!-- 5) Stream posts + the category allow/deny lists. -->
      <section class="block reveal" style="--i:5" aria-labelledby="dc-posts-h">
        <h2 id="dc-posts-h" class="block-title">{t('discord.postsTitle')}</h2>
        <Card>
          <form method="POST" action="?/save" use:enhance={saveSubmit} novalidate>
            <input type="hidden" name="config" value={payload} />
            <p class="hint">{t('discord.postsHelp')}</p>
            {@render switchRows(postSwitches)}

            <div class="setting-row stacked">
              <span class="tr-text">
                <span class="tr-label">{t('discord.allowLabel')}</span>
                <span class="tr-help" id="dch-allow">{t('discord.allowTag')}</span>
              </span>
              {@render chipList('categoryAllow', allowList)}
              <span class="adder">
                <input
                  class="setting-input"
                  aria-describedby="dch-allow"
                  aria-label={t('discord.allowLabel')}
                  maxlength={CATEGORY_NAME_MAX}
                  placeholder={t('discord.allowPlaceholder')}
                  bind:value={allowDraft}
                  onkeydown={(e) => {
                    if (e.key !== 'Enter') return;
                    e.preventDefault();
                    commitAllow();
                  }}
                />
                <Button variant="secondary" icon="plus" onclick={commitAllow}>{t('discord.chipAdd')}</Button>
              </span>
            </div>

            <div class="setting-row stacked">
              <span class="tr-text">
                <span class="tr-label">{t('discord.denyLabel')}</span>
                <span class="tr-help" id="dch-deny">{t('discord.denyTag')}</span>
              </span>
              {@render chipList('categoryDeny', denyList)}
              <span class="adder">
                <input
                  class="setting-input"
                  aria-describedby="dch-deny"
                  aria-label={t('discord.denyLabel')}
                  maxlength={CATEGORY_NAME_MAX}
                  placeholder={t('discord.denyPlaceholder')}
                  bind:value={denyDraft}
                  onkeydown={(e) => {
                    if (e.key !== 'Enter') return;
                    e.preventDefault();
                    commitDeny();
                  }}
                />
                <Button variant="secondary" icon="plus" onclick={commitDeny}>{t('discord.chipAdd')}</Button>
              </span>
            </div>

            {@render saveBar(t('discord.save'))}
          </form>
        </Card>
      </section>

      <!-- 6) Community ops: one declared list, one loop. -->
      <section class="block reveal" style="--i:6" aria-labelledby="dc-community-h">
        <h2 id="dc-community-h" class="block-title">{t('discord.communityTitle')}</h2>
        <Card>
          <form method="POST" action="?/save" use:enhance={saveSubmit} novalidate>
            <input type="hidden" name="config" value={payload} />
            <p class="hint">{t('discord.communityHelp')}</p>
            {@render switchRows(communitySwitches)}
            <p class="hint">{t('discord.tierRolesHelp')}</p>
            {@render saveBar(t('discord.save'))}
          </form>
        </Card>
      </section>

      <!-- 7) Ticket desk, including the panel embed and its live preview. -->
      <section class="block reveal" style="--i:7" aria-labelledby="dc-tickets-h">
        <h2 id="dc-tickets-h" class="block-title">{t('discord.ticketsTitle')}</h2>
        <Card>
          <form method="POST" action="?/save" use:enhance={saveSubmit} novalidate>
            <input type="hidden" name="config" value={payload} />
            <p class="hint">{t('discord.ticketsSectionHelp')}</p>

            <div class="setting-row">
              <span class="tr-text">
                <span class="tr-label">{t('discord.ticketsLabel')}</span>
                <span class="tr-help" id="dcs-tickets">{t('discord.ticketsHelp')}</span>
              </span>
              <Switch
                label={t('discord.ticketsLabel')}
                describedby="dcs-tickets"
                checked={ticketsOn}
                onchange={(v) => setFlag('ticketsEnabled', v)}
              />
            </div>

            {@render picker('ticketChannelId', t('discord.ticketChannelLabel'), t('discord.ticketChannelHelp'), textChannels, '#')}
            {@render picker('ticketCategoryId', t('discord.ticketCategoryLabel'), t('discord.ticketCategoryHelp'), categories, '')}
            {@render picker('ticketArchiveCategoryId', t('discord.ticketArchiveLabel'), t('discord.ticketArchiveHelp'), categories, '')}
            {@render picker('ticketLogChannelId', t('discord.ticketLogLabel'), t('discord.ticketLogHelp'), textChannels, '#')}

            <fieldset class="setting-row stacked staff">
              <legend class="tr-label">{t('discord.staffRolesLabel')}</legend>
              <span class="tr-help" id="dch-staff">{t('discord.staffRolesHelp')}</span>
              {#if roles.length === 0}
                <span class="tr-help">{t('discord.staffRolesEmpty')}</span>
              {:else}
                <div class="checks">
                  {#each roles as role (role.id)}
                    <label class="check">
                      <input
                        type="checkbox"
                        aria-describedby="dch-staff"
                        checked={staffSelected.includes(role.id)}
                        onchange={(e) => toggleStaffRole(role.id, e.currentTarget.checked)}
                      />
                      <span>@{role.name}</span>
                    </label>
                  {/each}
                </div>
              {/if}
            </fieldset>

            <div class="setting-row">
              <span class="tr-text">
                <span class="tr-label">{t('discord.openLimitLabel')}</span>
                <span class="tr-help">{t('discord.openLimitHelp')}</span>
              </span>
              <SegmentedControl
                options={LIMIT_OPTIONS}
                label={t('discord.openLimitLabel')}
                bind:value={
                  () => String(ticketOpenLimitN(config)), (v) => set('ticketOpenLimit', v)
                }
              />
            </div>

            <div class="setting-row">
              <span class="tr-text">
                <span class="tr-label">{t('discord.transcriptLabel')}</span>
                <span class="tr-help" id="dcs-transcript">{t('discord.transcriptHelp')}</span>
              </span>
              <Switch
                label={t('discord.transcriptLabel')}
                describedby="dcs-transcript"
                checked={alertOn(config.ticketTranscriptEnabled)}
                onchange={(v) => setFlag('ticketTranscriptEnabled', v)}
              />
            </div>

            <h3 class="group">{t('discord.panelTitle')}</h3>
            <p class="hint">{t('discord.panelHelp')}</p>

            <div class="setting-row">
              <label class="tr-text" for="dc-panel-title">
                <span class="tr-label">{t('discord.panelTitleLabel')}</span>
                <span class="tr-help">{TICKET_PANEL_DEFAULTS.title}</span>
              </label>
              <input
                id="dc-panel-title"
                class="setting-input"
                maxlength={TICKET_PANEL_TITLE_MAX}
                placeholder={TICKET_PANEL_DEFAULTS.title}
                value={config.ticketPanelTitle}
                oninput={(e) => set('ticketPanelTitle', e.currentTarget.value)}
              />
            </div>

            <div class="setting-row stacked">
              <label class="tr-text" for="dc-panel-body">
                <span class="tr-label">{t('discord.panelBodyLabel')}</span>
                <span class="tr-help">{TICKET_PANEL_DEFAULTS.body}</span>
              </label>
              <textarea
                id="dc-panel-body"
                class="setting-input setting-textarea"
                maxlength={TICKET_PANEL_BODY_MAX}
                placeholder={TICKET_PANEL_DEFAULTS.body}
                value={config.ticketPanelBody}
                oninput={(e) => set('ticketPanelBody', e.currentTarget.value)}
              ></textarea>
            </div>

            <div class="setting-row">
              <label class="tr-text" for="dc-panel-button">
                <span class="tr-label">{t('discord.panelButtonLabel')}</span>
                <span class="tr-help">{TICKET_PANEL_DEFAULTS.button}</span>
              </label>
              <input
                id="dc-panel-button"
                class="setting-input"
                maxlength={TICKET_PANEL_BUTTON_MAX}
                placeholder={TICKET_PANEL_DEFAULTS.button}
                value={config.ticketPanelButton}
                oninput={(e) => set('ticketPanelButton', e.currentTarget.value)}
              />
            </div>

            <div class="setting-row">
              <label class="tr-text" for="dc-panel-color">
                <span class="tr-label">{t('discord.panelColorLabel')}</span>
                <span class="tr-help">{t('discord.panelColorHelp')}</span>
              </label>
              <span class="colors">
                {#each SWATCHES as swatch (swatch)}
                  <button
                    type="button"
                    class="swatch {panelColor === swatch ? 'on' : ''}"
                    style="background: {swatch}"
                    aria-label={t('discord.swatchLabel', { hex: swatch })}
                    aria-pressed={panelColor === swatch}
                    onclick={() => set('ticketPanelColor', swatch)}
                  ></button>
                {/each}
                <input
                  id="dc-panel-color"
                  type="color"
                  class="color-input"
                  value={panelColor}
                  oninput={(e) => set('ticketPanelColor', normalizeHex(e.currentTarget.value))}
                />
              </span>
            </div>

            <DiscordEmbedPreview
              caption={t('discord.panelPreviewCaption')}
              title={panel.title}
              body={panel.body}
              button={panel.button}
              color={panel.color}
            />

            {@render saveBar(t('discord.save'))}
          </form>

          <div class="repost">
            <p class="hint">{t('discord.repostHelp')}</p>
            <form method="POST" action="?/repost" use:enhance={repostSubmit}>
              <Button variant="secondary" type="submit" icon="ticket" loading={busy} disabled={!ticketsOn}>
                {t('discord.repostCta')}
              </Button>
            </form>
          </div>
        </Card>
      </section>

      <!-- 8) Rebuild: re-runs the fill, adopting whatever is pinned. -->
      <section class="block reveal" style="--i:8" aria-labelledby="dc-setup-h">
        <h2 id="dc-setup-h" class="block-title">{t('discord.setupTitle')}</h2>
        <Card>
          <p class="hint">{t('discord.setupHelp')}</p>
          <form method="POST" action="?/setup" use:enhance={setupSubmit}>
            <Button variant="secondary" type="submit" icon="server" loading={busy}>{t('discord.setupCta')}</Button>
          </form>
        </Card>
      </section>
    {/if}

    <form method="POST" action="?/disconnect" use:enhance={disconnectSubmit} bind:this={disconnectForm} hidden></form>

    <ConfirmDialog
      open={disconnectOpen}
      title={t('discord.disconnectTitle')}
      body={t('discord.disconnectBody')}
      confirmLabel={t('discord.disconnectCta')}
      cancelLabel={t('common.cancel')}
      danger
      {busy}
      onConfirm={() => {
        disconnectOpen = false;
        disconnectForm?.requestSubmit();
      }}
      onCancel={() => (disconnectOpen = false)}
    />

    <ConfirmDialog
      open={discardOpen}
      title={t('discord.unsavedTitle')}
      body={t('discord.unsavedBody')}
      confirmLabel={t('discord.unsavedConfirm')}
      cancelLabel={t('common.cancel')}
      danger
      onConfirm={confirmDiscard}
      onCancel={cancelDiscard}
    />
  {/if}
</section>

<style>
  .screen { display: flex; flex-direction: column; gap: 18px; }
  .block { margin: 0; }
  .block-title {
    font-family: var(--bb-font-display);
    font-weight: 700;
    font-size: 16px;
    letter-spacing: -0.01em;
    color: var(--bb-white);
    margin: 0 0 12px;
  }
  .group {
    font-family: var(--bb-font-body);
    font-size: 13px;
    font-weight: 600;
    color: var(--bb-white);
    margin: 20px 0 2px;
  }
  .hint {
    margin: 0 0 14px;
    font-family: var(--bb-font-body);
    font-size: 12.5px;
    line-height: 1.55;
    color: var(--bb-muted);
  }
  .lead { margin: 0 0 12px; }
  .row { display: flex; gap: 10px; align-items: center; flex-wrap: wrap; }

  /* ── status card ── */
  .server { display: flex; align-items: center; gap: 14px; flex-wrap: wrap; }
  .crest {
    flex: none;
    width: 44px;
    height: 44px;
    border-radius: 8px;
    display: grid;
    place-items: center;
    background: rgba(201, 168, 124, 0.12);
    border: 1px solid var(--glass-border);
    color: var(--bb-tan-light);
    font-family: var(--bb-font-display);
    font-weight: 700;
    font-size: 15px;
    letter-spacing: 0.02em;
  }
  .server-copy { display: flex; flex-direction: column; gap: 2px; min-width: 0; flex: 1; }
  .server-name {
    font-family: var(--bb-font-display);
    font-weight: 700;
    font-size: 15px;
    color: var(--bb-white);
    overflow-wrap: anywhere;
  }
  .pill {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    padding: 5px 12px;
    border-radius: var(--bb-radius-pill);
    font-family: var(--bb-font-mono);
    font-size: 11px;
    letter-spacing: 0.06em;
    text-transform: uppercase;
    border: 1px solid var(--glass-border);
  }
  /* Green / red from the console status quad, never colour alone: each pill
     carries its own icon and its own word. */
  .pill.on { color: var(--bb-green-glow); background: rgba(82, 183, 136, 0.12); }
  .pill.off { color: #cf8a78; background: rgba(176, 90, 70, 0.12); }

  .facts { display: flex; flex-wrap: wrap; gap: 26px; margin: 16px 0 0; }
  .fact { display: flex; flex-direction: column; gap: 2px; }
  .facts dt {
    font-family: var(--bb-font-body);
    font-size: 11px;
    letter-spacing: 0.04em;
    text-transform: uppercase;
    color: var(--bb-muted);
  }
  .facts dd {
    margin: 0;
    font-family: var(--bb-font-mono);
    font-size: 13px;
    color: var(--bb-white);
    font-variant-numeric: tabular-nums;
  }

  .wizard { margin-top: 14px; display: flex; flex-direction: column; gap: 12px; }
  .wizard .hint { margin: 0; }

  /* ── setting rows ── */
  .setting-row {
    display: grid;
    grid-template-columns: minmax(0, 1fr) auto;
    gap: 12px;
    align-items: center;
    padding: 12px 0;
    border-top: 1px solid var(--glass-border);
  }
  .setting-row.stacked {
    grid-template-columns: minmax(0, 1fr);
    align-items: stretch;
  }
  .setting-row.staff { border: none; border-top: 1px solid var(--glass-border); margin: 0; }
  .setting-row.staff legend { padding: 0; }
  .tr-text { display: flex; flex-direction: column; gap: 4px; min-width: 0; }
  .tr-label { font-family: var(--bb-font-body); font-size: 13.5px; color: var(--bb-white); }
  .tr-help { font-family: var(--bb-font-body); font-size: 12.5px; color: var(--bb-muted); line-height: 1.45; }

  .setting-input {
    width: min(280px, 52vw);
    padding: 8px 12px;
    border: 1px solid var(--rule);
    border-radius: 6px;
    background: rgba(240, 236, 228, 0.04);
    color: var(--bb-white);
    font-family: var(--bb-font-body);
    font-size: 13px;
    transition: border-color var(--bb-dur-fast) ease;
  }
  .setting-input:focus { outline: none; border-color: var(--bb-tan); }
  .setting-input:disabled { opacity: 0.5; cursor: not-allowed; }
  .setting-input::placeholder { color: var(--bb-muted); opacity: 0.7; }
  select.setting-input { appearance: auto; }
  select.setting-input option { color: #1a1814; }
  .setting-row.stacked .setting-input { width: 100%; box-sizing: border-box; }
  .setting-textarea { min-height: 96px; line-height: 1.55; resize: vertical; }

  .role-controls { display: flex; align-items: center; gap: 8px; justify-self: end; flex-wrap: wrap; }

  .chips { display: flex; flex-wrap: wrap; gap: 8px; margin: 10px 0; align-items: center; }
  .chip-x { margin-left: 6px; opacity: 0.7; }
  .adder { display: flex; gap: 8px; align-items: center; flex-wrap: wrap; }
  .adder .setting-input { flex: 1; min-width: 0; }

  .checks {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(11rem, 1fr));
    gap: 8px 14px;
    margin-top: 10px;
  }
  .check {
    display: flex;
    align-items: center;
    gap: 8px;
    font-family: var(--bb-font-body);
    font-size: 13px;
    color: var(--bb-white);
    min-width: 0;
  }
  .check span { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .check input { accent-color: var(--bb-tan); width: 15px; height: 15px; }

  .colors { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; justify-self: end; }
  .swatch {
    width: 26px;
    height: 26px;
    border-radius: 6px;
    border: 1px solid var(--glass-border);
    cursor: pointer;
    padding: 0;
    transition: transform var(--bb-dur-fast) var(--bb-ease-out-expo), border-color var(--bb-dur-fast) ease;
  }
  .swatch:hover { transform: translateX(2px); }
  .swatch.on { border-color: var(--bb-white); }
  .color-input {
    width: 40px;
    height: 26px;
    padding: 0;
    border: 1px solid var(--rule);
    border-radius: 6px;
    background: transparent;
    cursor: pointer;
  }

  .repost { margin-top: 18px; padding-top: 14px; border-top: 1px solid var(--glass-border); }
  .repost .hint { margin-bottom: 10px; }

  .actions { display: flex; align-items: center; justify-content: flex-end; gap: 12px; margin-top: 18px; }

  @media (max-width: 560px) {
    .setting-row { grid-template-columns: minmax(0, 1fr); }
    .setting-input { width: 100%; box-sizing: border-box; }
    .role-controls,
    .colors { justify-self: start; }
    .actions { flex-wrap: wrap; }
  }
</style>
