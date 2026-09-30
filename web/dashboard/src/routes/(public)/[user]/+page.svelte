<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import AuroraBg from '@bagel/ui/svelte/AuroraBg.svelte';
  import Chip from '@bagel/ui/svelte/Chip.svelte';
  import Code from '@bagel/ui/svelte/Code.svelte';
  import EmptyState from '@bagel/ui/svelte/EmptyState.svelte';
  import Eyebrow from '@bagel/ui/svelte/Eyebrow.svelte';
  import Heading from '@bagel/ui/svelte/Heading.svelte';
  import Lead from '@bagel/ui/svelte/Lead.svelte';
  import LightField from '@bagel/ui/svelte/LightField.svelte';
  import AlertBanner from '@bagel/ui/svelte/AlertBanner.svelte';
  import Card from '@bagel/ui/svelte/Card.svelte';
  import Table from '@bagel/ui/svelte/Table.svelte';
  import Tag from '@bagel/ui/svelte/Tag.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import TextLink from '@bagel/ui/svelte/TextLink.svelte';
  import { formatPointValue, getI18n } from '@bagel/kit';
  import PublicHead from '$lib/components/public/PublicHead.svelte';
  import type { PageData } from './$types';
  import { commandsHref } from '@bagel/kit/site-links';

  let { data }: { data: PageData } = $props();

  const { t, locale } = getI18n();

  const rowName = (v: { viewerName: string; viewerLogin: string; viewerId: string }) =>
    v.viewerName || v.viewerLogin || t('leaderboard.anonymousViewer');

  const totalFmt = new Intl.NumberFormat(locale, { maximumFractionDigits: 0 });
  const hoursFmt = (seconds: number): string => {
    const hours = seconds / 3600;
    return hours < 10
      ? new Intl.NumberFormat(locale, { maximumFractionDigits: 1 }).format(hours)
      : totalFmt.format(Math.round(hours));
  };

  const podium = $derived(data.top.slice(0, 3));
  const rest = $derived(data.top.slice(3));

  const commandTriggers = $derived(
    (data.modules ?? [])
      .flatMap((m) => m.commands.map((c) => c.label))
      .sort((a, b) => a.localeCompare(b))
  );

  const channelHref = $derived(commandsHref(data.login));
</script>

<PublicHead
  title={t('leaderboard.title', { channel: data.channelName })}
  description={t('leaderboard.metaDescription', { channel: data.channelName })}
  url="https://leaderboard.itsbagelbot.com/{data.login}"
/>

<AuroraBg />
<div class="starfield" aria-hidden="true"><LightField /></div>

