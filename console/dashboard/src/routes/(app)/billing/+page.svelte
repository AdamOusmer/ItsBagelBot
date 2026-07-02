<script lang="ts">
  import { Icon, PageHead, Card, ConfirmDialog, EmptyState, toast } from '@bagel/shared';
  import { page } from '$app/state';
  import { enhance } from '$app/forms';
  import type { BillingState, BillingSummary } from '$lib/server/services';

  let { data, form } = $props();

  const account = $derived(data.account as BillingState);
  const billing = $derived(data.billing as BillingSummary);
  const sub = $derived(billing.subscription);

  const isVip = $derived(account.status === 'vip');
  const isPaid = $derived(account.status === 'paid' || isVip);
  const staffGrant = $derived(account.status === 'paid' && account.source === 'admin');
  const pendingCancel = $derived(sub?.status === 'pending_cancel');
  const subActive = $derived(
    sub != null && ['active', 'overdue', 'pending_cancel', 'pending_downgrade'].includes(sub.status)
  );
  // The date the current paid period runs to: the users DB entitlement is
  // authoritative (covers staff grants too), the Tebex row is the fallback.
  const paidUntil = $derived(account.expiresAt ?? sub?.current_period_end ?? null);
  // Subscribe only when checkout is configured AND the user is not already
  // premium in any form (Tebex subscription, staff grant, VIP) — an active
  // period must never lead to a second charge.
  const canSubscribe = $derived(billing.canCheckout && !isPaid && !subActive);
  const canCancel = $derived(billing.canCancel && subActive && !pendingCancel);

  const fmtDate = (iso?: string | null) =>
    iso
      ? new Date(iso).toLocaleDateString(undefined, { year: 'numeric', month: 'long', day: 'numeric' })
      : '';
  const fmtAmount = (cents?: number, currency?: string) =>
    cents == null
      ? '—'
      : new Intl.NumberFormat(undefined, { style: 'currency', currency: currency || 'USD' }).format(cents / 100);

  // Returning from Tebex: ?checkout=complete|cancelled (set on the basket's
  // return URLs). The entitlement itself lands via webhook moments later.
  let checkoutToasted = false;
  $effect(() => {
    if (checkoutToasted) return;
    const c = page.url.searchParams.get('checkout');
    if (!c) return;
    checkoutToasted = true;
    if (c === 'complete') toast('ok', 'Payment received — your plan activates within a minute.');
    else if (c === 'cancelled') toast('err', 'Checkout cancelled. No charge was made.');
  });

  // svelte-ignore state_referenced_locally
  let lastForm: unknown = form;
  $effect(() => {
    if (form === lastForm) return;
    lastForm = form;
    if (!form) return;
    if (form.error) toast('err', String(form.error));
    else if (form.ok && form.action === 'cancelled')
      toast('ok', 'Subscription cancelled. You keep premium until the period ends.');
  });

  let cancelOpen = $state(false);
  let cancelForm = $state<HTMLFormElement | null>(null);
  let subscribing = $state(false);

  const statusLabel = $derived(
    isVip
      ? 'VIP'
      : pendingCancel
        ? 'Premium — cancelling'
        : isPaid
          ? 'Premium'
          : 'Free'
  );
</script>

