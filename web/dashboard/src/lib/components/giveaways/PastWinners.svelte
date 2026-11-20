<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { Snippet } from 'svelte';
  import { enhance, type SubmitFunction } from '$app/forms';
  import { getI18n } from '@bagel/kit';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import Card from '@bagel/ui/svelte/Card.svelte';
  import CardHead from '@bagel/ui/svelte/CardHead.svelte';
  import Table from '@bagel/ui/svelte/Table.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import PageNotice from '#lib/components/shared/PageNotice.svelte';
  import type { GiveawayWinner } from '#lib/channel-giveaway';

  type Row = Pick<GiveawayWinner, 'round_id' | 'slot' | 'login' | 'prize' | 'won_at'> & Partial<Pick<GiveawayWinner, 'viewer_id' | 'delivered_at'>>;
  let { winners, degraded = false, busy = false, submit, pager }: {
    winners: Row[];
    degraded?: boolean;
    busy?: boolean;
    submit?: SubmitFunction;
    pager?: Snippet;
  } = $props();

  const { t, locale } = getI18n();
  const date = new Intl.DateTimeFormat(locale, { dateStyle: 'medium', timeStyle: 'short' });
  const cols = ['prize', 'winner', 'wonAt'] as const;
</script>

<Card>
  <div class="history">
    <CardHead title={t('giveaways.history')} headingLevel={2} />
    {#if degraded || !winners.length}
      <div class="historyState">
        <div class="stateMessage">
          <PageNotice tone="warning" visible={degraded}>{t('giveaways.historyUnavailable')}</PageNotice>
        </div>
        <div class="stateMessage empty" class:faded={degraded} inert={degraded} aria-hidden={degraded}>
          <Text as="span" size="sm" tone="muted"><bb-fit mode="block">{t('giveaways.noWinners')}</bb-fit></Text>
        </div>
      </div>
    {:else}
      <Table label={t('giveaways.history')} minWidth={submit ? '34rem' : undefined} compact>
        <thead><tr>
          {#each cols as col (col)}<th scope="col"><bb-fit><span data-fit="">{t(`giveaways.${col}`)}</span></bb-fit></th>{/each}
          {#if submit}<th scope="col"><bb-fit><span data-fit="">{t('giveaways.delivery')}</span></bb-fit></th>{/if}
        </tr></thead>
        <tbody>
          {#each winners as item (`${item.round_id}:${item.slot}`)}
            <tr>
              <td class="name">{item.prize}</td>
              <td class="name">{item.login || (submit && item.viewer_id) || t('giveaways.emptyCell')}</td>
              <td><time datetime={new Date(item.won_at).toISOString()}>{date.format(item.won_at)}</time></td>
              {#if submit}
                <td>
                  <form class="delivery" method="POST" action="?/deliver" use:enhance={submit}>
                    <input type="hidden" name="round_id" value={item.round_id} />
                    <input type="hidden" name="slot" value={item.slot} />
                    <input type="hidden" name="delivered" value={String(!item.delivered_at)} />
                    <Button variant={item.delivered_at ? 'ghost' : 'secondary'} size="sm" type="submit" disabled={busy}>
                      <bb-fit><span data-fit="">{t(item.delivered_at ? 'giveaways.undoDelivery' : 'giveaways.deliver')}</span></bb-fit>
                    </Button>
                  </form>
                </td>
              {/if}
            </tr>
          {/each}
        </tbody>
      </Table>
    {/if}
    {@render pager?.()}
  </div>
</Card>

<style>
  .history { display: grid; gap: var(--bb-space-3); min-width: 0; }
  .historyState { display: grid; }
  .stateMessage { grid-area: 1 / 1; min-inline-size: 0; }
  .empty { display: grid; align-content: center; transition: opacity var(--bb-dur-fast); }
  .faded { opacity: 0; }
  .delivery { --btn-w: 10rem; }
  .name { overflow-wrap: anywhere; }
  @media (prefers-reduced-motion: reduce) {
    .empty { transition: none; }
  }
</style>
