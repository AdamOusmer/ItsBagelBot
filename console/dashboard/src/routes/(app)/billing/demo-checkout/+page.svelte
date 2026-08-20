<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  // Fake Tebex-hosted checkout, DEMO=1 only. Real checkout redirects the
  // browser out to Tebex; this page is the demo's own stand-in for that trip,
  // styled to match the billing page it launches from and returns to.
  import { AuroraBg, LightField, PageHead, Card, Button, Icon } from '@bagel/shared';

  let { data } = $props();

  const PRICE = 7;

  const planLabel = $derived(data.plan === 'monthly' ? 'Premium, billed monthly' : 'Premium, one month');
  const isGift = $derived(data.kind === 'gift');
</script>

<AuroraBg />
<div class="starfield" aria-hidden="true"><LightField warmth={0.7} /></div>

<section class="screen active">
  <PageHead eyebrow="Demo checkout" description="No payment is taken here. This screen only stands in for Tebex-hosted checkout.">
    Demo <em>checkout</em>
  </PageHead>

  <div class="demo-banner" role="status">
    <Icon name="ban" size={13} />
    <span>This is a demo checkout. No card is charged, no email goes out, and nothing leaves this dev server.</span>
  </div>

  <Card class="checkout-card">
    <div class="row">
      <span class="row-label">Plan</span>
      <span class="row-value">{planLabel}</span>
    </div>
    {#if isGift}
      <div class="row">
        <span class="row-label">Gift to</span>
        <span class="row-value">@{data.recipient}</span>
      </div>
    {/if}
    <div class="row row-total">
      <span class="row-label">Total</span>
      <span class="row-value">${PRICE}.00 CAD</span>
    </div>

    <form method="POST" action="?/pay" class="pay-form">
      <input type="hidden" name="plan" value={data.plan} />
      <input type="hidden" name="kind" value={data.kind} />
      {#if isGift}<input type="hidden" name="recipient" value={data.recipient} />{/if}
      <Button type="submit" variant="primary" icon="heart">Pay ${PRICE}.00</Button>
    </form>
    <a class="cancel-link" href="/billing">Cancel, go back to billing</a>
  </Card>
</section>

<style>
  .starfield {
    position: fixed;
    inset: 0;
    z-index: 0;
    pointer-events: none;
  }
  .screen {
    position: relative;
    z-index: 1;
    max-width: 560px;
    margin: 0 auto;
    padding: 0 16px;
  }

  .demo-banner {
    display: flex;
    align-items: center;
    gap: 9px;
    margin: 0 0 20px;
    padding: 12px 16px;
    border: 1px solid var(--bb-status-error-border, #b05a46);
    background: var(--bb-status-error-bg, #2a1310);
    color: var(--bb-status-error-fg, #f0b0a4);
    border-radius: 10px;
    font-family: var(--bb-font-mono);
    font-size: 12px;
    line-height: 1.5;
  }
  .demo-banner :global(svg) {
    flex-shrink: 0;
  }

  :global(.checkout-card) {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }

  .row {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    padding: 12px 0;
    border-bottom: 1px solid var(--bb-border);
  }
  .row-total {
    border-bottom: none;
  }
  .row-label {
    font-family: var(--bb-font-mono);
    font-size: 11px;
    letter-spacing: 0.1em;
    text-transform: uppercase;
    color: var(--bb-muted);
  }
  .row-value {
    font-family: var(--bb-font-display);
    color: var(--bb-white);
    font-size: 14px;
  }
  .row-total .row-value {
    font-size: 20px;
    font-weight: 700;
  }

  .pay-form {
    margin-top: 16px;
  }
  .pay-form :global(.btn) {
    width: 100%;
  }

  .cancel-link {
    display: block;
    text-align: center;
    margin-top: 12px;
    font-family: var(--bb-font-mono);
    font-size: 12px;
    color: var(--bb-muted);
    text-decoration: underline;
    text-underline-offset: 3px;
  }
  .cancel-link:hover {
    color: var(--bb-tan-light);
  }
</style>