<section class="screen active">
  <PageHead eyebrow="Account" description="Your plan, payments, and subscription controls. Cancel any time — access runs to the end of the paid period.">Your <em>billing</em></PageHead>

  {#if data.degraded}
    <Card class="billing-card">
      <p class="hint">Billing data is temporarily unavailable. What you see may be incomplete — try again shortly.</p>
    </Card>
  {/if}

  <!-- PLAN -->
  <Card class="billing-card">
    <div class="plan-top">
      <div>
        <h2>Current plan</h2>
        <div class="plan-name {isPaid ? 'premium' : ''}">
          <Icon name={isPaid ? 'heart' : 'overview'} size={16} />
          {statusLabel}
        </div>
        {#if isVip}
          <p class="hint">VIP is permanent. It never expires and there is nothing to pay.</p>
        {:else if pendingCancel}
          <p class="hint">
            Cancellation requested{sub?.cancel_requested_at ? ` on ${fmtDate(sub.cancel_requested_at)}` : ''}.
            Premium stays active until <b>{fmtDate(paidUntil) || 'the end of the paid period'}</b>, then your plan returns to Free.
          </p>
        {:else if staffGrant}
          <p class="hint">
            Premium granted by the ItsBagelBot team{paidUntil ? `, active until ${fmtDate(paidUntil)}` : ''}.
            Nothing to pay and nothing will be charged while the grant is active.
          </p>
        {:else if isPaid && subActive}
          <p class="hint">
            Renews {paidUntil ? `on ${fmtDate(paidUntil)}` : 'automatically'}.
            Cancel any time below.
          </p>
        {:else if isPaid}
          <p class="hint">Premium is active{paidUntil ? ` until ${fmtDate(paidUntil)}` : ''}.</p>
        {:else}
          <p class="hint">You are on the free plan. Premium unlocks the priority lane and premium modules.</p>
        {/if}
      </div>

      <div class="plan-actions">
        {#if canSubscribe}
          <form
            method="POST"
            action="?/subscribe"
            use:enhance={() => {
              subscribing = true;
              return async ({ update }) => {
                await update();
                subscribing = false;
              };
            }}
          >
            <button class="btn primary" type="submit" disabled={subscribing}>
              <Icon name="heart" size={14} />
              {subscribing ? 'Opening checkout…' : 'Subscribe'}
            </button>
          </form>
          <p class="hint tiny">Secure checkout by Tebex. You can cancel in one click, right here.</p>
        {:else if canCancel}
          <button type="button" class="btn ghost danger" onclick={() => (cancelOpen = true)}>
            Cancel subscription
          </button>
        {:else if !billing.canCheckout && !isPaid}
          <p class="hint">Subscriptions aren't available right now.</p>
        {/if}
      </div>
    </div>
  </Card>

  <!-- PAYMENTS -->
  <Card class="billing-card">
    <h2>Payment history</h2>
    {#if billing.payments.length === 0}
      <EmptyState icon="clock" title="No payments yet" body="Your Tebex payments will show up here." />
    {:else}
      <div class="payments">
        {#each billing.payments as p (p.id)}
          <div class="payment">
            <span class="p-date">{fmtDate(p.created_at)}</span>
            <span class="p-amount">{fmtAmount(p.amount_cents, p.currency)}</span>
            <span class="p-status {p.status}">{p.status}</span>
            <code class="p-id">{p.id}</code>
          </div>
        {/each}
      </div>
    {/if}
  </Card>
</section>

<!-- Cancel confirm: one click + one confirm, nothing else. -->
<ConfirmDialog
  open={cancelOpen}
  title="Cancel your subscription?"
  body={`No further payments will be taken. Premium stays active until ${fmtDate(paidUntil) || 'the end of the paid period'}, and you can resubscribe any time.`}
  confirmLabel="Cancel subscription"
  cancelLabel="Keep premium"
  danger
  onCancel={() => (cancelOpen = false)}
  onConfirm={() => {
    cancelForm?.requestSubmit();
    cancelOpen = false;
  }}
/>
<form method="POST" action="?/cancel" use:enhance bind:this={cancelForm} hidden></form>

<style>
  :global(.billing-card) {
    margin-top: 18px;
  }
  h2 { margin: 0 0 6px; font-size: 16px; }
  .hint { color: var(--bb-muted, #998f82); font-size: 13px; margin: 6px 0 0; max-width: 52ch; }
  .hint.tiny { font-size: 12px; }

  .plan-top {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 18px;
  }
  .plan-name {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    margin-top: 6px;
    font-family: var(--bb-font-display);
    font-weight: 700;
    font-size: 20px;
    color: var(--bb-white);
  }
  .plan-name.premium { color: var(--bb-tan-light, #c9a87c); }

  .plan-actions {
    display: flex;
    flex-direction: column;
    align-items: flex-end;
    gap: 8px;
    flex-shrink: 0;
  }
  .plan-actions .hint { text-align: right; }
  .btn.danger { color: #e08f8f; }

  .payments { display: flex; flex-direction: column; gap: 8px; margin-top: 10px; }
  .payment {
    display: grid;
    grid-template-columns: 1fr auto auto auto;
    align-items: center;
    gap: 12px;
    padding: 10px 12px;
    border: 1px solid var(--glass-border);
    border-radius: var(--bb-radius-md, 10px);
    background: rgba(255, 255, 255, 0.02);
    font-size: 13px;
  }
  .p-date { color: var(--bb-white); }
  .p-amount { font-family: var(--bb-font-mono); color: var(--bb-tan-light); }
  .p-status {
    font-family: var(--bb-font-mono);
    font-size: 11px;
    text-transform: uppercase;
    letter-spacing: 0.03em;
    padding: 2px 10px;
    border-radius: 999px;
    border: 1px solid rgba(82, 183, 136, 0.3);
    color: var(--bb-green-glow, #52b788);
  }
  .p-status.refunded, .p-status.disputed, .p-status.declined {
    border-color: rgba(220, 120, 120, 0.35);
    color: #e08f8f;
  }
  .p-id { font-size: 11px; color: var(--bb-muted); }

  @media (max-width: 760px) {
    .plan-top { flex-direction: column; }
    .plan-actions { align-items: stretch; width: 100%; }
    .plan-actions .hint { text-align: left; }
    .payment { grid-template-columns: 1fr auto; row-gap: 4px; }
    .p-id { display: none; }
  }
</style>
