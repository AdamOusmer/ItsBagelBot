<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { enhance, type SubmitFunction } from '$app/forms';
  import { getI18n } from '@bagel/kit';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import Tag from '@bagel/ui/svelte/Tag.svelte';
  import { giveawayCountdown, type GiveawaySlot, type GiveawaySlotState } from '#lib/channel-giveaway';

  let { round, slot, now, busy, submit }: {
    round: string;
    slot: GiveawaySlot;
    now: number;
    busy: boolean;
    submit: SubmitFunction;
  } = $props();

  const { t } = getI18n();
  const canReroll = $derived(slot.state === 'no_show' || !!slot.can_skip_rule);
  const canSkip = $derived(!!slot.can_skip_rule);
  const deadline = $derived(slot.state === 'won' ? slot.deadline_ms : undefined);
  const tones = {
    pending: 'quiet', verifying: 'warning', won: 'info', claimed: 'success',
    no_show: 'danger', unfilled: 'quiet', ended: 'quiet'
  } as const satisfies Record<GiveawaySlotState, string>;

  function reason(code: string): string {
    const key = `giveaways.reason.${code}`;
    const label = t(key);
    return label === key ? t('giveaways.reason.other') : label;
  }
</script>

<tr>
  <td>{slot.n}</td>
  <td><bb-fit><span data-fit="">{slot.login || slot.viewer_id || t('giveaways.emptyCell')}</span></bb-fit></td>
  <td><Tag tone={tones[slot.state]}><bb-fit><span data-fit="">{t(`giveaways.slot.${slot.state}`)}</span></bb-fit></Tag></td>
  <td class="countdown">{deadline ? giveawayCountdown(deadline, now) : t('giveaways.emptyCell')}</td>
  <td>
    <ul class="rejected">
      {#each slot.rejected ?? [] as candidate (`${candidate.viewer_id}:${candidate.generation}`)}
        <li>{t('giveaways.rejection', { viewer: candidate.login || candidate.viewer_id, reason: reason(candidate.reason) })}</li>
      {:else}
        <li>{t('giveaways.emptyCell')}</li>
      {/each}
    </ul>
  </td>
  <td>
    <form method="POST" action="?/reroll" use:enhance={submit} class="actions">
      <input type="hidden" name="round_id" value={round} />
      <input type="hidden" name="slot" value={slot.n} />
      <Button type="submit" variant="secondary" size="sm" class={canReroll ? 'slotControl' : 'slotControl faded'} disabled={!canReroll || busy}>
        <bb-fit><span data-fit="">{t('giveaways.reroll')}</span></bb-fit>
      </Button>
      <Button type="submit" formaction="?/skip" variant="secondary" size="sm" class={canSkip ? 'slotControl' : 'slotControl faded'} disabled={!canSkip || busy}>
        <bb-fit><span data-fit="">{t('giveaways.skip')}</span></bb-fit>
      </Button>
    </form>
  </td>
</tr>

<style>
  .actions { --btn-w: 7rem; display: flex; gap: var(--bb-space-2); }
  .actions :global(.slotControl) { transition: opacity var(--bb-dur-fast); }
  .actions :global(.faded) { opacity: 0; }
  .rejected { list-style: none; margin: 0; padding: 0; overflow-wrap: anywhere; }
  .countdown { font-variant-numeric: tabular-nums; }
</style>
