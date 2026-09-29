<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { enhance } from '$app/forms';
  import { invalidateAll } from '$app/navigation';
  import type { SubmitFunction } from '@sveltejs/kit';
  import {
    PageHead,
    MasterToggle,
    SwitchRow,
    AlertBanner,
    Card,
    ButtonLink,
    Button,
    Field,
    Heading,
    Input,
    Table,
    Text,
    EmptyState,
    SaveStatus,
    DeckList,
    toast,
    getI18n,
    LOYALTY_DEFAULTS,
    formatPointValue,
    moduleDef,
    catalogChildren,
    type LoyaltyConfig
  } from '@bagel/kit';
  import type { SaveState } from '@bagel/ui/svelte/SaveStatus.svelte';
  import ModuleCommandList from '$lib/components/modules/ModuleCommandList.svelte';
  import LoyaltyGameRow from '$lib/components/loyalty/LoyaltyGameRow.svelte';

  let { data } = $props();
  const { t, locale } = getI18n();
  const loyaltyCommands = moduleDef('loyalty')?.commands ?? [];

  // svelte-ignore state_referenced_locally
  let enabled = $state<boolean>(data.enabled ?? false);
  // svelte-ignore state_referenced_locally
  let config = $state<LoyaltyConfig>({ ...data.config });
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

  let prevOn: boolean | undefined;
  $effect(() => {
    if (prevOn && !enabled) {
      for (const g of games) g.enabled = false;
    }
    prevOn = enabled;
  });

  const payload = $derived(JSON.stringify(config));

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

  const rateFields = $derived([
    { key: 'subPoints', label: t('loyalty.fieldSub'), dflt: LOYALTY_DEFAULTS.subPoints },
    { key: 'resubPoints', label: t('loyalty.fieldResub'), dflt: LOYALTY_DEFAULTS.resubPoints },
    { key: 'giftSubPoints', label: t('loyalty.fieldGift'), dflt: LOYALTY_DEFAULTS.giftSubPoints },
    { key: 'cheerPointsPer100', label: t('loyalty.fieldCheer'), dflt: LOYALTY_DEFAULTS.cheerPointsPer100 },
    { key: 'watchPointsPerTick', label: t('loyalty.fieldWatch'), dflt: LOYALTY_DEFAULTS.watchPointsPerTick }
  ] as const);

  const permToggles = $derived([
    { key: 'modSetPoints', label: t('loyalty.permSet'), hint: t('loyalty.permSetHint') },
    { key: 'modAdjustPoints', label: t('loyalty.permAdjust'), hint: t('loyalty.permAdjustHint') },
    { key: 'viewerTransfers', label: t('loyalty.permTransfers'), hint: t('loyalty.permTransfersHint') }
  ] as const);

  function rateValue(value: string | number | null): number {
    return value === null || value === '' ? 0 : Number(value);
  }

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

  <section class="block" aria-labelledby="loy-status-h">
    <Heading level={6} as="h2" variant="title" id="loy-status-h">{t('loyalty.statusTitle')}</Heading>
    <Card>
      <div class="status-row">
        <MasterToggle
          action="?/toggle"
          bind:enabled
          label={t('loyalty.botOn')}
          hint={t('loyalty.botOnHint')}
          ariaLabel={t('loyalty.botOn')}
          failMessage={t('loyalty.toastToggleFailed')}
        />
        <ButtonLink href="/counters" variant="ghost">{t('loyalty.countersLink')}</ButtonLink>
      </div>
    </Card>
  </section>

  {#if games.length}
    <section class="block" aria-labelledby="loy-games-h">
      <Heading level={6} as="h2" variant="title" id="loy-games-h">{t('loyalty.gamesTitle')}</Heading>
      <Card>
        <div class="hint"><Text size="xs" tone="muted">{enabled ? t('loyalty.gamesHint') : t('loyalty.gamesNeedsOn')}</Text></div>
        {#each games as g (g.def.id)}
          <LoyaltyGameRow def={g.def} bind:enabled={g.enabled} loyaltyOn={enabled} />
        {/each}
      </Card>
    </section>
  {/if}

  <section class="block" aria-labelledby="loy-rates-h">
    <Heading level={6} as="h2" variant="title" id="loy-rates-h">{t('loyalty.ratesTitle')}</Heading>
    <Card>
      <div class="hint"><Text size="xs" tone="muted">{t('loyalty.ratesHint')}</Text></div>

      <form method="POST" action="?/save" use:enhance={saveSubmit} class="rates" novalidate>
        <input type="hidden" name="config" value={payload} />

        <Field label={t('loyalty.fieldName')} tag={t('common.optional')}>
          <Input placeholder={t('loyalty.fieldNamePh')} maxlength="32" bind:value={config.pointsName} />
        </Field>

        {#each rateFields as rf (rf.key)}
          <Field label={rf.label} tag={t('loyalty.defaultTag', { n: String(rf.dflt) })}>
            <!-- Keep 0 in the payload as the default sentinel; an empty field shows the effective rate. -->
            <div class="num">
              <Input
                type="number"
                min="-1"
                max="1000000"
                placeholder={String(rf.dflt)}
                bind:value={() => config[rf.key] === 0 ? null : config[rf.key], (value) => (config[rf.key] = rateValue(value))}
              />
            </div>
          </Field>
        {/each}

        <div class="perm">
          <SwitchRow
            control="end"
            label={t('loyalty.streamerPoints')}
            hint={t('loyalty.streamerPointsHint')}
            hintId="streamer-points-hint"
            checked={config.streamerPoints >= 0}
            onchange={(v) => (config.streamerPoints = v ? 0 : -1)}
          />
        </div>

        <div class="hint"><Text size="xs" tone="muted">{t('loyalty.tierHint')}</Text></div>

        <div class="perm-title"><Heading level={6} as="h3">{t('loyalty.permissionsTitle')}</Heading></div>
        <div class="hint"><Text size="xs" tone="muted">{t('loyalty.permissionsHint')}</Text></div>

        {#each permToggles as pt (pt.key)}
          <div class="perm">
            <SwitchRow
              control="end"
              label={pt.label}
              hint={pt.hint}
              hintId="perm-hint-{pt.key}"
              checked={config[pt.key] >= 0}
              onchange={(v) => (config[pt.key] = v ? 0 : -1)}
            />
          </div>
        {/each}

        <div class="actions">
          <SaveStatus state={saveState} />
          <Button variant="primary" type="submit" loading={busy}>{t('loyalty.save')}</Button>
        </div>
      </form>
    </Card>
  </section>

  <section class="block" aria-labelledby="loy-top-h">
    <Heading level={6} as="h2" variant="title" id="loy-top-h">{t('loyalty.topTitle')}</Heading>
    <Card>
      {#if top.length === 0}
        <EmptyState title={t('loyalty.topEmpty')} />
      {:else}
        <Table label={t('loyalty.topCaption')}>
          <caption class="bb-sr-only">{t('loyalty.topCaption')}</caption>
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
                <th scope="row" class="r rank"><Text as="span" size="sm" tone="muted" mono>{i + 1}</Text></th>
                <td>{row.viewerName || row.viewerLogin || row.viewerId}</td>
                <td class="r">{formatPointValue(row.points, locale)}</td>
                <td class="r mut">{hours(row.watchSeconds)}</td>
              </tr>
            {/each}
          </tbody>
        </Table>
      {/if}
    </Card>
  </section>

  {#if loyaltyCommands.length}
    <div class="cmd-block">
      <DeckList>
        <ModuleCommandList moduleId="loyalty" commands={loyaltyCommands} headingId="loy-chat-h" />
      </DeckList>
    </div>
  {/if}
</section>

<style>
  .block { display: grid; gap: 12px; margin-bottom: 26px; }

  .status-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    flex-wrap: wrap;
    gap: 14px;
  }

  .hint { margin-bottom: 14px; }

  .perm-title { margin: 18px 0 6px; }
  .perm {
    padding: 9px 0;
    border-bottom: 1px solid rgba(var(--bb-white-rgb), 0.05);
  }

  .num { max-width: 160px; }

  .actions { display: flex; align-items: center; justify-content: flex-end; gap: 12px; margin-top: 4px; }

  .cmd-block { margin-top: 26px; }

  .rank { font-variant-numeric: tabular-nums; }

  @media (max-width: 480px) {
    .actions { flex-wrap: wrap; }
    .actions { --btn-min-h: 44px; }
  }
</style>
