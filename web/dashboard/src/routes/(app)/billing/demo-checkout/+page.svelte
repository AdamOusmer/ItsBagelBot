<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { AuroraBg, LightField, PageHead, Card, Button, getI18n } from '@bagel/kit';

  let { data } = $props();
  const { t } = getI18n();

  const PRICE = 7;

  const planLabel = $derived(data.plan === 'monthly' ? t('billing.subscribeMonthly') : t('billing.buyOneMonth'));
  const isGift = $derived(data.kind === 'gift');
</script>

<AuroraBg />
<div class="starfield" aria-hidden="true"><LightField warmth={0.7} /></div>

<section class="screen active">
  <PageHead eyebrow={data.copy.demoEyebrow} description={data.copy.demoDescription}>
    {data.copy.demoTitle}
  </PageHead>

  <div class="demo-banner" role="status">
    <span>{data.copy.demoNotice}</span>
  </div>

  <Card class="checkout-card">
    <div class="row">
      <span class="row-label">{data.copy.demoPlan}</span>
      <span class="row-value">{planLabel}</span>
    </div>
    {#if isGift}
      <div class="row">
        <span class="row-label">{data.copy.demoGiftTo}</span>
        <span class="row-value">@{data.recipient}</span>
      </div>
    {/if}
    <div class="row row-total">
      <span class="row-label">{data.copy.demoTotal}</span>
      <span class="row-value">${PRICE}.00 CAD</span>
    </div>

    <form method="POST" action="?/pay" class="pay-form">
      <input type="hidden" name="plan" value={data.plan} />
      <input type="hidden" name="kind" value={data.kind} />
      {#if isGift}<input type="hidden" name="recipient" value={data.recipient} />{/if}
      <Button type="submit" variant="primary">{data.copy.demoPay.replace('{price}', String(PRICE))}</Button>
    </form>
    <a class="cancel-link" href="/billing">{data.copy.demoCancel}</a>
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
    border-radius: var(--bb-radius-md);
    font-family: var(--bb-font-mono);
    font-size: 12px;
    line-height: 1.5;
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
  .pay-form { --btn-w: 100%; }

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
