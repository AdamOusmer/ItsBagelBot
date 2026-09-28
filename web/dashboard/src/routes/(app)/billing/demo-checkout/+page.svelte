<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { AlertBanner, AuroraBg, Button, Card, Heading, Label, LightField, PageHead, Text, TextLink } from '@bagel/ui/svelte';
  import { getI18n } from '@bagel/kit';

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

  <AlertBanner tone="danger" role="status">{data.copy.demoNotice}</AlertBanner>

  <Card>
    <div class="checkout">
      <div class="row">
        <Label mono as="span">{data.copy.demoPlan}</Label>
        <Text as="span" size="sm">{planLabel}</Text>
      </div>
      {#if isGift}
        <div class="row">
          <Label mono as="span">{data.copy.demoGiftTo}</Label>
          <Text as="span" size="sm">@{data.recipient}</Text>
        </div>
      {/if}
      <div class="row row-total">
        <Label mono as="span">{data.copy.demoTotal}</Label>
        <Heading level={4} as="span">${PRICE}.00 CAD</Heading>
      </div>

      <form method="POST" action="?/pay" class="pay-form">
        <input type="hidden" name="plan" value={data.plan} />
        <input type="hidden" name="kind" value={data.kind} />
        {#if isGift}<input type="hidden" name="recipient" value={data.recipient} />{/if}
        <Button type="submit" variant="primary" block>{data.copy.demoPay.replace('{price}', String(PRICE))}</Button>
      </form>
      <div class="cancel">
        <TextLink href="/billing" label={data.copy.demoCancel} />
      </div>
    </div>
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

  .checkout {
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

  .pay-form {
    margin-top: 16px;
  }

  .cancel {
    display: flex;
    justify-content: center;
    margin-top: 12px;
  }
</style>