<main class="lb-page">
  <header class="hero">
    <Eyebrow tone="go" class="reveal">{t('leaderboard.eyebrow')}</Eyebrow>
    <Heading level={1} class="headline">
      <span class="word pre reveal">{t('leaderboard.headlinePrefix')}</span>
      <span class="word name reveal">{data.channelName}&nbsp;</span>
    </Heading>
    <div class="lede reveal">
      <Lead>{t('leaderboard.tagline', { channel: data.channelName })}</Lead>
    </div>
    <div class="channel-link reveal">
      <TextLink variant="arrow" tone="go" href={channelHref} label={t('leaderboard.visitChannel')} />
    </div>
  </header>

  {#if data.degraded}
    <div class="notice reveal">
      <AlertBanner tone="warning">{t('leaderboard.degraded')}</AlertBanner>
    </div>
  {:else if data.top.length === 0}
    <div class="empty reveal">
      <Card atmo>
        <EmptyState title={t('leaderboard.emptyTitle')} body={t('leaderboard.emptyBody', { channel: data.channelName })} />
      </Card>
    </div>
  {:else}
    <section class="podium" aria-label={t('leaderboard.podiumLabel')}>
      {#each podium as viewer, i (viewer.viewerId)}
        <div class="spot-wrap reveal place-{i + 1}">
          <Card atmo hover>
            <div class="spot">
              <span class="medal medal-{i + 1}" aria-hidden="true">{i + 1}</span>
              <span class="avatar" aria-hidden="true">{rowName(viewer).slice(0, 2)}</span>
              <span class="name" title={viewer.viewerLogin || viewer.viewerName}>{rowName(viewer)}</span>
              <span class="points">
                <span class="num">{formatPointValue(viewer.points, locale)}</span>
                <span class="currency">{data.currencyName}</span>
              </span>
              <span class="watched">
                {hoursFmt(viewer.watchSeconds)}&nbsp;{t('leaderboard.watchUnit')}
              </span>
            </div>
          </Card>
        </div>
      {/each}
    </section>

    <section class="board-wrap reveal" aria-label={t('leaderboard.boardLabel')}>
      <Card atmo label={t('leaderboard.boardCh')}>
        {#snippet band()}
          <header class="board-head">
            <div class="board-titles">
              <Heading level={4} as="h2">{t('leaderboard.boardTitle')}</Heading>
              <Text size="sm" tone="muted">{t('leaderboard.boardNote')}</Text>
            </div>
          </header>
        {/snippet}
        {#if rest.length === 0}
          <Text size="sm" tone="muted">{t('leaderboard.soloNote')}</Text>
        {:else}
          <Table label={t('leaderboard.boardTitle')}>
            <thead>
              <tr>
                <th scope="col">{t('leaderboard.colRank')}</th>
                <th scope="col">{t('leaderboard.colViewer')}</th>
                <th class="r" scope="col">{t('leaderboard.colWatched')}</th>
                <th class="r" scope="col">{t('leaderboard.colPoints')}</th>
              </tr>
            </thead>
            <tbody>
              {#each rest as viewer, i (viewer.viewerId)}
                <tr>
                  <td class="rank"><Text as="span" size="xs" tone="muted" mono>{i + 4}</Text></td>
                  <td>
                    <span class="viewer">
                      <span class="avatar-sm" aria-hidden="true">{rowName(viewer).slice(0, 1)}</span>
                      <Text as="span" size="sm" truncate>{rowName(viewer)}</Text>
                    </span>
                  </td>
                  <td class="r">
                    <Text as="span" size="xs" tone="muted" mono>{hoursFmt(viewer.watchSeconds)}&nbsp;{t('leaderboard.watchUnit')}</Text>
                  </td>
                  <td class="r"><strong>{formatPointValue(viewer.points, locale)}</strong></td>
                </tr>
              {/each}
            </tbody>
          </Table>
          <Text size="xs" tone="muted" mono>{t('leaderboard.rankedNote', { count: totalFmt.format(data.top.length) })}</Text>
        {/if}
      </Card>
    </section>
  {/if}

  {#if data.commands.length > 0 || commandTriggers.length > 0}
    <section class="cmds-wrap reveal" aria-label={t('leaderboard.commandsLabel')}>
      <Card atmo label={t('leaderboard.commandsCh')}>
        {#snippet band()}
          <header class="cmds-head">
            <Heading level={6} as="h2">{t('leaderboard.commandsTitle')}</Heading>
          </header>
        {/snippet}

        {#if data.commands.length > 0}
          <Table label={t('leaderboard.commandsTitle')}>
            <thead>
              <tr>
                <th scope="col">{t('leaderboard.colCommand')}</th>
                <th scope="col">{t('leaderboard.colResponse')}</th>
                <th scope="col" class="r">{t('leaderboard.colPerm')}</th>
              </tr>
            </thead>
            <tbody>
              {#each data.commands as cmd (cmd.trigger)}
                <tr>
                  <td>
                    <Code tone="success">{cmd.trigger}</Code>
                    {#if cmd.aliases.length > 0}
                      <Text as="div" size="xs" tone="muted" mono>{cmd.aliases.join(' ')}</Text>
                    {/if}
                  </td>
                  <td class="response">{cmd.response}</td>
                  <td class="r"><Text as="span" size="xs" tone="muted" mono>{cmd.perm}</Text></td>
                </tr>
              {/each}
            </tbody>
          </Table>
        {/if}

        {#if commandTriggers.length > 0}
          <div class="chip-row">
            {#each commandTriggers as trig (trig)}
              <Chip as="span" tone="muted">{trig}</Chip>
            {/each}
          </div>
        {/if}
      </Card>
    </section>
  {/if}

  <footer class="foot reveal">
    <Tag tone="live" mark="solid" sweep class="bb-tag--wrap">{t('leaderboard.earnNote')}</Tag>
  </footer>
</main>

<style>
  .starfield {
    position: fixed;
    inset: 0;
    z-index: 0;
    pointer-events: none;
  }

  .lb-page {
    --tbl-head-size: var(--bb-text-xs);

    position: relative;
    z-index: 1;
    min-height: calc(100vh - var(--bb-nav-height));
    max-width: var(--bb-content-max);
    margin: 0 auto;
    padding: calc(var(--bb-nav-height) + env(safe-area-inset-top, 0px) + clamp(40px, 8vh, 88px)) var(--bb-space-5)
      var(--bb-space-8);
    display: flex;
    flex-direction: column;
    gap: var(--bb-space-6);
  }

  .hero {
    text-align: center;
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: var(--bb-space-3);
    overflow-wrap: anywhere;
  }
  .hero :global(.headline) { max-width: 24ch; }

  .word { display: inline-block; }
  .word.pre {
    --i: 0.5;

    font-size: 0.5em;
    font-weight: 600;
    color: var(--bb-muted);
    vertical-align: middle;
    margin-right: 0.35em;
    text-transform: lowercase;
  }
  .word.name { --i: 1; }

  .lede { --i: 2; }
  .channel-link { --i: 2.5; }

  .notice { --i: 3; max-width: 640px; width: 100%; margin: 0 auto; }
  .empty { --i: 3; max-width: 640px; width: 100%; margin: 0 auto; }

  .podium {
    --card-pad: clamp(20px, 2.4vw, 30px);

    display: grid;
    grid-template-columns: repeat(3, minmax(0, 1fr));
    gap: var(--bb-space-4);
    align-items: stretch;
    max-width: 980px;
    width: 100%;
    margin: 0 auto;
  }

  .spot-wrap { display: grid; min-width: 0; }
  .place-1 { --i: 3; order: 2; }
  .place-2 { --i: 3.5; order: 1; }
  .place-3 { --i: 4; order: 3; }
  @media (max-width: 720px) {
    .place-1, .place-2, .place-3 { order: 0; }
  }

  .spot {
    display: flex;
    flex-direction: column;
    align-items: center;
    text-align: center;
    gap: var(--bb-space-3);
    min-width: 0;
  }
  .place-1 .spot {
    padding-top: var(--bb-space-4);
  }
  .spot::before {
    content: '';
    position: absolute;
    inset: 0 0 auto;
    height: 1px;
    background: linear-gradient(90deg, transparent, rgba(var(--bb-white-pure-rgb), 0.14), transparent);
  }
  :global(:root[data-theme='light']) .spot::before {
    background: linear-gradient(90deg, transparent, rgba(var(--bb-ink-rgb), 0.12), transparent);
  }

  .medal {
    font-family: var(--bb-font-display);
    font-weight: 800;
    font-size: var(--bb-text-sm);
    line-height: 1;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 26px;
    height: 26px;
    border-radius: 50%;
    position: absolute;
    top: 10px;
    left: 50%;
    transform: translateX(-50%);
    border: 1px solid var(--bb-border);
    background: rgba(var(--bb-white-pure-rgb), 0.06);
    color: var(--bb-muted);
  }
  .medal-1 {
    background: rgba(var(--bb-tan-rgb), 0.16);
    border-color: var(--bb-tan);
    color: var(--bb-tan-light);
    box-shadow: 0 0 18px rgba(var(--bb-tan-rgb), 0.25);
  }
  .medal-2 { background: rgba(var(--bb-white-pure-rgb), 0.1); color: var(--bb-white); }
  .medal-3 { background: rgba(var(--bb-tan-rgb), 0.07); color: rgba(var(--bb-tan-rgb), 0.8); }

  .avatar {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 52px;
    height: 52px;
    border-radius: 50%;
    font-family: var(--bb-font-display);
    font-weight: 800;
    font-size: 19px;
    letter-spacing: var(--bb-tracking-tight);
    text-transform: uppercase;
    color: var(--bb-green-glow);
    background: rgba(var(--bb-green-glow-rgb), 0.1);
    border: 1px solid var(--bb-border);
  }
  .place-1 .avatar {
    width: 62px;
    height: 62px;
    font-size: 23px;
    color: var(--bb-tan-light);
    background: rgba(var(--bb-tan-rgb), 0.12);
  }

  .name {
    font-family: var(--bb-font-body);
    font-weight: 600;
    font-size: 15px;
    line-height: 1.35;
    color: var(--bb-white);
    max-width: 100%;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .points { display: flex; align-items: baseline; gap: 7px; min-width: 0; justify-content: center; }
  .points .num {
    font-family: var(--bb-font-display);
    font-weight: 800;
    font-size: clamp(26px, 3.4vw, 40px);
    line-height: 1;
    letter-spacing: var(--bb-tracking-tight);
    color: var(--bb-white);
    font-variant-numeric: tabular-nums;
  }
  .place-1 .points .num { font-size: clamp(32px, 4vw, 50px); color: var(--bb-tan-light); }
  .points .currency {
    font-family: var(--bb-font-mono);
    font-size: var(--bb-text-xs);
    letter-spacing: var(--bb-tracking-eyebrow);
    text-transform: uppercase;
    color: var(--bb-muted);
  }

  .watched {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    font-family: var(--bb-font-mono);
    font-size: var(--bb-text-xs);
    letter-spacing: 0.08em;
    text-transform: uppercase;
    color: var(--bb-muted);
    font-variant-numeric: tabular-nums;
  }

  .board-wrap {
    --i: 5;
    --card-pad: clamp(20px, 2.4vw, 30px);
    --card-band-h: calc(112px * var(--d));
    --card-band-pad: calc(16px * var(--d)) var(--card-pad);

    min-width: 0;
    max-width: 980px;
    width: 100%;
    margin: 0 auto;
  }

  .board-head { display: flex; align-items: flex-start; gap: var(--bb-space-3); min-width: 0; }
  .board-titles { display: flex; flex-direction: column; gap: var(--bb-space-1); min-width: 0; }

  .rank { width: 2.5ch; }

  .viewer { display: flex; align-items: center; gap: var(--bb-space-3); min-width: 0; }

  .avatar-sm {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 26px;
    height: 26px;
    flex: 0 0 auto;
    border-radius: 50%;
    font-family: var(--bb-font-display);
    font-weight: 700;
    font-size: var(--bb-text-xs);
    text-transform: uppercase;
    color: var(--bb-green-glow);
    background: rgba(var(--bb-green-glow-rgb), 0.1);
    border: 1px solid var(--bb-border);
  }

  .cmds-wrap { --i: 6; }
  .cmds-head {
    display: flex;
    align-items: center;
    gap: var(--bb-space-2);
  }
  .response { overflow-wrap: anywhere; }
  .chip-row { display: flex; flex-wrap: wrap; gap: 6px; margin-top: var(--bb-space-3); }

  .foot {
    --i: 7;

    display: flex;
    align-items: center;
    justify-content: center;
    gap: var(--bb-space-2);
  }

  @media (max-width: 900px) {
    .podium { grid-template-columns: minmax(0, 1fr); max-width: 480px; }
  }
</style>
