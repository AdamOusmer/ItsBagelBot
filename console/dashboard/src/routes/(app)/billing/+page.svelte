<script lang="ts">
  import { Icon, PageHead, Card, toast } from '@bagel/shared';
  import { page } from '$app/state';
  import { replaceState, invalidateAll } from '$app/navigation';
  import { onMount } from 'svelte';
  import type { BillingState } from '$lib/server/services';

  let { data, form } = $props();

  const account = $derived(data.account as BillingState);

  const isVip = $derived(account.status === 'vip');
  const isPaid = $derived(account.status === 'paid' || isVip);
  const staffGrant = $derived(account.status === 'paid' && account.source === 'admin');
  const tebexPaid = $derived(account.status === 'paid' && account.source === 'tebex');
  const paidUntil = $derived(account.expiresAt);
  // Basket minting happens server-side on click; the browser is then redirected
  // to Tebex-hosted checkout so payment collection never happens in our frame.
  const canSubscribe = $derived(!isPaid);
  // Rendered for every Tebex subscriber even when TEBEX_CANCEL_URL is not
  // configured: the ?/cancel action then answers 503 with a clear toast, which
  // beats silently hiding the only cancellation path.
  const canManage = $derived(tebexPaid);

  let launching = $state(false);
  let subscribeForm = $state<HTMLFormElement | null>(null);
  let giftLaunching = $state(false);
  let giftRecipient = $state('');

  // Set when the browser returns from hosted checkout with ?checkout=complete.
  // The entitlement lands out-of-band (Tebex webhook -> users service), so the
  // page thanks the buyer immediately and polls the billing state until the
  // plan flips to paid instead of asking for manual reloads.
  let justPaid = $state(false);
  let activationSlow = $state(false);

  // Auto-open checkout when the pricing page sent the visitor here with
  // ?subscribe=1 (possibly via the login flow). One shot: the param is
  // stripped so a refresh does not re-launch. The form performs a native
  // top-level redirect to Tebex-hosted checkout.
  onMount(() => {
    if (!data.autostart) return;
    const url = new URL(window.location.href);
    url.searchParams.delete('subscribe');
    replaceState(url, {});
    if (canSubscribe && !launching) subscribeForm?.requestSubmit();
  });

  // Returning from hosted checkout with a completed payment: thank the buyer,
  // strip the param (refreshes must not re-trigger), then chase the webhook by
  // re-running the load every few seconds until the plan flips to paid. The
  // server cache is event-invalidated when the entitlement lands, so a poll
  // hit after that is fresh. Bounded: after ~2 minutes stop and say so.
  onMount(() => {
    if (page.url.searchParams.get('checkout') !== 'complete') return;
    justPaid = true;
    const url = new URL(window.location.href);
    url.searchParams.delete('checkout');
    replaceState(url, {});
    toast('ok', 'Payment received — thank you!');

    let tries = 0;
    const timer = setInterval(() => {
      if (isPaid) {
        clearInterval(timer);
        return;
      }
      if (++tries > 40) {
        activationSlow = true;
        clearInterval(timer);
        return;
      }
      void invalidateAll();
    }, 3000);
    return () => clearInterval(timer);
  });

  const fmtDate = (iso?: string | null) =>
    iso
      ? new Date(iso).toLocaleDateString(undefined, { year: 'numeric', month: 'long', day: 'numeric' })
      : '';

  // A cancelled checkout only needs the one toast (the complete case is
  // handled in onMount above, where the polling lifecycle lives).
  let checkoutToasted = false;
  $effect(() => {
    if (checkoutToasted) return;
    if (page.url.searchParams.get('checkout') !== 'cancelled') return;
    checkoutToasted = true;
    toast('err', 'Checkout cancelled. No charge was made.');
  });

  // svelte-ignore state_referenced_locally
  let lastForm: unknown = form;
  $effect(() => {
    if (form === lastForm) return;
    lastForm = form;
    if (!form) return;
    // A form result means the action did not redirect to Tebex — re-enable the
    // buttons instead of leaving them stuck on "Opening checkout…".
    launching = false;
    giftLaunching = false;
    if (form.error) toast('err', String(form.error));
  });

  const statusLabel = $derived(isVip ? 'VIP' : isPaid ? 'Premium' : 'Free');
</script>

