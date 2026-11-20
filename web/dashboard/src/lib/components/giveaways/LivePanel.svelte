<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { enhance, type SubmitFunction } from '$app/forms';
  import { getI18n, GIVEAWAY_LIMITS as pins } from '@bagel/kit';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import Card from '@bagel/ui/svelte/Card.svelte';
  import CardHead from '@bagel/ui/svelte/CardHead.svelte';
  import StatTile from '@bagel/ui/svelte/StatTile.svelte';
  import Table from '@bagel/ui/svelte/Table.svelte';
  import Tag from '@bagel/ui/svelte/Tag.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import { giveawayCountdown, type GiveawayRound, type RaffleRound } from '#lib/channel-giveaway';
  import SlotRow from './SlotRow.svelte';

  let { round, now, busy, submit }: { round: GiveawayRound | RaffleRound; now: number; busy: boolean; submit: SubmitFunction } = $props();
  const { t, locale } = getI18n();
  const format = new Intl.NumberFormat(locale);
  const open = $derived(round.phase === 'open');
  const giveaway = $derived(round.kind === 'giveaway' ? round : null);
  const canExtend = $derived(giveaway !== null && open && round.closes_ms + 60_000 <= giveaway.opened_ms + pins.maxOpen * 1000);
  const settling = $derived(round.phase === 'settling');
  const hasNoShows = $derived(settling && giveaway?.slots.some((slot) => slot.state === 'no_show'));
  const unfinished = $derived(settling && giveaway?.slots.some((slot) => ['pending', 'verifying', 'no_show', 'won'].includes(slot.state)));
  const columns = ['slotNumber', 'winner', 'state', 'claimLeft', 'replaced', 'actions'] as const;
</script>

<Card>
  <div class="stack">
    <CardHead title={giveaway?.prize ?? t('giveaways.pointsRaffle')} headingLevel={2}>
      {#snippet actions()}<Tag tone={open ? 'live' : 'quiet'}><bb-fit><span data-fit="">{t(`giveaways.phase.${round.phase}`)}</span></bb-fit></Tag>{/snippet}
    </CardHead>
    {#if giveaway?.requirements.length}
      <Text size="sm" tone="muted">{giveaway.requirements.join(' · ')}</Text>
    {/if}
    <div class="stats" class:raffleStats={!giveaway}>
      <StatTile fitCopy label={t('giveaways.entries')} value={format.format(round.entries)} static text />
      {#if giveaway}
        <StatTile fitCopy label={t('giveaways.rejected')} value={format.format(giveaway.refused)} static text />
      {/if}
      <StatTile fitCopy label={t('giveaways.timeLeft')} value={open ? giveawayCountdown(round.closes_ms, now) : t('giveaways.emptyCell')} static text />
    </div>
    <form method="POST" action={giveaway ? '?/close' : '?/raffleDraw'} use:enhance={submit} class="commands">
      <input type="hidden" name="round_id" value={round.id} />
      <div class="phaseButtons" class:faded={!open} inert={!open}>
        <Button type="submit" disabled={!open || busy}><bb-fit><span data-fit="">{t(giveaway ? 'giveaways.close' : 'giveaways.drawNow')}</span></bb-fit></Button>
        {#if giveaway}
          <Button type="submit" formaction="?/extend" variant="secondary" disabled={!canExtend || busy}><bb-fit><span data-fit="">{t('giveaways.extend')}</span></bb-fit></Button>
        {/if}
        <Button type="submit" formaction={giveaway ? '?/cancel' : '?/raffleCancel'} variant="ghost" disabled={!open || busy}><bb-fit><span data-fit="">{t(giveaway ? 'giveaways.cancel' : 'giveaways.cancelRaffle')}</span></bb-fit></Button>
      </div>
      {#if giveaway}
        <div class="phaseButtons" class:faded={!settling} inert={!settling}>
          <Button type="submit" formaction="?/reroll" variant="secondary" disabled={!hasNoShows || busy}><bb-fit><span data-fit="">{t('giveaways.rerollAll')}</span></bb-fit></Button>
          <Button type="submit" formaction="?/end" variant="ghost" disabled={!unfinished || busy}><bb-fit><span data-fit="">{t('giveaways.endSlots')}</span></bb-fit></Button>
        </div>
      {/if}
    </form>
    {#if giveaway?.slots.length}
      <Table label={t('giveaways.slots')} minWidth="47rem" compact>
        <thead><tr>
          {#each columns as column (column)}
            <th scope="col"><bb-fit><span data-fit="">{t(`giveaways.${column}`)}</span></bb-fit></th>
          {/each}
        </tr></thead>
        <tbody>
          {#each giveaway.slots as slot (slot.n)}
            <SlotRow round={round.id} {slot} {now} {busy} {submit} />
          {/each}
        </tbody>
      </Table>
    {/if}
  </div>
</Card>

<style>
  .stack { display: grid; gap: var(--bb-space-3); min-width: 0; }
  .stats { display: grid; gap: var(--bb-space-2); grid-template-columns: repeat(3, minmax(0, 1fr)); }
  .raffleStats { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .commands { display: grid; }
  .phaseButtons { --btn-w: 10rem; grid-area: 1 / 1; display: flex; flex-wrap: wrap; gap: var(--bb-space-2); align-content: start; }
  .phaseButtons { transition: opacity var(--bb-dur-fast); }
  .faded { opacity: 0; pointer-events: none; }
</style>
