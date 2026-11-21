<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { enhance, type SubmitFunction } from '$app/forms';
  import { getI18n, GIVEAWAY_LIMITS as pins } from '@bagel/kit';
  import '@bagel/ui/styles/elements/collapse.css';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import Card from '@bagel/ui/svelte/Card.svelte';
  import CardHead from '@bagel/ui/svelte/CardHead.svelte';
  import StatTile from '@bagel/ui/svelte/StatTile.svelte';
  import Table from '@bagel/ui/svelte/Table.svelte';
  import Tag from '@bagel/ui/svelte/Tag.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import { giveawayCountdown, type GiveawayRound } from '#lib/channel-giveaway';
  import SlotRow from './SlotRow.svelte';

  let { round, now, busy, submit }: { round: GiveawayRound; now: number; busy: boolean; submit: SubmitFunction } = $props();
  const { t, locale } = getI18n();
  const format = new Intl.NumberFormat(locale);
  const open = $derived(round.phase === 'open');
  const canExtend = $derived(open && round.closes_ms + 60_000 <= round.opened_ms + pins.maxOpen * 1000);
  const settling = $derived(round.phase === 'settling');
  const hasNoShows = $derived(settling && round.slots.some((slot) => slot.state === 'no_show'));
  const unfinished = $derived(settling && round.slots.some((slot) => ['pending', 'verifying', 'no_show', 'won'].includes(slot.state)));
  const columns = ['slotNumber', 'winner', 'state', 'claimLeft', 'replaced', 'actions'] as const;
</script>

<Card>
  <div class="stack">
    <CardHead title={round.prize} headingLevel={2}>
      {#snippet actions()}<Tag tone={open ? 'live' : 'quiet'}><bb-fit><span data-fit="">{t(`giveaways.phase.${round.phase}`)}</span></bb-fit></Tag>{/snippet}
    </CardHead>
    {#if round.requirements.length}
      <Text size="sm" tone="muted">{round.requirements.join(' · ')}</Text>
    {/if}
    <div class="stats">
      <StatTile fitCopy label={t('giveaways.entries')} value={format.format(round.entries)} static text />
      <StatTile fitCopy label={t('giveaways.rejected')} value={format.format(round.refused)} static text />
      <StatTile fitCopy label={t('giveaways.timeLeft')} value={open ? giveawayCountdown(round.closes_ms, now) : t('giveaways.emptyCell')} static text />
    </div>
    <form method="POST" action="?/close" use:enhance={submit} class="bb-collapse commands" class:is-open={open || settling} inert={!(open || settling)} aria-hidden={!(open || settling)}>
      <div>
      <input type="hidden" name="round_id" value={round.id} />
      <div class="phases">
      <div class="phaseButtons" class:faded={!open} inert={!open}>
        <Button type="submit" disabled={!open || busy}><bb-fit><span data-fit="">{t('giveaways.close')}</span></bb-fit></Button>
        <Button type="submit" formaction="?/extend" variant="secondary" disabled={!canExtend || busy}><bb-fit><span data-fit="">{t('giveaways.extend')}</span></bb-fit></Button>
        <Button type="submit" formaction="?/cancel" variant="ghost" disabled={!open || busy}><bb-fit><span data-fit="">{t('giveaways.cancel')}</span></bb-fit></Button>
      </div>
      <div class="phaseButtons" class:faded={!settling} inert={!settling}>
        <Button type="submit" formaction="?/reroll" variant="secondary" disabled={!hasNoShows || busy}><bb-fit><span data-fit="">{t('giveaways.rerollAll')}</span></bb-fit></Button>
        <Button type="submit" formaction="?/end" variant="ghost" disabled={!unfinished || busy}><bb-fit><span data-fit="">{t('giveaways.endSlots')}</span></bb-fit></Button>
      </div>
      </div>
      </div>
    </form>
    {#if round.slots.length}
      <Table label={t('giveaways.slots')} minWidth="47rem" compact>
        <thead><tr>
          {#each columns as column (column)}
            <th scope="col"><bb-fit><span data-fit="">{t(`giveaways.${column}`)}</span></bb-fit></th>
          {/each}
        </tr></thead>
        <tbody>
          {#each round.slots as slot (slot.n)}
            <SlotRow round={round.id} {slot} {now} {busy} {submit} />
          {/each}
        </tbody>
      </Table>
    {/if}
  </div>
</Card>

<style>
  .stack { display: flex; flex-direction: column; gap: var(--bb-space-3); min-width: 0; }
  .stats { display: grid; gap: var(--bb-space-2); grid-template-columns: repeat(3, minmax(0, 1fr)); }
  .commands { --bb-collapse-gap: var(--bb-space-3); }
  .phases { display: grid; }
  .phaseButtons { --btn-w: 10rem; grid-area: 1 / 1; display: flex; flex-wrap: wrap; gap: var(--bb-space-2); align-content: start; }
  .phaseButtons { transition: opacity var(--bb-dur-fast); }
  .faded { opacity: 0; pointer-events: none; }
</style>
