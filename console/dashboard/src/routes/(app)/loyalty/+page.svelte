<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { enhance } from '$app/forms';
  import { invalidateAll } from '$app/navigation';
  import type { SubmitFunction } from '@sveltejs/kit';
  import {
    PageHead,
    MasterToggle,
    Switch,
    AlertBanner,
    ButtonLink,
    Button,
    Field,
    EmptyState,
    SaveStatus,
    DeckList,
    toast,
    getI18n,
    LOYALTY_DEFAULTS,
    moduleDef,
    catalogChildren,
    type LoyaltyConfig
  } from '@bagel/shared';
  import type { SaveState } from '@bagel/shared/components/SaveStatus.svelte';
  import ModuleCommandList from '$lib/components/modules/ModuleCommandList.svelte';
  import LoyaltyGameRow from '$lib/components/loyalty/LoyaltyGameRow.svelte';

  let { data } = $props();
  const { t } = getI18n();
  const loyaltyCommands = moduleDef('loyalty')?.commands ?? [];

  // Local source of truth, reseeded when a fresh SSR load lands.
  // svelte-ignore state_referenced_locally
  let enabled = $state<boolean>(data.enabled ?? false);
  // svelte-ignore state_referenced_locally
  let config = $state<LoyaltyConfig>({ ...data.config });
  // Nested wager games: compact on/off rows, not a second inspector. Seeded
  // from the load so a refresh after toggling loyalty off shows them off too.
  function seedGames(src: typeof data) {
    return catalogChildren('loyalty').map((def) => ({
      def,
      enabled: src.games?.find((g) => g.id === def.id)?.enabled ?? false
    }));
  }
  // svelte-ignore state_referenced_locally
  let games = $state(seedGames(data));
  let busy = $state(false);
  // svelte-ignore state_referenced_locally
  let seed = data;
  $effect(() => {
    if (data !== seed) {
      seed = data;
      enabled = data.enabled ?? false;
      config = { ...data.config };
      games = seedGames(data);
    }
  });

  // Turning loyalty off also disables the children server-side. Mirror that
  // here so the nested switches do not stay lit until the next full load.
  let prevOn = enabled;
  $effect(() => {
    if (prevOn && !enabled) {
      for (const g of games) g.enabled = false;
    }
    prevOn = enabled;
  });

  const payload = $derived(JSON.stringify(config));

  // Adjacent save indicator: a polite live region that mirrors the write so the
  // outcome is announced next to the button (the toast is the assertive backup).
  let saveState = $state<SaveState>('idle');
  let saveTimer: ReturnType<typeof setTimeout> | undefined;
  function markSave(s: SaveState, resetAfter = 0) {
    clearTimeout(saveTimer);
    saveState = s;
    if (resetAfter) saveTimer = setTimeout(() => (saveState = 'idle'), resetAfter);
  }

  const saveSubmit: SubmitFunction = () => {
    busy = true;
    markSave('saving');
    return async ({ result }) => {
      busy = false;
      if (result.type === 'success') {
        markSave('saved', 4000);
        toast('ok', t('loyalty.toastSaved'));
        await invalidateAll();
        return;
      }
      markSave('error', 4000);
      toast('err', t('loyalty.toastSaveFailed'));
    };
  };

  // Rate fields, declared as data so the form is one loop.
  const rateFields = $derived([
    { key: 'subPoints', label: t('loyalty.fieldSub'), dflt: LOYALTY_DEFAULTS.subPoints },
    { key: 'resubPoints', label: t('loyalty.fieldResub'), dflt: LOYALTY_DEFAULTS.resubPoints },
    { key: 'giftSubPoints', label: t('loyalty.fieldGift'), dflt: LOYALTY_DEFAULTS.giftSubPoints },
    { key: 'cheerPointsPer100', label: t('loyalty.fieldCheer'), dflt: LOYALTY_DEFAULTS.cheerPointsPer100 },
    { key: 'watchPointsPerTick', label: t('loyalty.fieldWatch'), dflt: LOYALTY_DEFAULTS.watchPointsPerTick }
  ] as const);

  // The granular moderator capabilities, same loop shape. checked ⇔ value >= 0
  // (0 = the default on, -1 = off), matching the rates' convention.
  const permToggles = $derived([
    { key: 'modSetPoints', label: t('loyalty.permSet'), hint: t('loyalty.permSetHint') },
    { key: 'modAdjustPoints', label: t('loyalty.permAdjust'), hint: t('loyalty.permAdjustHint') },
    { key: 'viewerTransfers', label: t('loyalty.permTransfers'), hint: t('loyalty.permTransfersHint') }
  ] as const);

  function hours(seconds: number): string {
    return t('loyalty.hoursShort', { n: (seconds / 3600).toFixed(1) });
  }

  const top = $derived(data.top ?? []);