<section class="screen active">
  <PageHead eyebrow="Account" description="Your plan lives here. Tebex handles payment and subscription management.">Your <em>billing</em></PageHead>

  {#if data.degraded}
    <Card class="billing-card">
      <p class="hint">Billing data is temporarily unavailable. What you see may be incomplete — try again shortly.</p>
    </Card>
  {/if}

  {#if justPaid}
    <Card class="billing-card">
      <div class="thanks">
        <Icon name="heart" size={18} />
        {#if isPaid}
          <div>
            <strong>Thank you! Premium is active.</strong>
            <p class="hint">Your support keeps ItsBagelBot running. Everything premium is unlocked now.</p>
          </div>
        {:else if activationSlow}
          <div>
            <strong>Payment received — activation is taking longer than usual.</strong>
            <p class="hint">Tebex confirmed the payment. Give it a minute and refresh; if it still shows Free, contact us.</p>
          </div>
        {:else}
          <div>
            <strong>Thank you! Payment received.</strong>
            <p class="hint">Activating your premium — this page updates by itself, usually within a minute.</p>
          </div>
        {/if}
      </div>
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
            {account.cancelPending ? 'Cancellation is scheduled' : 'Premium is active through Tebex'}{paidUntil ? ` until ${fmtDate(paidUntil)}` : ''}.
            Use Tebex-hosted subscription management to review payments or change the subscription.
          </p>
        {:else if isPaid}
          <p class="hint">Premium is active{paidUntil ? ` until ${fmtDate(paidUntil)}` : ''}.</p>
        {:else}
          <p class="hint">You are on the free plan. Premium unlocks the priority lane and premium modules.</p>
        {/if}
      </div>

      <div class="plan-actions">
        {#if canSubscribe}
          <!-- Two ways to pay, both one click: an auto-renewing monthly
               subscription or a single month with no renewal. The plan rides
               in a hidden input (never a button value: submit-time state
               changes can drop the submitter from the form data). -->
          <div class="plan-buttons">
            <form
              method="POST"
              action="?/subscribe"
              bind:this={subscribeForm}
              onsubmit={() => {
                launching = true;
              }}
            >
              <input type="hidden" name="plan" value="monthly" />
              <button class="btn primary" type="submit" disabled={launching}>
                <Icon name="heart" size={14} />
                {launching ? 'Opening checkout…' : 'Subscribe monthly'}
              </button>
            </form>
            <form
              method="POST"
              action="?/subscribe"
              onsubmit={() => {
                launching = true;
              }}
            >
              <input type="hidden" name="plan" value="once" />
              <button class="btn ghost" type="submit" disabled={launching}>
                {launching ? 'Opening checkout…' : 'Buy one month'}
              </button>
            </form>
          </div>
          <p class="hint tiny">Monthly renews automatically until cancelled. One month is a single charge, no renewal.</p>
          <p class="hint tiny">Redirects to Tebex’s secure hosted checkout. Your plan updates after the payment lands.</p>
        {:else if canManage}
          <form method="POST" action="?/cancel">
            <button type="submit" class="btn ghost">Manage subscription</button>
          </form>
          <form method="POST" action="?/cancel">
            <button type="submit" class="btn ghost danger">Cancel subscription</button>
          </form>
          <p class="hint tiny">Both redirect to Tebex-hosted subscription management, where cancellation is one click.</p>
        {/if}
        {#if form?.error && !form?.gift}
          <p class="hint form-error">{form.error}</p>
        {/if}
      </div>
    </div>
  </Card>

  <!-- GIFT: pay for someone else's premium. Open regardless of your own plan;
       the recipient must be a registered, non-premium ItsBagelBot user (the
       transactions service vets this and its errors surface as toasts). -->
  <Card class="billing-card">
    <div class="gift-top">
      <div>
        <h2>Gift premium</h2>
        <p class="hint">
          Pay for another streamer's premium. They need an ItsBagelBot account, and they get a
          notification the moment your payment lands.
        </p>
      </div>
      <div class="gift-side">
        <form
          class="gift-form"
          method="POST"
          action="?/gift"
          onsubmit={() => {
            giftLaunching = true;
          }}
        >
          <!-- readonly, never disabled: the submit handler flips giftLaunching
               before the browser serializes the form, and disabled fields are
               dropped from form data — the server would always see an empty
               recipient. -->
          <input
            class="gift-input"
            type="text"
            name="recipient"
            placeholder="Twitch username"
            autocomplete="off"
            spellcheck="false"
            maxlength="26"
            bind:value={giftRecipient}
            readonly={giftLaunching}
          />
          <button class="btn primary" type="submit" disabled={giftLaunching || !giftRecipient.trim()}>
            <Icon name="heart" size={14} />
            {giftLaunching ? 'Opening checkout…' : 'Gift premium'}
          </button>
        </form>
        <!-- Inline so the reason survives the full-page re-render of a failed
             plain-form POST; the toast alone is easy to miss. -->
        {#if form?.gift && form?.error}
          <p class="hint form-error">{form.error}</p>
        {/if}
      </div>
    </div>
  </Card>
</section>

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
  .plan-buttons {
    display: flex;
    gap: 10px;
    align-items: center;
  }
  .plan-actions .hint { text-align: right; }

  .gift-top {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 18px;
  }
  .gift-form {
    display: flex;
    gap: 10px;
    flex-shrink: 0;
    align-items: center;
  }
  .thanks {
    display: flex;
    align-items: flex-start;
    gap: 12px;
  }
  .thanks strong {
    color: var(--bb-white);
    font-size: 15px;
  }
  .thanks .hint { margin-top: 4px; }

  .btn.danger {
    color: #e5484d;
    border-color: rgba(229, 72, 77, 0.4);
  }

  .gift-side {
    display: flex;
    flex-direction: column;
    align-items: flex-end;
    gap: 8px;
    flex-shrink: 0;
  }
  .form-error {
    color: #e5484d;
    max-width: 34ch;
    text-align: right;
  }

  .gift-input {
    background: rgba(255, 255, 255, 0.04);
    border: 1px solid var(--bb-border, rgba(255, 255, 255, 0.1));
    border-radius: var(--bb-radius-md, 10px);
    color: var(--bb-white, #f0ece4);
    font-family: var(--bb-font-body);
    font-size: 13.5px;
    padding: 10px 14px;
    width: 200px;
  }
  .gift-input:focus {
    outline: none;
    border-color: var(--bb-tan, #c9a87c);
  }

  @media (max-width: 760px) {
    .plan-top { flex-direction: column; }
    .plan-actions { align-items: stretch; width: 100%; }
    .plan-actions .hint { text-align: left; }
    .plan-buttons { flex-direction: column; align-items: stretch; }
    .plan-buttons form { width: 100%; }
    .plan-buttons .btn { width: 100%; justify-content: center; }
    .gift-top { flex-direction: column; }
    .gift-side { align-items: stretch; width: 100%; }
    .gift-form { width: 100%; flex-wrap: wrap; }
    .gift-input { flex: 1; min-width: 0; }
    .form-error { text-align: left; max-width: none; }
  }
</style>
