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
    guildBotState,
    guildMonogram,
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
  import { DISCORD_CODE_KEYS, DISCORD_PILL_KEYS } from '$lib/discord-messages';
  import type { DiscordEntry, DiscordGuildSummary } from '$lib/server/discord-store';
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
  // The version the draft was read at. It rides along in every save form so
  // outgress can refuse a write that would clobber someone else's.
  // svelte-ignore state_referenced_locally
  let version = $state<number>(data.version ?? 0);
  let conflicted = $state(false);
  let busy = $state(false);
  $effect(() => {
    if (data !== seed) {
      seed = data;
      enabled = data.enabled ?? false;
      config = { ...data.config };
      baseline = JSON.stringify(data.config);
      version = data.version ?? 0;
    }
  });

  async function reloadPage() {
    conflicted = false;
    // The draft is abandoned deliberately: reseeding from the server is the
    // whole point of the button, and keeping the local edits would just
    // reproduce the conflict on the next save.
    baseline = payload;
    await invalidateAll();
  }

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

  type ActionResult = { ok?: boolean; error?: string; code?: string; refused?: string; fields?: string[] };
  function payloadOf(result: unknown): ActionResult | undefined {
    const r = result as { type: string; data?: ActionResult };
    return r.type === 'success' || r.type === 'failure' ? r.data : undefined;
  }

  // The refusal contract: switch on `code`, fall back to the sentence outgress
  // sent while it is still the only thing an older deployment returns. The
  // tables live in $lib/discord-messages so the server list and this page
  // cannot drift apart.
  function refusalText(p: ActionResult | undefined, fallback: string): string {
    if (p?.code === 'invalid' && p.fields?.length) {
      return t('discord.errInvalidFields', { fields: p.fields.map(fieldLabel).join(', ') });
    }
    const key = p?.code ? DISCORD_CODE_KEYS[p.code] : undefined;
    if (key) return t(key);
    // The translated sentence wins over `p.error`. Actions run on the server,
    // where there is no locale, so anything they phrase themselves is English
    // for every reader; the raw text is a last resort for a refusal this
    // console has no code for at all.
    return fallback || (p?.error ?? '');
  }

  /**
   * A refused field, named the way the page names it.
   *
   * The action reports wire field names (`ticketStaffRoleIds`), which are the
   * one thing this page has spent its whole existence not showing anybody. The
   * map is a typed literal so the i18n scanner sees the keys; a field missing
   * from it degrades to its own name rather than to nothing.
   */
  type I18nKey = Parameters<typeof t>[0];
  const FIELD_LABEL_KEYS: Partial<Record<keyof DiscordConfig, I18nKey>> = {
    liveChannelId: 'discord.liveChannelLabel',
    clipsChannelId: 'discord.clipsChannelLabel',
    welcomeChannelId: 'discord.welcomeChannelLabel',
    voiceHubId: 'discord.voiceHubLabel',
    logChannelId: 'discord.logChannelLabel',
    subsChannelId: 'discord.subsChannelLabel',
    subsCategoryId: 'discord.subsCategoryLabel',
    vipChannelId: 'discord.vipChannelLabel',
    vipCategoryId: 'discord.vipCategoryLabel',
    ticketChannelId: 'discord.ticketChannelLabel',
    ticketCategoryId: 'discord.ticketCategoryLabel',
    ticketArchiveCategoryId: 'discord.ticketArchiveLabel',
    ticketLogChannelId: 'discord.ticketLogLabel',
    ticketStaffRoleIds: 'discord.staffRolesLabel',
    ticketOpenLimit: 'discord.openLimitLabel',
    ticketPanelTitle: 'discord.panelTitleLabel',
    ticketPanelBody: 'discord.panelBodyLabel',
    ticketPanelColor: 'discord.panelColorLabel',
    ticketPanelButton: 'discord.panelButtonLabel',
    ownerRoleId: 'discord.ownerRoleLabel',
    leadModRoleId: 'discord.leadModRoleLabel',
    modsRoleId: 'discord.modsRoleLabel',
    vipRoleId: 'discord.vipRoleLabel',
    subscriberRoleId: 'discord.subscriberRoleLabel',
    regularsRoleId: 'discord.regularsRoleLabel',
    memberRoleId: 'discord.memberRoleLabel',
    pinnedRoles: 'discord.pinnedChip',
    categoryAllow: 'discord.allowLabel',
    categoryDeny: 'discord.denyLabel',
    linkAllowList: 'discord.linkGuardLabel'
  };

  function fieldLabel(field: string): string {
    const key = FIELD_LABEL_KEYS[field as keyof DiscordConfig];
    return key ? t(key) : field;
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
        conflicted = false;
        toast('ok', t('discord.toastSaved'));
        await invalidateAll();
        return;
      }
      markSave('error', 4000);
      conflicted = p?.code === 'conflict';
      toast('err', refusalText(p, t('discord.toastSaveFailed')));
      // An `invalid` refusal means the save DID land: every good field was
      // written and only the named ones kept their stored value. Reseeding is
      // what makes the refused control snap back to what is actually stored
      // instead of showing a draft the server rejected.
      if (p?.code === 'invalid') await invalidateAll();
    };
  };

  /**
   * The one navigation that must not be guarded.
   *
   * Disconnecting redirects to /discord, and there is nothing left to save --
   * the guild is unbound. Exempting the /discord PATH instead, as this did,
   * exempted the server list too: clicking "Discord" in the nav with unsaved
   * changes threw them away silently, which is the exact case the guard
   * exists for.
   */
  let disconnecting = $state(false);

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
        // A refused disconnect never redirects, so the guard has to come back
        // on or the next navigation drops the draft silently.
        disconnecting = false;
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
    if (disconnecting) return;
    if (!dirty) return;
    if (!nav.to) return;
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
  // Discord channel types: 0 text, 2 voice, 5 announcement. Categories arrive
  // as their own list from outgress rather than being sieved out of channels
  // by type.
  //
  // Type 5 belongs in every text picker: an announcement channel takes the
  // same message a text channel does, and #announcements is the single most
  // likely place a streamer wants go-live posts. Filtering on type === 0 alone
  // meant that channel simply was not in the list, with nothing on screen
  // saying why. It is labelled rather than silently mixed in because it
  // behaves differently once posted to -- Discord rate-limits it hard and
  // fans it out to every following server.
  const TEXTLIKE_TYPES = [0, 5];
  const textChannels = $derived(
    (data.layout?.channels ?? []).filter((c: DiscordEntry) => TEXTLIKE_TYPES.includes(c.type))
  );
  const voiceChannels = $derived((data.layout?.channels ?? []).filter((c: DiscordEntry) => c.type === 2));
  const categories = $derived(data.layout?.categories ?? []);
  const roles = $derived((data.layout?.roles ?? []).filter((r: DiscordEntry) => r.name !== '@everyone'));
  // Each picker asks its own list, so a reply that carried channels but no
  // roles disables the role pickers alone instead of the whole page.
  const layoutDown = $derived(textChannels.length === 0 && roles.length === 0 && categories.length === 0);

  const guild = $derived(data.status?.guildPresent ? data.status.guild : (data.layout?.guild ?? data.status.guild));
  const guildName = $derived(guild.name || t('discord.unknownServer'));
  const monogram = $derived(guildMonogram(guildName));

  // Discord serves guild icons from its own CDN, and the console CSP is
  // img-src 'self' data:, so an <img> pointed at cdn.discordapp.com renders as a
  // broken box with a console error and no way to fix it short of proxying
  // every guild icon through the dashboard. A monogram tile costs nothing,
  // never 404s and cannot leak the visit to Discord.
  const botOnline = $derived(data.status?.online === true && data.status?.guildPresent === true);
  const needsReauth = $derived(data.status?.needsReauth === true || data.layout?.needsReauth === true);
  const pillState = $derived(guildBotState({ botPresent: botOnline, needsReauth }));

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

  // ── server switcher ─────────────────────────────────────────────────────
  // A broadcaster owns many servers, so the toolbar's lead slot is how you get
  // from one to the next without going back to the list.
  const otherGuilds = $derived<DiscordGuildSummary[]>(data.guilds ?? []);

  /**
   * Switcher labels, made unique.
   *
   * Discord happily lets one person own two servers with the same name, and
   * SegmentedControl keys its options by their string. Without the suffix the
   * second one would be unclickable and the first would look selected for
   * both.
   */
  function uniqueLabels(names: string[]): string[] {
    const seen = new Map<string, number>();
    return names.map((raw) => {
      const name = raw || t('discord.unknownServer');
      const n = (seen.get(name) ?? 0) + 1;
      seen.set(name, n);
      return n === 1 ? name : `${name} (${n})`;
    });
  }

  const switchLabels = $derived(uniqueLabels(otherGuilds.map((g) => g.name)));
  const switchIndex = $derived(otherGuilds.findIndex((g) => g.guildId === data.guildId));
  const switchValue = $derived(switchLabels[switchIndex] ?? switchLabels[0] ?? '');
  // Segmented up to three, a select past that: four pills already wrap the
  // toolbar on a laptop, and the list page is the right surface for browsing
  // more than a handful.
  const SEGMENTED_MAX = 3;

  function switchTo(guildId: string) {
    if (!guildId || guildId === data.guildId) return;
    goto(`/discord/${guildId}`);
  }

  function switchToLabel(label: string) {
    const i = switchLabels.indexOf(label);
    if (i < 0) return;
    switchTo(otherGuilds[i].guildId);
  }

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

  /** Announcement channels are offered by name plus a marker, so picking one
   *  is a choice rather than a surprise. */
  function optionLabel(opt: DiscordEntry): string {
    return opt.type === 5 ? `${opt.name} ${t('discord.announcementTag')}` : opt.name;
  }
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
        <option value={opt.id}>{prefix}{optionLabel(opt)}</option>
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

    <!--
      A save refused because the row moved under us. Not retried and not
      merged: two drafts of the same server differ in ways only a human can
      reconcile, and the old module-blob save silently reverted whichever mod
      saved first.
    -->
    {#if conflicted}
      <AlertBanner variant="warn" icon="ban">
        {t('discord.conflictBody')}
        {#snippet action()}
          <Button variant="secondary" icon="power" onclick={reloadPage}>{t('discord.conflictCta')}</Button>
        {/snippet}
      </AlertBanner>
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
        <div class="switcher">
          <ButtonLink variant="ghost" icon="list" href="/discord">{t('discord.allServersCta')}</ButtonLink>
          {#if otherGuilds.length > 1 && otherGuilds.length <= SEGMENTED_MAX}
            <SegmentedControl
              options={switchLabels}
              label={t('discord.switcherLabel')}
              bind:value={() => switchValue, switchToLabel}
            />
          {:else if otherGuilds.length > SEGMENTED_MAX}
            <label class="sr-only" for="dc-switcher">{t('discord.switcherLabel')}</label>
            <select
              id="dc-switcher"
              class="setting-input"
              value={data.guildId}
              onchange={(e) => switchTo(e.currentTarget.value)}
            >
              {#each otherGuilds as g, i (g.guildId)}
                <option value={g.guildId}>{switchLabels[i]}</option>
              {/each}
            </select>
          {/if}
        </div>
      {/snippet}
      {#snippet trail()}
        <ButtonLink variant="ghost" icon="power" href="/discord/connect" data-sveltekit-reload>
          {t('discord.reconnectCta')}
        </ButtonLink>
        <Button variant="destructive" icon="ban" onclick={() => (disconnectOpen = true)}>
          {t('discord.disconnectCta')}
        </Button>
      {/snippet}
    </PageToolbar>

    <!-- 1) Server and bot status. -->
    <section class="block reveal" style="--i:1" aria-labelledby="dc-status-h">
      <h2 id="dc-status-h" class="block-title">{t('discord.statusTitle')}</h2>
      <Card>
        <div class="server">
          <span class="crest" aria-hidden="true">{monogram}</span>
          <span class="server-copy">
            <span class="server-name">{guildName}</span>
            <span class="tr-help">
              {#if memberCount}{t('discord.statusMembers', { n: memberCount })}{:else}{t('discord.statusNoMembers')}{/if}
            </span>
          </span>
          <span class="pill {pillState}">
            <Icon name={pillState === 'online' ? 'check' : pillState === 'reauth' ? 'power' : 'ban'} size={13} />
            {t(DISCORD_PILL_KEYS[pillState])}
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
      </Card>
    </section>

    <!-- 2) Channels, grouped by what they are for. -->
    <section class="block reveal" style="--i:2" aria-labelledby="dc-channels-h">
      <h2 id="dc-channels-h" class="block-title">{t('discord.channelsTitle')}</h2>
      <Card>
        <form method="POST" action="?/save" use:enhance={saveSubmit} novalidate>
          <input type="hidden" name="config" value={payload} />
          <input type="hidden" name="version" value={version} />
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

    <!-- 3) Roles: staff and tiers, each pinnable to a role you already have. -->
    <section class="block reveal" style="--i:3" aria-labelledby="dc-roles-h">
      <h2 id="dc-roles-h" class="block-title">{t('discord.rolesTitle')}</h2>
      <Card>
        <form method="POST" action="?/save" use:enhance={saveSubmit} novalidate>
          <input type="hidden" name="config" value={payload} />
          <input type="hidden" name="version" value={version} />
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

    <!-- 4) Stream posts + the category allow/deny lists. -->
    <section class="block reveal" style="--i:4" aria-labelledby="dc-posts-h">
      <h2 id="dc-posts-h" class="block-title">{t('discord.postsTitle')}</h2>
      <Card>
        <form method="POST" action="?/save" use:enhance={saveSubmit} novalidate>
          <input type="hidden" name="config" value={payload} />
          <input type="hidden" name="version" value={version} />
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

    <!-- 5) Community ops: one declared list, one loop. -->
    <section class="block reveal" style="--i:5" aria-labelledby="dc-community-h">
      <h2 id="dc-community-h" class="block-title">{t('discord.communityTitle')}</h2>
      <Card>
        <form method="POST" action="?/save" use:enhance={saveSubmit} novalidate>
          <input type="hidden" name="config" value={payload} />
          <input type="hidden" name="version" value={version} />
          <p class="hint">{t('discord.communityHelp')}</p>
          {@render switchRows(communitySwitches)}
          <p class="hint">{t('discord.tierRolesHelp')}</p>
          {@render saveBar(t('discord.save'))}
        </form>
      </Card>
    </section>

    <!-- 6) Ticket desk, including the panel embed and its live preview. -->
    <section class="block reveal" style="--i:6" aria-labelledby="dc-tickets-h">
      <h2 id="dc-tickets-h" class="block-title">{t('discord.ticketsTitle')}</h2>
      <Card>
        <form method="POST" action="?/save" use:enhance={saveSubmit} novalidate>
          <input type="hidden" name="config" value={payload} />
          <input type="hidden" name="version" value={version} />
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

    <!-- 7) Rebuild: re-runs the fill, adopting whatever is pinned. -->
    <section class="block reveal" style="--i:7" aria-labelledby="dc-setup-h">
      <h2 id="dc-setup-h" class="block-title">{t('discord.setupTitle')}</h2>
      <Card>
        <p class="hint">{t('discord.setupHelp')}</p>
        <form method="POST" action="?/setup" use:enhance={setupSubmit}>
          <Button variant="secondary" type="submit" icon="server" loading={busy}>{t('discord.setupCta')}</Button>
        </form>
      </Card>
    </section>

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
        // The redirect the action throws lands on /discord with a draft that
        // no longer has a row to save into, so the guard has to stand down for
        // that one navigation and only that one.
        disconnecting = true;
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
  .pill.online { color: var(--bb-green-glow); background: rgba(82, 183, 136, 0.12); }
  .pill.offline { color: #cf8a78; background: rgba(176, 90, 70, 0.12); }
  .pill.reauth { color: var(--bb-tan-light); background: rgba(201, 168, 124, 0.14); }

  .switcher { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; min-width: 0; }
  .switcher .setting-input { width: min(220px, 60vw); }

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