</script>

<section class="screen active">
  <PageHead eyebrow={t('loyalty.eyebrow')} description={t('loyalty.description')}>
    {t('loyalty.titlePre')}<em>{t('loyalty.titleEm')}</em>
  </PageHead>

  {#if data.degraded}
    <AlertBanner>{t('loyalty.degraded')}</AlertBanner>
  {/if}

  <!-- 1) Module status: the master enable, in its own labelled section. -->
  <section class="block" aria-labelledby="loy-status-h">
    <h2 id="loy-status-h" class="block-title">{t('loyalty.statusTitle')}</h2>
    <div class="card status-row">
      <MasterToggle
        action="?/toggle"
        bind:enabled
        label={t('loyalty.botOn')}
        hint={t('loyalty.botOnHint')}
        ariaLabel={t('loyalty.botOn')}
        failMessage={t('loyalty.toastToggleFailed')}
      />
      <ButtonLink href="/counters" variant="ghost" icon="modules">{t('loyalty.countersLink')}</ButtonLink>
    </div>
  </section>

  <!-- Nested wager games: two rows, not a third settings form. Odds and chat
       lines stay on each game's inspector so this page does not double in height. -->
  {#if games.length}
    <section class="block" aria-labelledby="loy-games-h">
      <h2 id="loy-games-h" class="block-title">{t('loyalty.gamesTitle')}</h2>
      <div class="card">
        <p class="hint">{enabled ? t('loyalty.gamesHint') : t('loyalty.gamesNeedsOn')}</p>
        {#each games as g (g.def.id)}
          <LoyaltyGameRow def={g.def} bind:enabled={g.enabled} loyaltyOn={enabled} />
        {/each}
      </div>
    </section>
  {/if}

  <!-- 2) Configuration: earning rates, an explicit Save form with Field labels. -->
  <section class="block" aria-labelledby="loy-rates-h">
    <h2 id="loy-rates-h" class="block-title">{t('loyalty.ratesTitle')}</h2>
    <div class="card">
      <p class="hint">{t('loyalty.ratesHint')}</p>

      <form method="POST" action="?/save" use:enhance={saveSubmit} class="rates" novalidate>
        <input type="hidden" name="config" value={payload} />

        <Field label={t('loyalty.fieldName')} tag={t('common.optional')}>
          <input class="search" placeholder={t('loyalty.fieldNamePh')} maxlength="32" bind:value={config.pointsName} />
        </Field>

        {#each rateFields as rf (rf.key)}
          <Field label={rf.label} tag={t('loyalty.defaultTag', { n: String(rf.dflt) })}>
            <input class="search num" type="number" min="-1" max="1000000" bind:value={config[rf.key]} />
          </Field>
        {/each}

        <p class="hint">{t('loyalty.tierHint')}</p>

        <!-- Granular moderator capabilities: each toggle maps 0=on / -1=off in
             the same blob the chat side reads per invocation. -->
        <h3 class="perm-title">{t('loyalty.permissionsTitle')}</h3>
        <p class="hint">{t('loyalty.permissionsHint')}</p>

        {#each permToggles as pt (pt.key)}
          <div class="perm">
            <span class="perm-copy">
              <span class="perm-label">{pt.label}</span>
              <span class="perm-hint" id="perm-hint-{pt.key}">{pt.hint}</span>
            </span>
            <Switch
              label={pt.label}
              describedby="perm-hint-{pt.key}"
              checked={config[pt.key] >= 0}
              onchange={(v) => (config[pt.key] = v ? 0 : -1)}
            />
          </div>
        {/each}

        <div class="actions">
          <SaveStatus state={saveState} />
          <Button variant="primary" type="submit" icon="check" loading={busy}>{t('loyalty.save')}</Button>
        </div>
      </form>
    </div>
  </section>

  <!-- 3) Leaderboard: a data table with a caption, column headers and a per-row
       header. Rank is textual (never conveyed by position or colour alone). -->
  <section class="block" aria-labelledby="loy-top-h">
    <h2 id="loy-top-h" class="block-title">{t('loyalty.topTitle')}</h2>
    <div class="card">
      {#if top.length === 0}
        <EmptyState icon="coin" title={t('loyalty.topEmpty')} />
      {:else}
        <div class="tbl-wrap">
          <table class="tbl">
            <caption class="sr-only">{t('loyalty.topCaption')}</caption>
            <thead>
              <tr>
                <th scope="col" class="r">{t('loyalty.colRank')}</th>
                <th scope="col">{t('loyalty.colViewer')}</th>
                <th scope="col" class="r">{t('loyalty.colPoints')}</th>
                <th scope="col" class="r">{t('loyalty.colWatch')}</th>
              </tr>
            </thead>
            <tbody>
              {#each top as row, i (row.viewerId)}
                <tr>
                  <th scope="row" class="r rank">{i + 1}</th>
                  <td>{row.viewerName || row.viewerLogin || row.viewerId}</td>
                  <td class="r">{row.points.toLocaleString()}</td>
                  <td class="r mut">{hours(row.watchSeconds)}</td>
                </tr>
              {/each}
            </tbody>
          </table>
        </div>
      {/if}
    </div>
  </section>

  {#if loyaltyCommands.length}
    <div class="cmd-block">
      <DeckList>
        <ModuleCommandList commands={loyaltyCommands} headingId="loy-chat-h" />
      </DeckList>
    </div>
  {/if}
</section>

<style>
  .block { margin-bottom: 26px; }
  .block-title {
    font-family: var(--bb-font-display);
    font-weight: 700;
    font-size: 16px;
    letter-spacing: -0.01em;
    color: var(--bb-white);
    margin: 0 0 12px;
  }

  /* Local card shell (shared Card is a component; a plain block keeps this page's
     four sections visually consistent without nesting extra wrappers). */
  .card {
    padding: 18px;
    border: 1px solid var(--bb-border);
    border-radius: 10px;
    background: rgba(240, 236, 228, 0.02);
  }
  .status-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    flex-wrap: wrap;
    gap: 14px;
  }

  .hint { margin: 0 0 14px; font-family: var(--bb-font-body); font-size: 12px; color: var(--bb-muted); }

  .perm-title {
    font-family: var(--bb-font-body);
    font-size: 13px;
    font-weight: 600;
    color: var(--bb-white);
    margin: 18px 0 6px;
  }
  .perm {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 10px;
    padding: 9px 0;
    border-bottom: 1px solid rgba(240, 236, 228, 0.05);
  }
  .perm:last-of-type { border-bottom: none; }
  .perm-copy { display: flex; flex-direction: column; gap: 1px; }
  .perm-label { font-family: var(--bb-font-body); font-size: 13px; color: var(--bb-white); }
  .perm-hint { font-family: var(--bb-font-body); font-size: 11.5px; color: var(--bb-muted); }

  .rates :global(.num) { max-width: 160px; }

  .actions { display: flex; align-items: center; justify-content: flex-end; gap: 12px; margin-top: 4px; }

  .cmd-block { margin-top: 26px; }

  /* Table scrolls inside its own box so the page never scrolls sideways at 320px. */
  .tbl-wrap { overflow-x: auto; -webkit-overflow-scrolling: touch; }
  .tbl { width: 100%; border-collapse: collapse; font-family: var(--bb-font-body); font-size: 13px; }
  .tbl caption { text-align: left; }
  .tbl th[scope='col'] {
    text-align: left;
    font-size: 11px;
    letter-spacing: 0.04em;
    text-transform: uppercase;
    color: var(--bb-muted);
    padding: 4px 8px;
    border-bottom: 1px solid var(--bb-border);
    font-weight: 600;
  }
  .tbl td,
  .tbl th[scope='row'] { padding: 7px 8px; border-bottom: 1px solid rgba(240, 236, 228, 0.05); color: var(--bb-white); }
  .tbl th[scope='row'] { font-weight: 600; font-family: var(--bb-font-mono); font-variant-numeric: tabular-nums; }
  .tbl .r { text-align: right; }
  .tbl .rank { color: var(--bb-muted); }
  .tbl .mut { color: var(--bb-muted); }

  @media (max-width: 480px) {
    .actions { flex-wrap: wrap; }
    .actions :global(.btn) { min-height: 44px; }
  }
</style>
