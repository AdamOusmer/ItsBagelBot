<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { enhance, type SubmitFunction } from '$app/forms';
  import { getI18n } from '@bagel/kit/i18n/context';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import Card from '@bagel/ui/svelte/Card.svelte';
  import CardHead from '@bagel/ui/svelte/CardHead.svelte';
  import Cluster from '@bagel/ui/svelte/Cluster.svelte';
  import EmptyState from '@bagel/ui/svelte/EmptyState.svelte';
  import Fact from '@bagel/ui/svelte/Fact.svelte';
  import FactList from '@bagel/ui/svelte/FactList.svelte';
  import Grid from '@bagel/ui/svelte/Grid.svelte';
  import Stack from '@bagel/ui/svelte/Stack.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import TextLink from '@bagel/ui/svelte/TextLink.svelte';
  import Badge from '@bagel/ui/svelte/Badge.svelte';
  import { AWARD_STATUS, BILLING_STATUS, EMAIL_STATUS, canRetryAward, giveawayDate } from '#lib/giveaway-workflow';
  import type { GiveawayWinnerWire } from '#lib/server/giveaways';

  let { winners, busy, submit }: { winners: GiveawayWinnerWire[]; busy: boolean; submit: SubmitFunction } = $props();
  const { locale, t } = getI18n();
  const formatter = new Intl.DateTimeFormat(locale, {
    year: 'numeric', month: 'short', day: 'numeric', hour: 'numeric', minute: '2-digit', timeZone: 'UTC', timeZoneName: 'short'
  });
</script>

<Stack as="section" gap={3}>
  <CardHead title={t('admin.giveaways.winnerHistory')} headingLevel={2} />

  {#if winners.length === 0}
    <Card><EmptyState title={t('admin.giveaways.noWinners')} /></Card>
  {:else}
    {#each winners as winner (winner.id)}
      {@const award = AWARD_STATUS[winner.awardState]}
      {@const billing = BILLING_STATUS[winner.billingState]}
      {@const email = EMAIL_STATUS[winner.emailState]}
      {@const canRetry = canRetryAward(winner)}
      <Card as="article" aria-label={winner.login}>
        <Stack gap={3}>
          <Cluster justify="between" gap={2}>
            <Stack gap={1} align="start">
              <TextLink variant="inline" href={`/users?q=${encodeURIComponent(winner.userId)}`}>@{winner.login}</TextLink>
              <Text size="xs" tone="muted" mono>{winner.userId}</Text>
            </Stack>
            <form
              class="retry"
              class:available={canRetry}
              inert={!canRetry}
              method="POST"
              action="?/retry"
              use:enhance={submit}
            >
              <input type="hidden" name="award_id" value={winner.id} />
              <Button type="submit" variant="secondary" size="sm" block disabled={!canRetry || busy} aria-label={`${t('admin.giveaways.retry')} · @${winner.login}`}>
                <bb-fit><span data-fit>{t('admin.giveaways.retry')}</span></bb-fit>
              </Button>
            </form>
          </Cluster>
          <Grid min="13rem" gap={3}>
            <Stack gap={2}>
              <CardHead title={t('admin.giveaways.award')} headingLevel={3} />
              <div class="award-status"><Badge shape="status" tone={award.tone}><bb-fit><span data-fit>{t(award.label)}</span></bb-fit></Badge></div>
              <FactList>
                <Fact term={t('admin.giveaways.selectedAt')} fit>{giveawayDate(winner.selectedAt, formatter, t)}</Fact>
                <Fact term={t('admin.giveaways.start')} fit>{giveawayDate(winner.startAt, formatter, t)}</Fact>
                <Fact term={t('admin.giveaways.end')} fit>{giveawayDate(winner.endAt, formatter, t)}</Fact>
              </FactList>
              {#if winner.failureReason}
                <Text size="xs" tone="muted">{winner.failureReason}</Text>
              {/if}
            </Stack>
            <Stack gap={2}>
              <CardHead title={t('admin.giveaways.billing')} headingLevel={3} />
              <div class="award-status"><Badge shape="status" tone={billing.tone}><bb-fit><span data-fit>{t(billing.label)}</span></bb-fit></Badge></div>
              <FactList>
                <Fact term={t('admin.giveaways.nextCharge')} fit>{giveawayDate(winner.nextChargeAt, formatter, t)}</Fact>
                <Fact term={t('admin.giveaways.providerVerified')} fit>{giveawayDate(winner.providerVerifiedAt, formatter, t)}</Fact>
              </FactList>
            </Stack>
            <Stack gap={2}>
              <CardHead title={t('admin.giveaways.email')} headingLevel={3} />
              <div class="award-status"><Badge shape="status" tone={email.tone}><bb-fit><span data-fit>{t(email.label)}</span></bb-fit></Badge></div>
              {#if winner.emailState === 'missing_contact'}
                <Text size="xs" tone="muted">{t('admin.giveaways.missingEmail')}</Text>
              {:else if winner.emailWarning}
                <Text size="xs" tone="muted">{winner.emailWarning}</Text>
              {/if}
            </Stack>
          </Grid>
        </Stack>
      </Card>
    {/each}
  {/if}
</Stack>

<style>
  .retry {
    inline-size: min(100%, 10rem);
    opacity: 0;
    transition: opacity var(--bb-dur-fast);
  }

  .award-status {
    display: grid;
    inline-size: 8.5rem;
  }

  .available {
    opacity: 1;
  }
</style>
