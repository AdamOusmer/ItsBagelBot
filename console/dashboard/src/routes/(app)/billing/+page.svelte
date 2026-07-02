<script lang="ts">
  import { Icon, PageHead, Card, ConfirmDialog, toast } from '@bagel/shared';
  import { page } from '$app/state';
  import type { BillingState } from '$lib/server/services';

  let { data, form } = $props();

  const account = $derived(data.account as BillingState);
  const links = $derived(data.links as { checkoutUrl: string | null; cancelUrl: string | null });

  const isVip = $derived(account.status === 'vip');
  const isPaid = $derived(account.status === 'paid' || isVip);
  const staffGrant = $derived(account.status === 'paid' && account.source === 'admin');
  const tebexPaid = $derived(account.status === 'paid' && account.source === 'tebex');
  const paidUntil = $derived(account.expiresAt);
  const canSubscribe = $derived(!!links.checkoutUrl && !isPaid);
  const canCancel = $derived(!!links.cancelUrl && tebexPaid);

  const fmtDate = (iso?: string | null) =>
    iso
      ? new Date(iso).toLocaleDateString(undefined, { year: 'numeric', month: 'long', day: 'numeric' })
      : '';

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
  });

  let cancelOpen = $state(false);
  let cancelForm = $state<HTMLFormElement | null>(null);

  const statusLabel = $derived(isVip ? 'VIP' : isPaid ? 'Premium' : 'Free');
</script>

<section class="screen active">
  <PageHead eyebrow="Account" description="Your plan lives here. Tebex handles payment and subscription management.">Your <em>billing</em></PageHead>

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
        {:else if staffGrant}
          <p class="hint">
            Premium granted by the ItsBagelBot team{paidUntil ? `, active until ${fmtDate(paidUntil)}` : ''}.
            Nothing to pay and nothing will be charged while the grant is active.
          </p>
        {:else if tebexPaid}
          <p class="hint">
            Premium is active through Tebex{paidUntil ? ` until ${fmtDate(paidUntil)}` : ''}.
            Use Tebex to manage or cancel the subscription.
          </p>
        {:else if isPaid}
          <p class="hint">Premium is active{paidUntil ? ` until ${fmtDate(paidUntil)}` : ''}.</p>
        {:else}
          <p class="hint">You are on the free plan. Premium unlocks the priority lane and premium modules.</p>
        {/if}
      </div>

      <div class="plan-actions">
        {#if canSubscribe}
          <form method="POST" action="?/subscribe">
            <button class="btn primary" type="submit">
              <Icon name="heart" size={14} />
              Subscribe
            </button>
          </form>
          <p class="hint tiny">Secure payment opens on Tebex. Your plan updates here after the webhook lands.</p>
        {:else if canCancel}
          <button type="button" class="btn ghost danger" onclick={() => (cancelOpen = true)}>
            Cancel subscription
          </button>
          <p class="hint tiny">You will leave the dashboard and manage the subscription on Tebex.</p>
        {:else if !links.checkoutUrl && !isPaid}
          <p class="hint">Subscriptions aren't available right now.</p>
        {/if}
      </div>
    </div>
  </Card>
</section>

<!-- Cancel confirm: one confirmation before handing off to Tebex. -->
<ConfirmDialog
  open={cancelOpen}
  title="Open Tebex subscription management?"
  body={`You will leave ItsBagelBot and manage cancellation on Tebex. Premium stays active here until Tebex confirms the change${paidUntil ? ` or the period ends on ${fmtDate(paidUntil)}` : ''}.`}
  confirmLabel="Continue to Tebex"
  cancelLabel="Keep premium"
  danger
  onCancel={() => (cancelOpen = false)}
  onConfirm={() => {
    cancelForm?.requestSubmit();
    cancelOpen = false;
  }}
/>
<form method="POST" action="?/cancel" bind:this={cancelForm} hidden></form>

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

  @media (max-width: 760px) {
    .plan-top { flex-direction: column; }
    .plan-actions { align-items: stretch; width: 100%; }
    .plan-actions .hint { text-align: left; }
  }
</style>
