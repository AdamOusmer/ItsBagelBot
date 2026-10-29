<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import Badge from '@bagel/ui/svelte/Badge.svelte';
  import PageHead from '@bagel/ui/svelte/PageHead.svelte';
  import Card from '@bagel/ui/svelte/Card.svelte';
  import HaloPanel from '@bagel/ui/svelte/HaloPanel.svelte';
  import MarkBadge from '@bagel/ui/svelte/MarkBadge.svelte';
  import Table from '@bagel/ui/svelte/Table.svelte';
  import Modal from '@bagel/ui/svelte/Modal.svelte';
  import AlertBanner from '@bagel/ui/svelte/AlertBanner.svelte';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import ConfirmDialog from '@bagel/ui/svelte/ConfirmDialog.svelte';
  import Eyebrow from '@bagel/ui/svelte/Eyebrow.svelte';
  import Field from '@bagel/ui/svelte/Field.svelte';
  import FieldError from '@bagel/ui/svelte/FieldError.svelte';
  import AuroraBg from '@bagel/ui/svelte/AuroraBg.svelte';
  import Input from '@bagel/ui/svelte/Input.svelte';
  import Label from '@bagel/ui/svelte/Label.svelte';
  import LightField from '@bagel/ui/svelte/LightField.svelte';
  import Tag from '@bagel/ui/svelte/Tag.svelte';
  import Heading from '@bagel/ui/svelte/Heading.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import Textarea from '@bagel/ui/svelte/Textarea.svelte';
  import { toast } from '@bagel/ui/svelte/toast';
  import { getI18n, containsLink } from '@bagel/kit';
  import CelebrationModal from '#lib/components/billing/CelebrationModal.svelte';
  import { reseedOnChange } from '#lib/client-actions.svelte';
  import { fmtDateTime } from '@bagel/kit/format';
  import { page } from '$app/state';
  import { invalidateAll, replaceState } from '$app/navigation';
  import { onMount } from 'svelte';
  import type { BillingState } from '#lib/server/services';
  import type { PrizeAward } from '#lib/server/giveaways';

  let { data, form } = $props();

  const i18n = getI18n();
  const { t } = i18n;

  const account = $derived(data.account as BillingState);
  const prizes = $derived((data.prizes ?? []) as PrizeAward[]);

  const ACTIVATION_POLL_MS = 3000;
  const ACTIVATION_WINDOW_MS = 30000;

  let awaitingActivation = $state(false);

  const isVip = $derived(account.status === 'vip');
  const isPaid = $derived(account.status === 'paid' || isVip);
  const staffGrant = $derived(account.status === 'paid' && account.source === 'admin');
  const tebexPaid = $derived(account.status === 'paid' && account.source === 'tebex');
  const cancelPending = $derived(tebexPaid && account.cancelPending);
  const money = $derived(
    new Intl.NumberFormat(i18n.locale, { style: 'currency', currency: 'CAD', maximumFractionDigits: 0 }).format(7)
  );
  const paidUntil = $derived(account.expiresAt);
  const canSubscribe = $derived(!isPaid);
  const canManage = $derived(tebexPaid);
  const paymentFailed = $derived(tebexPaid && account.paymentFailed === true);
  const statusLabel = $derived(isVip ? 'VIP' : isPaid ? t('billing.premium') : t('billing.free'));

  const freeFeatures = $derived([
    t('billing.freeFeat1'),
    t('billing.freeFeat2'),
    t('billing.freeFeat3')
  ]);
  const premiumFeatures = $derived([
    t('billing.premiumFeat1'),
    t('billing.premiumFeat2'),
    t('billing.premiumFeat3'),
    t('billing.premiumFeat4'),
    t('billing.premiumFeat5')
  ]);

  let launching = $state<'monthly' | 'once' | null>(null);
  let subscribeForm = $state<HTMLFormElement | null>(null);

  let managing = $state(false);
  let resuming = $state(false);
  let cancelDialogOpen = $state(false);
  let cancelling = $state(false);
  let cancelForm = $state<HTMLFormElement | null>(null);

  let giftModalOpen = $state(false);
  let giftLaunching = $state(false);
  let giftRecipient = $state('');
  let giftMessage = $state('');

  const giftMessageHasLink = $derived(giftMessage.trim().length > 0 && containsLink(giftMessage));
  const giftNeedsRecipient = $derived(giftRecipient.trim().length === 0);

  let celebrateOpen = $state(false);
  let celebrateKind = $state<'premium' | 'gift'>('premium');
  let celebrateRecipient = $state('');
  let activationSlow = $state(false);

  const INTENT_KEY = 'bagel_checkout_intent';


  function stashIntent(kind: 'premium' | 'gift', recipient = '') {
    try {
      sessionStorage.setItem(INTENT_KEY, JSON.stringify({ kind, recipient }));
    } catch {
    }
  }

  function readIntent(): { kind: 'premium' | 'gift'; recipient: string } | null {
    try {
      const raw = sessionStorage.getItem(INTENT_KEY);
      sessionStorage.removeItem(INTENT_KEY);
      if (!raw) return null;
      const parsed = JSON.parse(raw);
      return { kind: parsed.kind === 'gift' ? 'gift' : 'premium', recipient: String(parsed.recipient ?? '') };
    } catch {
      return null;
    }
  }

  function stripCheckoutParam() {
    const url = new URL(window.location.href);
    if (!url.searchParams.has('checkout')) return;
    url.searchParams.delete('checkout');
    replaceState(url, {});
  }

  function openGift() {
    giftModalOpen = true;
  }
  function closeGift() {
    if (giftLaunching) return;
    giftModalOpen = false;
  }
  function onSubscribeSubmit(plan: 'monthly' | 'once') {
    launching = plan;
    stashIntent('premium');
  }
  function onGiftSubmit(e: SubmitEvent) {
    if (giftMessageHasLink) {
      e.preventDefault();
      return;
    }
    giftLaunching = true;
    stashIntent('gift', giftRecipient.trim());
  }
  function openCancel() {
    cancelDialogOpen = true;
  }
  function closeCancel() {
    if (cancelling) return;
    cancelDialogOpen = false;
  }

  function prizeDate(value?: string | null): string {
    return fmtDateTime(value, '');
  }

  const PRIZE_COPY: Record<string, string> = {
    completed: 'billing.prizeCompleted',
    active: 'billing.prizeActive',
    scheduled: 'billing.prizeScheduled'
  };
  function prizeCopy(prize: PrizeAward): string {
    return t(PRIZE_COPY[prize.state] ?? 'billing.prizePending');
  }
  function confirmCancel() {
    cancelling = true;
    cancelForm?.requestSubmit();
  }

  onMount(() => {
    if (!data.autostart) return;
    const url = new URL(window.location.href);
    url.searchParams.delete('subscribe');
    replaceState(url, {});
    if (canSubscribe && !launching) subscribeForm?.requestSubmit();
  });

  function watchActivation(): () => void {
    awaitingActivation = true;
    const startedAt = Date.now();
    const timer = setInterval(async () => {
      await invalidateAll();
      if (isPaid) {
        clearInterval(timer);
        return;
      }
      if (Date.now() - startedAt < ACTIVATION_WINDOW_MS) return;
      clearInterval(timer);
      activationSlow = true;
    }, ACTIVATION_POLL_MS);
    return () => clearInterval(timer);
  }

  $effect(() => {
    if (isPaid) awaitingActivation = false;
  });

  onMount(() => {
    if (page.url.searchParams.get('checkout') !== 'complete') return;

    const intent = readIntent();
    celebrateKind = intent?.kind ?? 'premium';
    celebrateRecipient = intent?.recipient ?? '';
    celebrateOpen = true;
    stripCheckoutParam();

    if (celebrateKind === 'gift') {
      toast('success', t('billing.toastGiftSent'));
      return;
    }

    toast('success', t('billing.toastPaymentReceived'));
    return isPaid ? undefined : watchActivation();
  });

  const fmtDate = (iso?: string | null) =>
    iso
      ? new Date(iso).toLocaleDateString(i18n.locale, { year: 'numeric', month: 'long', day: 'numeric' })
      : '';

  let checkoutToasted = false;
  $effect(() => {
    if (checkoutToasted) return;
    if (page.url.searchParams.get('checkout') !== 'cancelled') return;
    checkoutToasted = true;
    toast('danger', t('billing.toastCheckoutCancelled'));
  });

  reseedOnChange(() => form, (next) => {
    if (!next) return;
    launching = null;
    giftLaunching = false;
    managing = false;
    resuming = false;
    cancelling = false;
    cancelDialogOpen = false;
    if (next.error) toast('danger', String(next.error));
    if (next.gift) {
      giftModalOpen = true;
      if ('recipient' in next) giftRecipient = String(next.recipient);
      if ('message' in next) giftMessage = String(next.message);
    }
  });
