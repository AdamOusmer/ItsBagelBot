<script lang="ts">
  import { Icon, PageHead, Card, ConfirmDialog, toast } from '@bagel/shared';
  import { page } from '$app/state';
  import { enhance } from '$app/forms';
  import { invalidateAll, replaceState } from '$app/navigation';
  import { onMount } from 'svelte';
  import type { BillingState } from '$lib/server/services';

  let { data, form } = $props();

  const account = $derived(data.account as BillingState);
  const links = $derived(data.links as { checkoutUrl: string | null; cancelUrl: string | null });

  const isVip = $derived(account.status === 'vip');
  const isPaid = $derived(account.status === 'paid' || isVip);
  const staffGrant = $derived(account.status === 'paid' && account.source === 'admin');
  const tebexPaid = $derived(account.status === 'paid' && account.source === 'tebex');
  const paidUntil = $derived(account.expiresAt);
  // Basket minting happens server-side on click, so subscribing only needs a
  // free plan; the static checkoutUrl is just the hosted fallback.
  const canSubscribe = $derived(!isPaid);
  const canCancel = $derived(!!links.cancelUrl && tebexPaid);

  // ── Tebex.js embedded checkout ──
  // The official script (js.tebex.io, loaded in <svelte:head>) exposes
  // window.Tebex. The subscribe action returns a basket ident; init + launch
  // opens the payment overlay without leaving the dashboard.
  type TebexGlobal = {
    checkout: {
      init: (opts: { ident: string; theme?: 'light' | 'dark' }) => void;
      launch: () => void;
      close: () => void;
      on: (event: 'payment:complete' | 'payment:error' | 'close', cb: () => void) => void;
    };
  };
  const tebexGlobal = () => (window as unknown as { Tebex?: TebexGlobal }).Tebex;

  let launching = $state(false);
  let subscribeForm = $state<HTMLFormElement | null>(null);
  let giftLaunching = $state(false);
  let giftRecipient = $state('');

  // Shared enhance handler for both checkout forms (subscribe + gift): a
  // successful action returns a basket ident to launch, the hosted-checkout
  // fallback arrives as an external redirect, anything else releases the
  // button and lets the form-error toast handle it.
  function checkoutEnhance(setBusy: (busy: boolean) => void) {
    return () => {
      setBusy(true);
      return async ({
        result,
        update
      }: {
        result: import('@sveltejs/kit').ActionResult;
        update: () => Promise<void>;
      }) => {
        if (result.type === 'success' && result.data?.ident) {
          if (result.data.recipientLogin) {
            toast('ok', `Gifting premium to ${String(result.data.recipientLogin)} — complete the payment to send it.`);
          }
          await launchCheckout(
            String(result.data.ident),
            result.data.checkoutUrl ? String(result.data.checkoutUrl) : null,
            setBusy
          );
          return;
        }
        if (result.type === 'redirect') {
          // Hosted-checkout fallback is an external URL; goto() cannot take
          // it, so navigate directly.
          window.location.href = result.location;
          return;
        }
        setBusy(false);
        await update();
      };
    };
  }

  function waitForTebex(timeoutMs = 6000): Promise<TebexGlobal | null> {
    return new Promise((resolve) => {
      const started = Date.now();
      const poll = () => {
        const t = tebexGlobal();
        if (t) return resolve(t);
        if (Date.now() - started > timeoutMs) return resolve(null);
        setTimeout(poll, 100);
      };
      poll();
    });
  }

  async function launchCheckout(ident: string, fallbackUrl: string | null, setBusy: (busy: boolean) => void) {
    if (fallbackUrl) {
      window.location.href = fallbackUrl;
      return;
    }

    const tebex = await waitForTebex(1500);
    if (!tebex) {
      setBusy(false);
      toast('err', 'Could not open checkout. Please try again.');
      return;
    }

    tebex.checkout.init({ ident, theme: 'dark' });
    tebex.checkout.on('payment:complete', () => {
      toast('ok', 'Payment received — it activates within a minute.');
      tebex.checkout.close();
      // The entitlement lands via the Tebex webhook; refetch shortly after.
      setTimeout(() => void invalidateAll(), 1500);
    });
    tebex.checkout.on('payment:error', () => {
      toast('err', 'Payment did not go through. No charge was made.');
    });
    tebex.checkout.on('close', () => {
      setBusy(false);
    });
    tebex.checkout.launch();
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

  let cancelOpen = $state(false);
  let cancelForm = $state<HTMLFormElement | null>(null);

  const statusLabel = $derived(isVip ? 'VIP' : isPaid ? 'Premium' : 'Free');
</script>

<svelte:head>
  <!-- Official Tebex.js — embedded checkout overlay (allowed by CSP script-src). -->
  <script src="https://js.tebex.io/v/1.js" async></script>
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
          <form
            method="POST"
            action="?/subscribe"
            bind:this={subscribeForm}
            use:enhance={checkoutEnhance((busy) => (launching = busy))}
          >
            <button class="btn primary" type="submit" disabled={launching}>
              <Icon name="heart" size={14} />
              {launching ? 'Opening checkout…' : 'Subscribe'}
            </button>
          </form>
          <p class="hint tiny">Secure payment by Tebex, right here. Your plan updates after the payment lands.</p>
        {:else if canCancel}
          <button type="button" class="btn ghost danger" onclick={() => (cancelOpen = true)}>
            Cancel subscription
          </button>
          <p class="hint tiny">You will leave the dashboard and manage the subscription on Tebex.</p>
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
        use:enhance={checkoutEnhance((busy) => (giftLaunching = busy))}
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
