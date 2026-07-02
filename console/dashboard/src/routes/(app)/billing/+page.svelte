<script lang="ts">
  import { Icon, PageHead, Card, toast } from '@bagel/shared';
  import { page } from '$app/state';
  import { applyAction, deserialize } from '$app/forms';
  import { invalidateAll, replaceState } from '$app/navigation';
  import { onDestroy, onMount } from 'svelte';
  import type { BillingState } from '$lib/server/services';

  let { data, form } = $props();

  const account = $derived(data.account as BillingState);
  const links = $derived(data.links as { checkoutUrl: string | null; cancelUrl: string | null; portalToken: string | null });

  const isVip = $derived(account.status === 'vip');
  const isPaid = $derived(account.status === 'paid' || isVip);
  const staffGrant = $derived(account.status === 'paid' && account.source === 'admin');
  const tebexPaid = $derived(account.status === 'paid' && account.source === 'tebex');
  const paidUntil = $derived(account.expiresAt);
  // Basket minting happens server-side on click, so subscribing only needs a
  // free plan; the static checkoutUrl is just the hosted fallback.
  const canSubscribe = $derived(!isPaid);
  const canManage = $derived(tebexPaid && (!!links.portalToken || !!links.cancelUrl));

  // ── Tebex.js embedded checkout ──
  // The official script (js.tebex.io, loaded in <svelte:head>) exposes
  // window.Tebex. The subscribe action returns a basket ident; init + launch
  // opens the payment overlay without leaving the dashboard.
  type TebexGlobal = {
    checkout: {
      init: (opts: {
        ident?: string;
        theme?: 'light' | 'dark' | 'auto' | 'default';
        closeOnPaymentComplete?: boolean;
        closeOnClickOutside?: boolean;
        closeOnEsc?: boolean;
        launchTimeout?: number;
      }) => void;
      launch: (ident?: () => Promise<string>) => Promise<void>;
      close: () => Promise<void>;
      on: (event: 'payment:complete' | 'payment:error' | 'close', cb: () => void) => () => void;
    };
    portal: {
      init: (opts: { token: string; theme?: 'light' | 'dark' | 'auto' }) => void;
      launch: () => Promise<void>;
    };
  };
  const tebexGlobal = () => (window as unknown as { Tebex?: TebexGlobal }).Tebex;

  let launching = $state(false);
  let subscribeForm = $state<HTMLFormElement | null>(null);
  let giftLaunching = $state(false);
  let giftRecipient = $state('');

  let checkoutUnsubscribers: Array<() => void> = [];
  let checkoutFallback: string | null = null;

  onDestroy(() => checkoutUnsubscribers.splice(0).forEach((unsubscribe) => unsubscribe()));

  async function submitAction(form: HTMLFormElement): Promise<import('@sveltejs/kit').ActionResult> {
    const body = new URLSearchParams();
    new FormData(form).forEach((value, key) => {
      if (typeof value === 'string') body.append(key, value);
    });
    const response = await fetch(form.action, {
      method: 'POST',
      headers: {
        accept: 'application/json',
        'content-type': 'application/x-www-form-urlencoded',
        'x-sveltekit-action': 'true'
      },
      cache: 'no-store',
      body
    });
    const result = deserialize(await response.text());
    if (result.type === 'error') result.status = response.status;
    return result;
  }

  async function createBasket(form: HTMLFormElement, setBusy: (busy: boolean) => void): Promise<string> {
    const result = await submitAction(form);
    if (result.type === 'success' && result.data?.ident) {
      checkoutFallback = result.data.checkoutUrl ? String(result.data.checkoutUrl) : null;
      if (result.data.recipientLogin) {
        toast('ok', `Gifting premium to ${String(result.data.recipientLogin)} — complete the payment to send it.`);
      }
      return String(result.data.ident);
    }
    if (result.type === 'redirect') {
      window.location.href = result.location;
      throw new Error('redirecting to hosted checkout');
    }
    setBusy(false);
    await applyAction(result);
    throw new Error('basket creation failed');
  }

  function bindCheckoutEvents(tebex: TebexGlobal, setBusy: (busy: boolean) => void) {
    checkoutUnsubscribers.splice(0).forEach((unsubscribe) => unsubscribe());
    checkoutUnsubscribers = [
      tebex.checkout.on('payment:complete', () => {
        toast('ok', 'Payment received — confirming your plan now.');
        void pollForEntitlement();
      }),
      tebex.checkout.on('payment:error', () => {
        toast('err', 'Payment did not go through. No charge was made.');
      }),
      tebex.checkout.on('close', () => setBusy(false))
    ];
  }

  async function pollForEntitlement() {
    for (const delay of [1000, 2000, 3000, 5000, 8000]) {
      await new Promise((resolve) => setTimeout(resolve, delay));
      await invalidateAll();
      if (account.status !== 'free') return;
    }
  }

  async function launchCheckout(form: HTMLFormElement, setBusy: (busy: boolean) => void) {
    setBusy(true);
    // Read synchronously inside the submit gesture. Tebex v1.11 can then open
    // its mobile window before awaiting the basket callback.
    const tebex = tebexGlobal();
    if (!tebex) {
      try {
        await createBasket(form, setBusy);
        if (checkoutFallback) window.location.href = checkoutFallback;
      } catch {
        // createBasket has already applied the form error or started redirecting.
      }
      return;
    }

    checkoutFallback = null;
    bindCheckoutEvents(tebex, setBusy);
    tebex.checkout.init({
      theme: 'dark',
      closeOnPaymentComplete: true,
      closeOnClickOutside: false,
      closeOnEsc: true,
      launchTimeout: 16000
    });
    try {
      // Tebex.js v1.11 opens the mobile window synchronously, then invokes this
      // callback while its spinner is visible. Basket creation can therefore be
      // asynchronous without browsers blocking the checkout popup.
      await tebex.checkout.launch(() => createBasket(form, setBusy));
    } catch {
      if (checkoutFallback) window.location.href = checkoutFallback;
      else setBusy(false);
    }
  }

  async function launchPortal() {
    const tebex = tebexGlobal();
    if (!tebex || !links.portalToken) {
      cancelForm?.requestSubmit();
      return;
    }
    tebex.portal.init({ token: links.portalToken, theme: 'dark' });
    try {
      await tebex.portal.launch();
    } catch {
      cancelForm?.requestSubmit();
    }
  }

  // Auto-open checkout when the pricing page sent the visitor here with
  // ?subscribe=1 (possibly via the login flow). One shot: the param is
  // stripped so a refresh does not re-launch.
  onMount(() => {
    if (!data.autostart) return;
    const url = new URL(window.location.href);
    url.searchParams.delete('subscribe');
    replaceState(url, {});
    if (canSubscribe && !launching) subscribeForm?.requestSubmit();
  });

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

  let cancelForm = $state<HTMLFormElement | null>(null);

  const statusLabel = $derived(isVip ? 'VIP' : isPaid ? 'Premium' : 'Free');
