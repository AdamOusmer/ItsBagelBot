<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { invalidate } from '$app/navigation';
  import { enhance, type SubmitFunction } from '$app/forms';
  import { actionPayload } from '@bagel/kit';
  import { getI18n } from '@bagel/kit/i18n/context';
  import { giveawaySummary, MAX_PRIZE_MONTHS } from '@bagel/kit/giveaway';
  import AlertBanner from '@bagel/ui/svelte/AlertBanner.svelte';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import Card from '@bagel/ui/svelte/Card.svelte';
  import CardHead from '@bagel/ui/svelte/CardHead.svelte';
  import Field from '@bagel/ui/svelte/Field.svelte';
  import Grid from '@bagel/ui/svelte/Grid.svelte';
  import Input from '@bagel/ui/svelte/Input.svelte';
  import Stack from '@bagel/ui/svelte/Stack.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import TextLink from '@bagel/ui/svelte/TextLink.svelte';
  import { giveawayNotice, type GiveawayActionPayload } from '#lib/giveaway-workflow';
  import PoolPreview from './PoolPreview.svelte';

  let { result }: { result?: unknown } = $props();
  const { locale, t } = getI18n();
  const number = new Intl.NumberFormat(locale);

  let title = $state('');
  let reason = $state('');
  let winners = $state(1);
  let months = $state(1);
  let busy = $state<'preview' | 'create'>();
  let retainedPreview = $state.raw<GiveawayActionPayload['preview']>();
  let lastCreatedId = $state<string>();
  const payload = $derived(result as GiveawayActionPayload | null);
  const preview = $derived(payload?.preview ?? retainedPreview);
  const createdId = $derived(payload?.giveaway?.id ?? lastCreatedId);
  const notice = $derived(giveawayNotice(result));
  const available = $derived(preview?.eligible.eligible);
  const exceedsPool = $derived(available !== undefined && winners > available);
  const total = $derived(giveawaySummary(winners, months).totalMonths);

  $effect(() => {
    if (payload?.preview) retainedPreview = payload.preview;
    if (payload?.giveaway?.id) lastCreatedId = payload.giveaway.id;
  });

  const submit: SubmitFunction = ({ action, cancel }) => {
    if (busy) return cancel();
    const creating = action.searchParams.has('/create');
    busy = creating ? 'create' : 'preview';
    return async ({ result, update }) => {
      try {
        await update({ reset: false, refreshAll: false, navigate: false });
        if (actionPayload<GiveawayActionPayload>(result)?.action?.ok) await invalidate('admin:giveaways');
      } finally {
        busy = undefined;
      }
    };
  };
</script>

<section id="giveaway-draft" class="draft" tabindex="-1" aria-labelledby="giveaway-draft-label">
  <Grid min="25rem" gap={4}>
    <Card as="article">
      <Stack gap={3}>
        <div id="giveaway-draft-label"><CardHead title={t('admin.giveaways.newTitle')} headingLevel={2} /></div>
        <Text size="sm" tone="muted">{t('admin.giveaways.oneEntry')}</Text>

        <form method="POST" action="?/preview" use:enhance={submit} aria-busy={Boolean(busy)} style="--field-mb: 0">
          <Stack gap={3}>
            <Field label={t('admin.giveaways.fieldTitle')} fit>
              <Input name="title" bind:value={title} required maxlength={160} />
            </Field>
            <Field label={t('admin.giveaways.fieldReason')} fit>
              <Input name="reason" bind:value={reason} required maxlength={500} />
            </Field>
            <div class="prize-fields">
              <Field label={t('admin.giveaways.fieldWinners')} class="prize-field" fit>
                <Input type="number" name="winner_count" min={1} step={1} bind:value={winners} aria-describedby={available === undefined ? undefined : 'giveaway-winner-limit'} required />
              </Field>
              <Field label={t('admin.giveaways.fieldMonths')} class="prize-field" fit>
                <Input type="number" name="prize_months" min={1} max={MAX_PRIZE_MONTHS} step={1} bind:value={months} aria-describedby="giveaway-month-hint" required />
              </Field>
            </div>
            <div class="month-hint">
              <Text size="xs" tone="muted" id="giveaway-month-hint"><bb-fit><span data-fit>{t('admin.giveaways.monthsHint')}</span></bb-fit></Text>
            </div>
            <div class="capacity" class:shown={available !== undefined} inert={available === undefined}>
              <Text size="xs" tone={exceedsPool ? 'danger' : 'muted'} id="giveaway-winner-limit">
                <bb-fit><span data-fit>{t(exceedsPool ? 'admin.giveaways.winnersExceedEligible' : 'admin.giveaways.winnerLimit', { eligible: number.format(available ?? 0) })}</span></bb-fit>
              </Text>
            </div>
            <output class="summary">
              <Text as="span" size="sm" tone="pale">
                <bb-fit><span data-fit>
                  {#if total === null}
                    {t('admin.giveaways.summaryOverflow')}
                  {:else}
                    {t('admin.giveaways.previewSummary', {
                      total: typeof total === 'number' ? number.format(total) : total
                    })}
                  {/if}
                </span></bb-fit>
              </Text>
            </output>
            <div class="draft-actions">
              <Button type="submit" variant="secondary" formnovalidate block busy={busy === 'preview'} disabled={busy === 'create'}>
                <bb-fit><span data-fit>{t('admin.giveaways.preview')}</span></bb-fit>
              </Button>
              <Button type="submit" formaction="?/create" block busy={busy === 'create'} disabled={busy === 'preview' || exceedsPool}>
                <bb-fit><span data-fit>{t('admin.giveaways.create')}</span></bb-fit>
              </Button>
            </div>
          </Stack>
        </form>

        <div class="action-feedback" aria-live="polite">
          {#if notice}
            <AlertBanner tone={notice.failed ? 'danger' : 'success'} role="note" flush>{notice.text}</AlertBanner>
          {/if}
        </div>
        <div class="open-draft" class:shown={Boolean(createdId)} inert={!createdId}>
          <TextLink variant="arrow" href={`/giveaways/${encodeURIComponent(createdId ?? '')}`}>{t('admin.giveaways.open')}</TextLink>
        </div>
      </Stack>
    </Card>
    <PoolPreview {preview} />
  </Grid>
</section>

<style>
  .draft {
    scroll-margin-block-start: 5rem;
  }

  .prize-fields,
  .draft-actions {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: var(--bb-space-3);
  }

  .prize-fields :global(.prize-field) {
    min-block-size: 4.875rem;
    justify-content: space-between;
  }

  .draft-actions {
    max-inline-size: 30rem;
    --btn-pad: 0 10px;
  }

  .capacity,
  .open-draft {
    opacity: 0;
    transition: opacity var(--bb-dur-fast);
  }

  .shown {
    opacity: 1;
  }

  .capacity,
  .month-hint,
  .summary {
    display: block;
    min-inline-size: 0;
  }

  .action-feedback {
    block-size: 4.5rem;
    overflow: auto;
  }
</style>
