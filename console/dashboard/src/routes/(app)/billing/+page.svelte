<script lang="ts">
  import { Icon, PageHead, Card, Modal, AlertBanner, Button, ConfirmDialog, FieldError, toast, getI18n, containsLink } from '@bagel/shared';
  import { page } from '$app/state';
  import { replaceState } from '$app/navigation';
  import { onMount } from 'svelte';
  import type { BillingState } from '$lib/server/services';

  let { data, form } = $props();

  const i18n = getI18n();
  const { t } = i18n;

  const account = $derived(data.account as BillingState);

  let optimisticPaid = $state(false);

  const isVip = $derived(account.status === 'vip');
  const isPaid = $derived(account.status === 'paid' || isVip || optimisticPaid);
  const staffGrant = $derived(account.status === 'paid' && account.source === 'admin');
  const tebexPaid = $derived((account.status === 'paid' && account.source === 'tebex') || optimisticPaid);
  const paidUntil = $derived(account.expiresAt);
  // Basket minting happens server-side on click; the browser is then redirected
  // to Tebex-hosted checkout so payment collection never happens in our frame.
  // Owners and billing-granted delegates share the same CTAs (the server gates
  // access + targets the owner's account).
  const canSubscribe = $derived(!isPaid);
  // Rendered for every Tebex subscriber even when TEBEX_CANCEL_URL is not
  // configured: the ?/cancel action then answers 503 with a clear toast, which
  // beats silently hiding the only cancellation path.
  const canManage = $derived(tebexPaid);
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
    t('billing.premiumFeat4')
  ]);

  let launching = $state(false);
  let subscribeForm = $state<HTMLFormElement | null>(null);

  // Manage/cancel both POST ?/cancel (the server gates the session, verifies the
  // plan, then redirects to Tebex-hosted management). Cancel routes through a
  // ConfirmDialog first; the hidden form below is what it submits.
  let managing = $state(false);
  let cancelDialogOpen = $state(false);
  let cancelling = $state(false);
  let cancelForm = $state<HTMLFormElement | null>(null);

  // Gift modal state.
  let giftModalOpen = $state(false);
  let giftLaunching = $state(false);
  let giftRecipient = $state('');
  let giftMessage = $state('');

  // Gift notes are emailed to the recipient, so links are refused. Checked live
  // for instant feedback, again in the server action, and a third time in the
  // transactions service (@bagel/shared/validation mirrors the Go detector).
  const giftMessageHasLink = $derived(giftMessage.trim().length > 0 && containsLink(giftMessage));
  // The gift submit is disabled until there is a recipient; the reason is
  // surfaced via aria-describedby so the block is never silent.
  const giftNeedsRecipient = $derived(giftRecipient.trim().length === 0);

  // Celebratory purchase-complete modal (replaces the old top ribbon).
  let celebrateOpen = $state(false);
  let celebrateKind = $state<'premium' | 'gift'>('premium');
  let celebrateRecipient = $state('');
  let activationSlow = $state(false);
  let celebratedActivation = $state(false);
  let confetti = $state<
    { tx: number; ty: number; rot: number; delay: number; dur: number; color: string; w: number; h: number }[]
  >([]);

  const INTENT_KEY = 'bagel_checkout_intent';

  const prefersReducedMotion = () =>
    typeof window !== 'undefined' && window.matchMedia('(prefers-reduced-motion: reduce)').matches;

  function stashIntent(kind: 'premium' | 'gift', recipient = '') {
    try {
      sessionStorage.setItem(INTENT_KEY, JSON.stringify({ kind, recipient }));
    } catch {
      /* private mode / storage disabled — modal falls back to the premium copy */
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

  // Drop ?checkout=complete from the URL. Kept in the URL while polling so each
  // invalidateAll re-runs the load's fresh-read path; stripped once we stop.
  function stripCheckoutParam() {
    const url = new URL(window.location.href);
    if (!url.searchParams.has('checkout')) return;
    url.searchParams.delete('checkout');
    replaceState(url, {});
  }

  const CONFETTI_COLORS = ['#c9a87c', '#e0c49a', '#52b788', '#f0ece4'];

  function burst() {
    if (prefersReducedMotion()) return;
    confetti = Array.from({ length: 44 }, () => ({
      tx: Math.round((Math.random() - 0.5) * 620),
      ty: Math.round(140 + Math.random() * 460),
      rot: Math.round((Math.random() - 0.5) * 720),
      delay: Math.round(Math.random() * 140),
      dur: Math.round(900 + Math.random() * 700),
      color: CONFETTI_COLORS[Math.floor(Math.random() * CONFETTI_COLORS.length)],
      w: 6 + Math.round(Math.random() * 6),
      h: 3 + Math.round(Math.random() * 4)
    }));
    // Clear after the longest piece finishes so re-opens start clean.
    setTimeout(() => (confetti = []), 1900);
  }

  function openGift() {
    giftModalOpen = true;
  }
  function closeGift() {
    if (giftLaunching) return;
    giftModalOpen = false;
  }
  function onSubscribeSubmit() {
    launching = true;
    stashIntent('premium');
  }
  function onGiftSubmit(e: SubmitEvent) {
    // Block the round-trip if a link is present; the inline error already shows.
    if (giftMessageHasLink) {
      e.preventDefault();
      return;
    }
    giftLaunching = true;
    stashIntent('gift', giftRecipient.trim());
  }
  function closeCelebrate() {
    celebrateOpen = false;
    confetti = [];
  }

  // Cancellation: confirm the consequence first, then submit the hidden ?/cancel
  // form (which redirects to Tebex-hosted management).
  function openCancel() {
    cancelDialogOpen = true;
  }
  function closeCancel() {
    if (cancelling) return;
    cancelDialogOpen = false;
  }
  function confirmCancel() {
    cancelling = true;
    cancelForm?.requestSubmit();
  }

  // Auto-open checkout when the pricing page sent the visitor here with
  // ?subscribe=1 (possibly via the login flow). One shot: the param is stripped
  // so a refresh does not re-launch.
  onMount(() => {
    if (!data.autostart) return;
    const url = new URL(window.location.href);
    url.searchParams.delete('subscribe');
    replaceState(url, {});
    if (canSubscribe && !launching) subscribeForm?.requestSubmit();
  });

  // Returning from hosted checkout with a completed payment: open the
  // celebratory modal with copy that matches what the buyer did (self-purchase
  // vs gift, recovered from the sessionStorage intent stashed at submit time).
  // A self-purchase immediately optimistically updates the UI to show Premium,
  // bypassing the spinner, while the backend syncs.
  // A gift changes nothing on the buyer's own account so it does not poll.
  onMount(() => {
    if (page.url.searchParams.get('checkout') !== 'complete') return;

    const intent = readIntent();
    celebrateKind = intent?.kind ?? 'premium';
    celebrateRecipient = intent?.recipient ?? '';
    celebrateOpen = true;
    burst();
    stripCheckoutParam();

    // A gift never changes the buyer's own plan, so there is nothing to wait for.
    if (celebrateKind === 'gift') {
      toast('ok', t('billing.toastGiftSent'));
      return;
    }

    toast('ok', t('billing.toastPaymentReceived'));
    // Optimistically update the UI to show premium immediately.
    optimisticPaid = true;
  });

  // When a self-purchase finally flips to paid while the modal is open, fire a
  // second confetti burst to mark the activation.
  $effect(() => {
    if (celebrateOpen && celebrateKind === 'premium' && isPaid && !celebratedActivation) {
      celebratedActivation = true;
      burst();
    }
  });

  const fmtDate = (iso?: string | null) =>
    iso
      ? new Date(iso).toLocaleDateString(i18n.locale, { year: 'numeric', month: 'long', day: 'numeric' })
      : '';

  // A cancelled checkout only needs the one toast.
  let checkoutToasted = false;
  $effect(() => {
    if (checkoutToasted) return;
    if (page.url.searchParams.get('checkout') !== 'cancelled') return;
    checkoutToasted = true;
    toast('err', t('billing.toastCheckoutCancelled'));
  });

  // svelte-ignore state_referenced_locally
  let lastForm: unknown = form;
  $effect(() => {
    if (form === lastForm) return;
    lastForm = form;
    if (!form) return;
    // A form result means the action did not redirect to Tebex — re-enable the
    // buttons instead of leaving them stuck on the loading state, and drop the
    // cancel confirmation so its error surfaces as a toast.
    launching = false;
    giftLaunching = false;
    managing = false;
    cancelling = false;
    cancelDialogOpen = false;
    if (form.error) toast('err', String(form.error));
    // A gift error re-renders the whole page (plain POST), losing the modal —
    // reopen it and repopulate the fields the action echoed back.
    if (form.gift) {
      giftModalOpen = true;
      if ('recipient' in form) giftRecipient = String(form.recipient);
      if ('message' in form) giftMessage = String(form.message);
    }
  });
</script>

<section class="screen active">
  <PageHead
    eyebrow={t('billing.eyebrow')}
    description={isPaid ? t('billing.descManage') : t('billing.descChoose')}
  >
    {isPaid ? t('billing.managePre') : t('billing.choosePre')}<em>{t('billing.planEm')}</em>
  </PageHead>

  <!-- 6. Error / unavailable state — announced (AlertBanner is role="alert"). -->
  {#if data.degraded}
    <AlertBanner>{t('billing.degraded')}</AlertBanner>
  {/if}

  {#if !isPaid}
    <!-- ────── SELECTION VIEW (free plan) ────── -->

    <!-- 1. Current plan + status — TEXT, announced on change. -->
    <p class="plan-status" role="status">
      <span class="ps-label">{t('billing.currentPlan')}</span>
      <span class="ps-value">{statusLabel}</span>
    </p>

    <!-- 3. Plan comparison. Heading kept for structure, hidden visually. -->
    <h2 class="sr-only">{t('billing.comparePlans')}</h2>
    <div class="plans">
      <!-- Free: the whole product -->
      <Card class="plan-card">
        <span class="plan-eyebrow">{t('billing.currentPlan')}</span>
        <h3 class="plan-headline">{t('billing.free')}</h3>
        <p class="plan-price">
          <span class="plan-amt">$0</span>
          <span class="plan-per">{t('billing.priceForever')}</span>
        </p>
        <p class="plan-desc">{t('billing.freeDesc')}</p>
        <!-- 4. Features — semantic list. -->
        <ul class="plan-feats">
          {#each freeFeatures as feature}
            <li><Icon name="check" size={15} />{feature}</li>
          {/each}
        </ul>
        <p class="plan-current">{t('billing.onThisPlan')}</p>
      </Card>

      <!-- Premium: the upgrade -->
      <Card class="plan-card plan-card--premium">
        <span class="plan-badge">{t('billing.priorityLane')}</span>
        <span class="plan-eyebrow">{t('billing.upgrade')}</span>
        <h3 class="plan-headline">{t('billing.premium')}</h3>
        <p class="plan-price">
          <span class="plan-amt">$7</span>
          <span class="plan-per">{t('billing.perMonth')}</span>
        </p>
        <p class="plan-desc">{t('billing.premiumDesc')}</p>
        <!-- 4. Features — semantic list. -->
        <ul class="plan-feats">
          {#each premiumFeatures as feature}
            <li><Icon name="check" size={15} />{feature}</li>
          {/each}
        </ul>
        <!-- 2. Primary billing action — the ONE primary on the page. -->
        <div class="plan-buttons">
          <form method="POST" action="?/subscribe" bind:this={subscribeForm} onsubmit={onSubscribeSubmit}>
            <input type="hidden" name="plan" value="monthly" />
            <Button
              type="submit"
              variant="primary"
              icon="heart"
              loading={launching}
              aria-describedby="premium-fine"
            >
              {t('billing.subscribeMonthly')}
            </Button>
          </form>
          <form method="POST" action="?/subscribe" onsubmit={onSubscribeSubmit}>
            <input type="hidden" name="plan" value="once" />
            <Button type="submit" variant="secondary" loading={launching} aria-describedby="premium-fine">
              {t('billing.buyOneMonth')}
            </Button>
          </form>
        </div>
        <p class="plan-fine" id="premium-fine">{t('billing.premiumFine')} &middot; {t('billing.tebexNote')}</p>
      </Card>
    </div>

    <p class="oath">{t('billing.oath')}</p>

    <!-- 5. Billing-management action: gift Premium to someone else. -->
    <div class="gift-link-row">
      <button type="button" class="gift-link" onclick={openGift}>{t('billing.giftLink')}</button>
    </div>
    {#if form?.error && !form?.gift}
      <p class="form-error center" role="alert">{form.error}</p>
    {/if}
  {:else}
    <!-- ────── MANAGEMENT VIEW (premium / vip) ────── -->
    <div class="premium-dashboard-hero">
      <div class="premium-hero-content">
        <div class="premium-hero-badge">
          <img src="/premium-logo.png" alt="" />
        </div>
        <!-- 1. Current plan + status — TEXT, announced on change. -->
        <div class="premium-hero-text" role="status">
          <span class="premium-eyebrow">{t('billing.currentPlan')}</span>
          <h2 class="premium-title">{statusLabel}</h2>

          {#if tebexPaid}
            <p class="premium-price">
              <span class="plan-amt">$7</span>
              <span class="plan-per">{t('billing.perMonth')}</span>
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
            <!-- 2. Primary billing action — the ONE primary. -->
            <form method="POST" action="?/cancel" onsubmit={() => (managing = true)}>
              <Button type="submit" variant="primary" loading={managing} aria-describedby="manage-note">
                {t('billing.manageSubscription')}
              </Button>
            </form>
            <!-- 5. Cancellation — confirmed first via ConfirmDialog. -->
            <Button variant="destructive" onclick={openCancel}>
              {t('billing.cancelSubscription')}
            </Button>
          </div>
          <p class="premium-tiny-hint" id="manage-note">{t('billing.manageTiny')}</p>
        {/if}
        {#if form?.error && !form?.gift}
          <p class="form-error center" role="alert">{form.error}</p>
        {/if}
      </div>
    </div>

    <!-- 4. Features — what the current plan includes. -->
    <section class="premium-includes">
      <h3 class="includes-h">{t('billing.premiumIncludes')}</h3>
      <ul class="plan-feats plan-feats--flow">
        {#each premiumFeatures as feature}
          <li><Icon name="check" size={15} />{feature}</li>
        {/each}
      </ul>
    </section>

    <!-- Gift: available whatever your own plan is. -->
    <Card class="billing-card premium-gift-card">
      <div class="gift-cta">
        <div>
          <h2 class="gift-h">{t('billing.giftPremium')}</h2>
          <p class="hint">
            {t('billing.giftCtaHint')}
          </p>
        </div>
        <Button variant="secondary" icon="heart" onclick={openGift}>
          {t('billing.giftPremium')}
        </Button>
      </div>
    </Card>
  {/if}
</section>

<!-- ────── CANCEL CONFIRMATION ────── -->
<ConfirmDialog
  open={cancelDialogOpen}
  title={t('billing.cancelConfirmTitle')}
  body={paidUntil
    ? t('billing.cancelConfirmBody', { date: fmtDate(paidUntil) })
    : t('billing.cancelConfirmBodyNoDate')}
  confirmLabel={t('billing.cancelConfirmLabel')}
  cancelLabel={t('billing.keepSubscription')}
  danger
  busy={cancelling}
  onConfirm={confirmCancel}
  onCancel={closeCancel}
/>
<!-- Submitted programmatically by the confirmation above. -->
<form method="POST" action="?/cancel" bind:this={cancelForm} hidden></form>

<!-- ────── GIFT MODAL (both views) ────── -->
<Modal open={giftModalOpen} title={t('billing.giftPremium')} closeModal={closeGift}>
  <p class="modal-body">
    {t('billing.giftModalBody')}
  </p>
  <form method="POST" action="?/gift" onsubmit={onGiftSubmit} class="gift-form">
    <label class="fld">
      <span class="fld-label">{t('billing.twitchUsername')}</span>
      <input
        class="fld-input"
        type="text"
        name="recipient"
        data-cursor
        placeholder={t('billing.usernamePlaceholder')}
        autocomplete="off"
        spellcheck="false"
        maxlength="26"
        bind:value={giftRecipient}
        readonly={giftLaunching}
      />
    </label>
    <label class="fld">
      <span class="fld-label">{t('billing.messageLabel')} <em>{t('billing.optional')}</em></span>
      <textarea
        class="fld-input fld-textarea"
        name="message"
        data-cursor
        placeholder={t('billing.messagePlaceholder')}
        maxlength="280"
        rows="3"
        bind:value={giftMessage}
        aria-describedby="gift-msg-counter{giftMessageHasLink ? ' gift-msg-error' : ''}"
        readonly={giftLaunching}
      ></textarea>
      <span id="gift-msg-counter" class="counter" class:counter--full={giftMessage.length >= 280}>{giftMessage.length}/280</span>
      {#if giftMessageHasLink}
        <!-- Wrapped so the textarea's aria-describedby target resolves; the shared
             FieldError takes only `message`. -->
        <span id="gift-msg-error"><FieldError message={t('billing.giftNoteLink')} /></span>
      {/if}
    </label>
    {#if form?.gift && form?.error}
      <FieldError message={form.error} />
    {/if}
    {#if giftNeedsRecipient && !giftLaunching}
      <p class="hint tiny" id="gift-need-recipient">{t('billing.giftNeedRecipient')}</p>
    {/if}
    <div class="modal-actions">
      <Button variant="ghost" onclick={closeGift} disabled={giftLaunching}>{t('common.cancel')}</Button>
      <Button
        type="submit"
        variant="primary"
        icon="heart"
        loading={giftLaunching}
        disabled={giftNeedsRecipient || giftMessageHasLink}
        aria-describedby={giftNeedsRecipient ? 'gift-need-recipient' : undefined}
      >
        {t('billing.giftPremium')}
      </Button>
    </div>
  </form>
</Modal>

<!-- ────── CELEBRATORY PURCHASE-COMPLETE MODAL ────── -->
<Modal open={celebrateOpen} closeModal={closeCelebrate}>
  <div class="celebrate">
    <div class="celebrate-badge" class:celebrate-badge--gift={celebrateKind === 'gift'}>
      <Icon name="heart" size={30} />
    </div>

    {#if celebrateKind === 'gift'}
      <h3 class="celebrate-title">{t('billing.giftSent')}</h3>
      <p class="celebrate-body">
        {#if celebrateRecipient}
          {t('billing.giftSentNamedPre')}<strong>@{celebrateRecipient}</strong>{t('billing.giftSentNamedPost')}
        {:else}
          {t('billing.giftSentBody')}
        {/if}
      </p>
    {:else if isPaid}
      <h3 class="celebrate-title">{t('billing.premiumActivated')}</h3>
      <p class="celebrate-body">
        {t('billing.premiumActivatedBody')}
      </p>
    {:else if activationSlow}
      <h3 class="celebrate-title">{t('billing.paymentReceived')}</h3>
      <p class="celebrate-body">
        {t('billing.paymentSlowBody')}
      </p>
    {:else}
      <h3 class="celebrate-title">{t('billing.paymentReceivedTitle')}</h3>
      <p class="celebrate-body">
        {t('billing.paymentReceivedBody')}
      </p>
      <div class="celebrate-spinner" aria-hidden="true"></div>
    {/if}

    <div class="modal-actions celebrate-actions">
      <Button variant="primary" onclick={closeCelebrate}>
        {celebrateKind === 'gift' ? t('common.done') : isPaid ? t('billing.explorePremium') : t('common.gotIt')}
      </Button>
    </div>
  </div>
</Modal>

{#if confetti.length}
  <div class="confetti-layer" aria-hidden="true">
    {#each confetti as p}
      <span
        class="confetti-piece"
        style="--tx:{p.tx}px; --ty:{p.ty}px; --rot:{p.rot}deg; --delay:{p.delay}ms; --dur:{p.dur}ms; background:{p.color}; width:{p.w}px; height:{p.h}px;"
      ></span>
    {/each}
  </div>
{/if}

<style>
  :global(.billing-card) {
    margin-top: 18px;
  }
  .gift-h {
    margin: 0 0 6px;
    font-size: 16px;
    font-family: var(--bb-font-display);
    font-weight: 700;
    color: var(--bb-white);
  }
  .hint {
    color: var(--bb-muted, #998f82);
    font-size: 13px;
    margin: 6px 0 0;
    max-width: 52ch;
  }
  .hint.tiny {
    font-size: 12px;
  }

  /* ── Current-plan status strip (selection view) ── */
  .plan-status {
    display: inline-flex;
    align-items: center;
    gap: 10px;
    margin: 0 0 4px;
    padding: 8px 16px;
    border: 1px solid var(--bb-border);
    border-radius: var(--bb-radius-pill, 100px);
    font-family: var(--bb-font-mono);
    font-size: 11px;
  }
  .ps-label {
    letter-spacing: 0.12em;
    text-transform: uppercase;
    color: var(--bb-muted);
  }
  .ps-value {
    color: var(--bb-green-light, #74c69d);
    font-weight: 600;
  }

  /* ── Selection view: plan cards ── */
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

  :global(.plan-card) {
    position: relative;
    display: flex;
    flex-direction: column;
    height: 100%;
  }
  :global(.plan-card--premium) {
    border-color: rgba(201, 168, 124, 0.4) !important;
    box-shadow: 0 0 44px rgba(201, 168, 124, 0.08);
  }

  /* Seated inside the card's top-right corner: the shared Card clips overflow,
     so a badge straddling the top border (translateY(-50%)) gets cut off. */
  .plan-badge {
    position: absolute;
    top: 16px;
    right: 16px;
    font-family: var(--bb-font-mono);
    font-size: 10px;
    letter-spacing: 0.12em;
    text-transform: uppercase;
    background: var(--bb-tan);
    color: #0a0a0a;
    padding: 4px 12px;
    border-radius: var(--bb-radius-pill, 100px);
    font-weight: 600;
  }
  .plan-eyebrow {
    font-family: var(--bb-font-mono);
    font-size: 11px;
    letter-spacing: var(--bb-tracking-eyebrow, 0.14em);
    text-transform: uppercase;
    color: var(--bb-muted);
  }
  .plan-headline {
    font-family: var(--bb-font-display);
    font-weight: 700;
    font-size: 15px;
    letter-spacing: 0.02em;
    text-transform: uppercase;
    color: var(--bb-white);
    margin: 6px 0 12px;
  }
  .plan-price {
    display: flex;
    align-items: baseline;
    gap: 3px;
    margin: 0 0 12px;
  }
  .plan-amt {
    font-family: var(--bb-font-display);
    font-weight: 800;
    font-size: 3rem;
    line-height: 1;
    letter-spacing: -0.03em;
    color: var(--bb-white);
    font-variant-numeric: tabular-nums;
  }
  .plan-per {
    font-family: var(--bb-font-body);
    font-size: 0.85rem;
    color: var(--bb-muted);
    margin-left: 4px;
  }
  .plan-desc {
    font-family: var(--bb-font-body);
    font-size: 0.9rem;
    line-height: 1.6;
    color: var(--bb-muted);
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
  .plan-feats li {
    display: flex;
    align-items: flex-start;
    gap: 10px;
    font-family: var(--bb-font-body);
    font-size: 0.87rem;
    line-height: 1.45;
    color: rgba(240, 236, 228, 0.82);
  }
  .plan-feats :global(svg) {
    flex-shrink: 0;
    color: var(--bb-green-glow, #52b788);
    margin-top: 1px;
  }
  .plan-current {
    margin: auto 0 0;
    font-family: var(--bb-font-mono);
    font-size: 11px;
    letter-spacing: 0.08em;
    text-transform: uppercase;
    color: var(--bb-green-light, #74c69d);
    border: 1px solid rgba(82, 183, 136, 0.3);
    border-radius: var(--bb-radius-pill, 100px);
    padding: 9px 16px;
    text-align: center;
  }
  .plan-buttons {
    display: flex;
    gap: 10px;
    margin-top: auto;
  }
  .plan-buttons form {
    flex: 1;
  }
  .plan-buttons :global(.btn) {
    width: 100%;
    justify-content: center;
  }
  .plan-fine {
    font-family: var(--bb-font-body);
    font-size: 12px;
    color: var(--bb-muted);
    margin: 12px 0 0;
    line-height: 1.5;
  }

  .oath {
    font-family: var(--bb-font-mono);
    font-size: 11px;
    letter-spacing: 0.05em;
    color: var(--bb-muted);
    text-align: center;
    border: 1px dashed rgba(201, 168, 124, 0.22);
    border-radius: var(--bb-radius-pill, 100px);
    padding: 11px 22px;
    margin: 18px auto 0;
    max-width: fit-content;
  }

  .gift-link-row {
    display: flex;
    justify-content: center;
    margin-top: 22px;
  }
  .gift-link {
    background: none;
    border: none;
    cursor: pointer;
    font-family: var(--bb-font-body);
    font-size: 13.5px;
    color: var(--bb-tan-light);
    /* >=44px hit target while staying a text-style link. */
    min-height: 44px;
    display: inline-flex;
    align-items: center;
    padding: 8px 4px;
    transition: color var(--bb-dur-fast, 160ms) ease;
  }
  .gift-link:hover {
    color: var(--bb-tan-pale, #e8d8c0);
    text-decoration: underline;
    text-underline-offset: 3px;
  }

  /* ── Premium Management View ── */
  .premium-dashboard-hero {
    margin-top: 24px;
    padding: 32px;
    border-radius: 8px 8px;
    border: 1px solid rgba(201, 168, 124, 0.4);
    background: radial-gradient(circle at 10% 0%, rgba(201, 168, 124, 0.12) 0%, rgba(10, 10, 10, 0) 60%),
                linear-gradient(180deg, rgba(201, 168, 124, 0.05) 0%, rgba(10, 10, 10, 0) 100%),
                var(--bb-card-bg, #111110);
    box-shadow: 0 12px 64px rgba(201, 168, 124, 0.1);
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

  .premium-hero-badge {
    width: 64px;
    height: 64px;
    border-radius: 50%;
    background: rgba(201, 168, 124, 0.15);
    border: 1px solid rgba(201, 168, 124, 0.5);
    display: flex;
    align-items: center;
    justify-content: center;
    flex-shrink: 0;
    box-shadow: 0 0 24px rgba(201, 168, 124, 0.2);
  }
  .premium-hero-badge img {
    width: 36px;
    height: 36px;
    object-fit: contain;
  }

  .premium-hero-text {
    display: flex;
    flex-direction: column;
  }
  .premium-eyebrow {
    font-family: var(--bb-font-mono);
    font-size: 11px;
    letter-spacing: 0.14em;
    text-transform: uppercase;
    color: var(--bb-tan, #c9a87c);
    margin-bottom: 6px;
  }
  .premium-title {
    font-family: var(--bb-font-display);
    font-weight: 800;
    font-size: 28px;
    letter-spacing: -0.02em;
    color: var(--bb-white);
    margin: 0 0 8px;
    background: linear-gradient(135deg, var(--bb-white) 0%, var(--bb-tan-pale) 100%);
    -webkit-background-clip: text;
    background-clip: text;
    -webkit-text-fill-color: transparent;
  }
  .premium-price {
    display: flex;
    align-items: baseline;
    gap: 3px;
    margin: 0 0 10px;
  }
  .premium-price .plan-amt {
    font-size: 1.6rem;
  }
  .premium-hint {
    font-family: var(--bb-font-body);
    font-size: 14.5px;
    line-height: 1.5;
    color: rgba(240, 236, 228, 0.7);
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
    font-size: 12px;
    color: rgba(240, 236, 228, 0.55);
    margin: 0;
    max-width: 40ch;
  }
  @media (min-width: 720px) {
    .premium-tiny-hint {
      text-align: right;
    }
  }

  /* ── Premium "includes" list ── */
  .premium-includes {
    margin-top: 22px;
  }
  .includes-h {
    font-family: var(--bb-font-mono);
    font-size: 11px;
    letter-spacing: 0.14em;
    text-transform: uppercase;
    color: var(--bb-muted);
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

  /* ── Premium Gift Card ── */
  :global(.premium-gift-card) {
    border-color: rgba(201, 168, 124, 0.2) !important;
    background: linear-gradient(180deg, rgba(201, 168, 124, 0.02) 0%, rgba(10, 10, 10, 0) 100%), var(--bb-card-bg, #111110) !important;
  }
  .gift-cta {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 18px;
  }
  .gift-cta :global(.btn) {
    flex-shrink: 0;
  }

  .form-error {
    color: #e5484d;
    font-size: 13px;
  }
  .form-error.center {
    text-align: center;
    margin-top: 14px;
  }

  /* ── Gift modal form ── */
  .gift-form {
    display: flex;
    flex-direction: column;
    gap: 16px;
  }
  .fld {
    display: flex;
    flex-direction: column;
    gap: 6px;
    position: relative;
  }
  .fld-label {
    font-family: var(--bb-font-mono);
    font-size: 11px;
    letter-spacing: 0.08em;
    text-transform: uppercase;
    color: var(--bb-muted);
  }
  .fld-label em {
    font-style: normal;
    text-transform: none;
    letter-spacing: 0;
    opacity: 0.7;
  }
  .fld-input {
    background: rgba(255, 255, 255, 0.04);
    border: 1px solid var(--bb-border, rgba(255, 255, 255, 0.1));
    border-radius: 8px 8px;
    color: var(--bb-white, #f0ece4);
    font-family: var(--bb-font-body);
    font-size: 13.5px;
    padding: 10px 14px;
    width: 100%;
  }
  .fld-input:focus {
    outline: none;
    border-color: var(--bb-tan, #c9a87c);
  }
  .fld-textarea {
    resize: vertical;
    min-height: 68px;
    line-height: 1.5;
  }
  .counter {
    align-self: flex-end;
    font-family: var(--bb-font-mono);
    font-size: 11px;
    color: var(--bb-muted);
  }
  .counter--full {
    color: var(--bb-tan-light);
  }

  /* ── Celebratory modal ── */
  .celebrate {
    text-align: center;
    padding: 4px 2px 0;
  }
  .celebrate-badge {
    width: 68px;
    height: 68px;
    margin: 0 auto 18px;
    display: flex;
    align-items: center;
    justify-content: center;
    border-radius: 50%;
    color: var(--bb-tan-light);
    background: radial-gradient(circle at 50% 40%, rgba(201, 168, 124, 0.28), rgba(201, 168, 124, 0.06));
    border: 1px solid rgba(201, 168, 124, 0.4);
    animation: pop 620ms var(--bb-ease-out-back, cubic-bezier(0.34, 1.56, 0.64, 1)) both;
  }
  .celebrate-badge--gift {
    color: var(--bb-green-light, #74c69d);
    background: radial-gradient(circle at 50% 40%, rgba(82, 183, 136, 0.28), rgba(82, 183, 136, 0.06));
    border-color: rgba(82, 183, 136, 0.4);
  }
  .celebrate-title {
    font-family: var(--bb-font-display);
    font-weight: 800;
    font-size: 24px;
    color: var(--bb-white);
    margin: 0 0 10px;
    letter-spacing: -0.01em;
    animation: rise 500ms var(--bb-ease-out-expo, cubic-bezier(0.16, 1, 0.3, 1)) both;
    animation-delay: 80ms;
  }
  .celebrate-body {
    font-family: var(--bb-font-body);
    font-size: 14px;
    line-height: 1.6;
    color: var(--bb-muted);
    margin: 0 auto;
    max-width: 42ch;
    animation: rise 500ms var(--bb-ease-out-expo, cubic-bezier(0.16, 1, 0.3, 1)) both;
    animation-delay: 140ms;
  }
  .celebrate-body strong {
    color: var(--bb-tan-light);
  }
  .celebrate-spinner {
    width: 22px;
    height: 22px;
    margin: 18px auto 0;
    border: 2px solid var(--bb-border-strong, rgba(201, 168, 124, 0.3));
    border-top-color: var(--bb-tan);
    border-radius: 50%;
    animation: spin 700ms linear infinite;
  }
  .celebrate-actions {
    justify-content: center;
    margin-top: 24px;
  }

  @keyframes pop {
    0% {
      transform: scale(0);
      opacity: 0;
    }
    60% {
      transform: scale(1.12);
    }
    100% {
      transform: scale(1);
      opacity: 1;
    }
  }
  @keyframes rise {
    from {
      transform: translateY(10px);
      opacity: 0;
    }
    to {
      transform: translateY(0);
      opacity: 1;
    }
  }
  @keyframes spin {
    to {
      transform: rotate(360deg);
    }
  }

  /* ── Confetti ── */
  .confetti-layer {
    position: fixed;
    inset: 0;
    z-index: 260;
    pointer-events: none;
    overflow: hidden;
  }
  .confetti-piece {
    position: absolute;
    top: 42%;
    left: 50%;
    border-radius: 8px;
    opacity: 0;
    animation: confetti var(--dur, 1200ms) var(--bb-ease-out-expo, cubic-bezier(0.16, 1, 0.3, 1)) var(--delay, 0ms) forwards;
  }
  @keyframes confetti {
    0% {
      transform: translate(-50%, -50%) rotate(0deg) scale(1);
      opacity: 1;
    }
    100% {
      transform: translate(calc(-50% + var(--tx)), calc(-50% + var(--ty))) rotate(var(--rot)) scale(0.9);
      opacity: 0;
    }
  }

  @media (prefers-reduced-motion: reduce) {
    .celebrate-badge,
    .celebrate-title,
    .celebrate-body,
    .celebrate-spinner,
    .confetti-piece {
      animation: none;
    }
    .celebrate-badge,
    .celebrate-title,
    .celebrate-body {
      opacity: 1;
      transform: none;
    }
  }

  @media (max-width: 760px) {
    .gift-cta {
      flex-direction: column;
    }
    .gift-cta :global(.btn) {
      width: 100%;
      justify-content: center;
    }
    .plan-buttons {
      flex-direction: column;
    }
  }
</style>