</script>

<svelte:head>
  <!-- Official Tebex.js — embedded checkout overlay (allowed by CSP script-src). -->
  <script src="https://js.tebex.io/v/1.js" defer></script>
</svelte:head>

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
            {account.cancelPending ? 'Cancellation is scheduled' : 'Premium is active through Tebex'}{paidUntil ? ` until ${fmtDate(paidUntil)}` : ''}.
            Use the Tebex Payment Portal to review payments or change the subscription.
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
            bind:this={subscribeForm}
            onsubmit={(event) => {
              event.preventDefault();
              void launchCheckout(event.currentTarget, (busy) => (launching = busy));
            }}
          >
            <button class="btn primary" type="submit" disabled={launching}>
              <Icon name="heart" size={14} />
              {launching ? 'Opening checkout…' : 'Subscribe'}
            </button>
          </form>
          <p class="hint tiny">Secure payment by Tebex, right here. Your plan updates after the payment lands.</p>
        {:else if canManage}
          <button type="button" class="btn ghost" onclick={() => void launchPortal()}>
            Manage subscription
          </button>
          <p class="hint tiny">Opens Tebex’s secure Payment Portal.</p>
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
      <form
        class="gift-form"
        method="POST"
        action="?/gift"
        onsubmit={(event) => {
          event.preventDefault();
          void launchCheckout(event.currentTarget, (busy) => (giftLaunching = busy));
        }}
      >
        <input
          class="gift-input"
          type="text"
          name="recipient"
          placeholder="Twitch username"
          autocomplete="off"
          spellcheck="false"
          maxlength="26"
          bind:value={giftRecipient}
          disabled={giftLaunching}
        />
        <button class="btn primary" type="submit" disabled={giftLaunching || !giftRecipient.trim()}>
          <Icon name="heart" size={14} />
          {giftLaunching ? 'Opening checkout…' : 'Gift premium'}
        </button>
      </form>
    </div>
  </Card>
</section>

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
    .gift-top { flex-direction: column; }
    .gift-form { width: 100%; flex-wrap: wrap; }
    .gift-input { flex: 1; min-width: 0; }
  }
</style>