</script>

<AuroraBg />
<div class="starfield" aria-hidden="true"><LightField warmth={0.7} /></div>

<section class="screen active">
  <PageHead
    eyebrow={t('billing.eyebrow')}
    description={isPaid ? t('billing.descManage') : t('billing.descChoose')}
  >
    {isPaid ? t('billing.managePre') : t('billing.choosePre')}<em>{t('billing.planEm')}</em>
    {#snippet trailing()}
      <div class="gift-trail">
        <Button variant="secondary" onclick={openGift}>{t('billing.giftLink')}</Button>
      </div>
    {/snippet}
  </PageHead>

  {#if data.degraded}
    <AlertBanner>{t('billing.degraded')}</AlertBanner>
  {/if}
  {#if data.prizeDegraded}
    <AlertBanner>{t('billing.prizeUnavailable')}</AlertBanner>
  {/if}

  {#if prizes.length}
    <div class="prize">
      <Card as="section" tone="accent" aria-labelledby="prize-title">
        <div class="prize-card-head">
          <div class="prize-cell"><Eyebrow>{t('billing.prizeTitle')}</Eyebrow><Heading level={5} as="h2" id="prize-title">{t('billing.prizeTimeline')}</Heading></div>
          <span class="prize-mark" aria-hidden="true">✦</span>
        </div>
        <Table label={t('billing.prizeTimeline')} compact minWidth="520px">
          <tbody>
            {#each prizes as prize (prize.id)}
              <tr>
                <th scope="row">
                  <div class="prize-cell"><strong>{t('billing.prizeMonths', { n: prize.prizeMonths })}</strong><Text as="span" size="xs" tone="muted">{prizeCopy(prize)}</Text></div>
                </th>
                <td>
                  <div class="prize-cell">
                    {#if prize.confirmedStart || prize.plannedStart}<Text as="span" size="xs" tone="pale">{prizeDate(prize.confirmedStart ?? prize.plannedStart)}</Text>{/if}
                    {#if prize.confirmedEnd || prize.plannedEnd}<Text as="span" size="xs" tone="pale">{prizeDate(prize.confirmedEnd ?? prize.plannedEnd)}</Text>{/if}
                    {#if prize.billingState === 'pending' || prize.billingState === 'uncertain'}<Text as="small" size="xs" tone="warning">{t('billing.prizeBillingPending')}</Text>{/if}
                  </div>
                </td>
                <td>{#if prize.emailState === 'missing_contact'}<Text as="small" size="xs" tone="warning">{t('billing.prizeEmailMissing')}</Text>{/if}</td>
              </tr>
            {/each}
          </tbody>
        </Table>
      </Card>
    </div>
  {/if}

  {#if !isPaid}

    <p class="plan-status">
      <Tag tone="quiet">{t('billing.currentPlan')}</Tag>
      <Tag tone="live" status>{awaitingActivation ? t('billing.activating') : statusLabel}</Tag>
    </p>

    <h2 class="bb-sr-only">{t('billing.comparePlans')}</h2>
    <div class="plans">
      <Card>
        <div class="plan">
          <div class="plan-title">
            <Label mono as="span">{t('billing.currentPlan')}</Label>
            <Heading level={6} as="h3" variant="title" uppercase>{t('billing.free')}</Heading>
          </div>
          <p class="plan-price">
            <span class="plan-amt">{t('billing.free')}</span>
          </p>
          <div class="plan-desc"><Text size="sm" tone="muted">{t('billing.freeDesc')}</Text></div>
          <ul class="plan-feats">
            {#each freeFeatures as feature}
              <Text as="li" size="sm" tone="soft">{feature}</Text>
            {/each}
          </ul>
          <div class="plan-current">
            <Tag tone="live">{t('billing.onThisPlan')}</Tag>
          </div>
        </div>
      </Card>

      <Card tone="accent">
        <div class="plan">
          <span class="plan-badge"><Badge shape="pill" tone="paid">{t('billing.priorityLane')}</Badge></span>
          <div class="plan-title">
            <Label mono as="span">{t('billing.upgrade')}</Label>
            <Heading level={6} as="h3" variant="title" uppercase>{t('billing.premium')}</Heading>
          </div>
          <p class="plan-price">
            <span class="plan-amt">{money}</span>
            <Text as="span" size="sm" tone="muted">{t('billing.perMonth')}</Text>
          </p>
          <div class="plan-desc"><Text size="sm" tone="muted">{t('billing.premiumDesc')}</Text></div>
          <ul class="plan-feats">
            {#each premiumFeatures as feature}
              <Text as="li" size="sm" tone="soft">{feature}</Text>
            {/each}
          </ul>
          <div class="plan-buttons">
            <form method="POST" action="?/subscribe" bind:this={subscribeForm} onsubmit={() => onSubscribeSubmit('monthly')}>
              <input type="hidden" name="plan" value="monthly" />
              <Button
                type="submit"
                variant="primary"
                block
                busy={launching === 'monthly'}
                disabled={launching === 'once' || awaitingActivation}
                aria-describedby="premium-fine"
              >
                {t('billing.subscribeMonthly')}
              </Button>
            </form>
            <form method="POST" action="?/subscribe" onsubmit={() => onSubscribeSubmit('once')}>
              <input type="hidden" name="plan" value="once" />
              <Button
                type="submit"
                variant="secondary"
                block
                busy={launching === 'once'}
                disabled={launching === 'monthly' || awaitingActivation}
                aria-describedby="premium-fine"
              >
                {t('billing.buyOneMonth')}
              </Button>
            </form>
          </div>
          <div class="launch-note" role="status" class:is-on={launching !== null}>
            <Text size="sm" tone="accent">{launching ? t('billing.takingToCheckout') : ''}</Text>
          </div>
          <div class="plan-fine"><Text size="xs" tone="muted" id="premium-fine">{t('billing.premiumFine')} &middot; {t('billing.tebexNote')}</Text></div>
        </div>
      </Card>
    </div>

    <div class="oath">
      <Card style="--card-pad: 11px 22px">
        <Text size="xs" mono tone="muted">{t('billing.oath')}</Text>
      </Card>
    </div>

    {#if form?.error && !form?.gift}
      <div class="form-error"><FieldError message={String(form.error)} /></div>
    {/if}
  {:else}
    {#if paymentFailed}
      <AlertBanner tone="warning">
        {t('billing.paymentFailed')}
        {#snippet actions()}
          <form method="POST" action="?/cancel" onsubmit={() => (managing = true)}>
            <Button type="submit" variant="primary" busy={managing}>{t('billing.updatePayment')}</Button>
          </form>
        {/snippet}
      </AlertBanner>
    {/if}
    <div class="premium-halo">
    <HaloPanel as="section" aria-label={statusLabel}>
    <div class="premium-dashboard-hero">
      <div class="premium-hero-content">
        <MarkBadge src="/premium-logo.png" size="lg" tone="premium" />
        <div class="premium-hero-text" role="status">
          <Eyebrow>{t('billing.currentPlan')}</Eyebrow>
          <h2 class="premium-title">{statusLabel}</h2>

          {#if tebexPaid}
            <p class="plan-price premium-price">
              <span class="plan-amt">{money}</span>
              <Text as="span" size="sm" tone="muted">{t('billing.perMonth')}</Text>
              {#if cancelPending && paidUntil}
                <Tag tone="alpha">{t('billing.endsOn', { date: fmtDate(paidUntil) })}</Tag>
              {/if}
            </p>
          {/if}

          {#if isVip}
            <p class="premium-hint">{t('billing.vipHint')}</p>
          {:else if staffGrant}
            <p class="premium-hint">
              {t('billing.staffGrantHint', { until: paidUntil ? t('billing.activeUntil', { date: fmtDate(paidUntil) }) : '' })}
            </p>
          {:else if tebexPaid}
            <p class="premium-hint">
              {t('billing.tebexHint', {
                state: account.cancelPending ? t('billing.cancelScheduled') : t('billing.activeThroughTebex'),
                until: paidUntil ? t('billing.untilDate', { date: fmtDate(paidUntil) }) : ''
              })}
            </p>
          {:else}
            <p class="premium-hint">{t('billing.premiumActive', { until: paidUntil ? t('billing.untilDate', { date: fmtDate(paidUntil) }) : '' })}</p>
          {/if}
        </div>
      </div>

      <div class="premium-hero-actions">
        {#if canManage}
          <div class="premium-actions-row">
            <form method="POST" action="?/cancel" onsubmit={() => (managing = true)}>
              <Button type="submit" variant="primary" busy={managing} aria-describedby="manage-note">
                {t('billing.manageSubscription')}
              </Button>
            </form>
            {#if cancelPending}
              <form method="POST" action="?/cancel" onsubmit={() => (resuming = true)}>
                <Button type="submit" variant="secondary" busy={resuming} aria-describedby="manage-note">
                  {t('billing.resumeSubscription')}
                </Button>
              </form>
            {:else}
              <Button onclick={openCancel} tone="danger">
                {t('billing.cancelSubscription')}
              </Button>
            {/if}
          </div>
          <p class="premium-tiny-hint" id="manage-note">{t('billing.manageTiny')}</p>
          <p class="premium-tiny-hint">{t('billing.receiptsEmailed')}</p>
        {/if}
        {#if form?.error && !form?.gift}
          <div class="form-error"><FieldError message={String(form.error)} /></div>
        {/if}
      </div>
    </div>
    </HaloPanel>
    </div>

    <section class="premium-includes">
      <div class="includes-h"><Heading level={6} as="h3" variant="label">{t('billing.premiumIncludes')}</Heading></div>
      <ul class="plan-feats plan-feats--flow">
        {#each premiumFeatures as feature}
          <Text as="li" size="sm" tone="soft">{feature}</Text>
        {/each}
      </ul>
    </section>

    <div class="gift-card">
      <Card>
        <div class="gift-cta">
          <div class="gift-copy">
            <Heading level={6} as="h2" variant="title">{t('billing.giftPremium')}</Heading>
            <Text size="sm" tone="muted">
              {t('billing.giftCtaHint')}
            </Text>
          </div>
          <Button variant="secondary" onclick={openGift}>
            {t('billing.giftPremium')}
          </Button>
        </div>
      </Card>
    </div>
  {/if}
</section>

<ConfirmDialog
  open={cancelDialogOpen}
  title={t('billing.cancelConfirmTitle')}
  body={paidUntil
    ? t('billing.cancelConfirmBody', { date: fmtDate(paidUntil) })
    : t('billing.cancelConfirmBodyNoDate')}
  confirmLabel={t('billing.cancelConfirmLabel')}
  cancelLabel={t('billing.keepSubscription')}
  busy={cancelling}
  onConfirm={confirmCancel}
  onCancel={closeCancel}
  tone="danger"
/>
<form method="POST" action="?/cancel" bind:this={cancelForm} hidden></form>

<Modal open={giftModalOpen} title={t('billing.giftPremium')} onClose={closeGift}>
  <p class="bb-modal__body">
    {t('billing.giftModalBody')}
  </p>
  <form method="POST" action="?/gift" onsubmit={onGiftSubmit} class="gift-form">
    <Field label={t('billing.twitchUsername')}>
      <Input
        name="recipient"
        data-cursor
        placeholder={t('billing.usernamePlaceholder')}
        autocomplete="off"
        spellcheck="false"
        maxlength={26}
        bind:value={giftRecipient}
        readonly={giftLaunching}
      />
    </Field>
    <Field
      label={t('billing.messageLabel')}
      tag={t('billing.optional')}
      error={giftMessageHasLink ? t('billing.giftNoteLink') : undefined}
      errorId="gift-msg-error"
    >
      <Textarea
        name="message"
        data-cursor
        placeholder={t('billing.messagePlaceholder')}
        maxlength={280}
        rows={3}
        invalid={giftMessageHasLink}
        bind:value={giftMessage}
        aria-describedby="gift-msg-counter{giftMessageHasLink ? ' gift-msg-error' : ''}"
        readonly={giftLaunching}
      />
      <span class="counter">
        <Text as="span" size="xs" tone={giftMessage.length >= 280 ? 'accent' : 'muted'} mono id="gift-msg-counter">{giftMessage.length}/280</Text>
      </span>
    </Field>
    {#if form?.gift && form?.error}
      <FieldError message={String(form.error)} />
    {/if}
    {#if giftNeedsRecipient && !giftLaunching}
      <Text size="xs" tone="muted" id="gift-need-recipient">{t('billing.giftNeedRecipient')}</Text>
    {/if}
    <div class="bb-modal__actions">
      <Button variant="ghost" onclick={closeGift} disabled={giftLaunching}>{t('common.cancel')}</Button>
      <Button
        type="submit"
        variant="primary"
        busy={giftLaunching}
        disabled={giftNeedsRecipient || giftMessageHasLink}
        aria-describedby={giftNeedsRecipient ? 'gift-need-recipient' : undefined}
      >
        {t('billing.giftPremium')}
      </Button>
    </div>
  </form>
</Modal>

<CelebrationModal
  bind:open={celebrateOpen}
  kind={celebrateKind}
  recipient={celebrateRecipient}
  {isPaid}
  {activationSlow}
/>

<style>
  .starfield {
    position: fixed;
    inset: 0;
    z-index: 0;
    pointer-events: none;
  }
  .screen {
    --label-mono-size: var(--bb-text-xs);
    --h-label-size: var(--bb-text-xs);
    --badge-pill-size: var(--bb-text-xs);

    position: relative;
    z-index: 1;
  }

  .prize { margin: 18px 0 22px; }
  .prize-card-head { display:flex; justify-content:space-between; align-items:flex-start; gap:16px; margin-bottom:14px; }
  .prize-cell { display:flex; flex-direction:column; gap:5px; }
  .prize-mark { color:var(--bb-tan-pale); font-size:24px; }

  .plan-status {
    display: inline-flex;
    align-items: center;
    gap: 10px;
    margin: 0 0 4px;
  }

  .plans {
    display: grid;
    grid-template-columns: 1fr;
    gap: 20px;
    margin-top: 18px;
  }
  @media (min-width: 820px) {
    .plans {
      grid-template-columns: repeat(2, 1fr);
    }
  }

  .plan {
    display: flex;
    flex-direction: column;
    height: 100%;
  }
  .plan-badge {
    position: absolute;
    top: 16px;
    right: 16px;
  }
  .plan-title {
    display: flex;
    flex-direction: column;
    gap: 6px;
    margin-bottom: 12px;
  }
  .plan-price {
    display: flex;
    flex-wrap: wrap;
    align-items: baseline;
    gap: 7px;
    margin: 0 0 12px;
  }
  .plan-amt {
    font-family: var(--bb-font-display);
    font-weight: 800;
    font-size: 3rem;
    line-height: 1;
    letter-spacing: var(--bb-tracking-tight);
    color: var(--bb-white);
    font-variant-numeric: tabular-nums;
  }
  .plan-desc {
    margin: 0 0 20px;
    max-width: 42ch;
  }
  .plan-feats {
    list-style: none;
    display: flex;
    flex-direction: column;
    gap: 11px;
    margin: 0 0 24px;
    padding: 20px 0 0;
    border-top: 1px solid var(--bb-border);
  }
  .plan-current {
    margin: auto 0 0;
  }
  .plan-buttons {
    display: flex;
    gap: 10px;
    margin-top: auto;
  }
  .plan-buttons form {
    flex: 1;
  }
  .launch-note {
    min-height: calc(var(--bb-text-sm) * 1.5);
    margin: 12px 0 0;
    visibility: hidden;
    opacity: 0;
    transition: opacity var(--bb-dur-fast) var(--bb-ease-out-expo);
  }
  .launch-note.is-on {
    visibility: visible;
    opacity: 1;
  }
  .plan-fine {
    margin: 4px 0 0;
  }

  .oath {
    margin: 18px auto 0;
    max-width: fit-content;
    text-align: center;
  }

  .gift-trail { --btn-min-h: 44px; }

  .premium-halo { margin-top: 24px; }
  .premium-dashboard-hero {
    padding: 32px;
    display: flex;
    flex-direction: column;
    gap: 32px;
  }
  @media (min-width: 720px) {
    .premium-dashboard-hero {
      flex-direction: row;
      align-items: flex-start;
      justify-content: space-between;
    }
  }

  .premium-hero-content {
    display: flex;
    align-items: flex-start;
    gap: 24px;
  }

  .premium-hero-text {
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  .premium-title {
    font-family: var(--bb-font-display);
    font-weight: 800;
    font-size: 28px;
    letter-spacing: var(--bb-tracking-display);
    color: var(--bb-white);
    margin: 0;
    background: linear-gradient(135deg, var(--bb-white) 0%, var(--bb-tan-pale) 100%);
    -webkit-background-clip: text;
    background-clip: text;
    -webkit-text-fill-color: transparent;
  }
  .premium-price {
    gap: 3px 8px;
    margin: 0;
  }
  .premium-price .plan-amt {
    font-size: 1.6rem;
    margin-right: 4px;
  }
  .premium-hint {
    font-family: var(--bb-font-body);
    font-size: 14.5px;
    line-height: 1.5;
    color: var(--bb-muted-light);
    max-width: 46ch;
    margin: 0;
  }

  .premium-hero-actions {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 12px;
  }
  @media (min-width: 720px) {
    .premium-hero-actions {
      align-items: flex-end;
    }
  }

  .premium-actions-row {
    display: flex;
    flex-wrap: wrap;
    gap: 12px;
  }

  .premium-tiny-hint {
    font-size: var(--bb-text-xs);
    color: var(--bb-muted);
    margin: 0;
    max-width: 40ch;
  }
  @media (min-width: 720px) {
    .premium-tiny-hint {
      text-align: right;
    }
  }

  .premium-includes {
    margin-top: 22px;
  }
  .includes-h {
    margin: 0 0 4px;
  }
  .plan-feats--flow {
    border-top: none;
    padding-top: 8px;
  }
  @media (min-width: 620px) {
    .plan-feats--flow {
      display: grid;
      grid-template-columns: repeat(2, minmax(0, 1fr));
      gap: 12px 24px;
    }
  }

  .gift-card {
    margin-top: 18px;
  }
  .gift-cta {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 18px;
  }
  .gift-copy {
    display: flex;
    flex-direction: column;
    gap: 6px;
    max-width: 52ch;
  }

  .form-error {
    text-align: center;
    margin-top: 14px;
  }

  .gift-form {
    --field-mb: 0;

    display: flex;
    flex-direction: column;
    gap: 16px;
  }
  .counter {
    align-self: flex-end;
  }

  @media (max-width: 760px) {
    .gift-cta {
      --btn-w: 100%;
      --btn-justify: center;

      flex-direction: column;
    }
    .plan-buttons {
      flex-direction: column;
    }
  }
</style>
