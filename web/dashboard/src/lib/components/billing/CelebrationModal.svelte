<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { untrack } from 'svelte';
  import { page } from '$app/state';
  import { prefersReducedMotion } from '@bagel/ui/lib/motion-query';
  import { portal } from '@bagel/ui/lib/overlay-stack';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import Heading from '@bagel/ui/svelte/Heading.svelte';
  import Modal from '@bagel/ui/svelte/Modal.svelte';
  import Spinner from '@bagel/ui/svelte/Spinner.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import { Bolota, getI18n } from '@bagel/kit';
  import { confettiPieces, type ConfettiPiece } from './confetti';

  let {
    open = $bindable(false),
    kind,
    recipient,
    isPaid,
    activationSlow
  }: {
    open: boolean;
    kind: 'premium' | 'gift';
    recipient: string;
    isPaid: boolean;
    activationSlow: boolean;
  } = $props();

  const { t } = getI18n();

  const label = $derived(
    kind === 'gift'
      ? t('billing.giftSent')
      : isPaid
        ? t('billing.premiumActivated')
        : activationSlow
          ? t('billing.paymentReceived')
          : t('billing.paymentReceivedTitle')
  );

  let celebratedActivation = $state(false);
  let confetti = $state<ConfettiPiece[]>([]);
  let confettiOrigin = $state({ x: 0, y: 0 });

  function burst() {
    if (prefersReducedMotion()) return;
    const badge = document.querySelector('.celebrate-badge')?.getBoundingClientRect();
    confettiOrigin = {
      x: badge ? badge.left + badge.width / 2 : window.innerWidth / 2,
      y: badge ? badge.top + badge.height / 2 : window.innerHeight / 2
    };
    confetti = confettiPieces(window.innerWidth, window.innerHeight, confettiOrigin.y);
    setTimeout(() => (confetti = []), 4600);
  }

  const SWIRL_MS = 1500;
  const BURST_MS = 2400;
  const BURST_SETTLED_MS = 2200;

  let confettiPending = $state(false);
  let seq = $state<'entrance' | 'burst' | null>(null);
  let seqKey = $state(0);
  let expr = $state<string | null>(null);
  let choreo: ReturnType<typeof setTimeout>[] = [];

  function clearChoreo() {
    choreo.forEach(clearTimeout);
    choreo = [];
    confettiPending = false;
  }

  function play() {
    clearChoreo();
    expr = 'love';
    if (prefersReducedMotion()) {
      seq = null;
      return;
    }
    seq = 'entrance';
    seqKey += 1;
    choreo.push(
      setTimeout(() => {
        seq = 'burst';
        seqKey += 1;
      }, SWIRL_MS)
    );
    confettiPending = true;
    choreo.push(
      setTimeout(() => {
        confettiPending = false;
        burst();
      }, SWIRL_MS + BURST_SETTLED_MS)
    );
  }

  function close() {
    open = false;
    clearChoreo();
    seq = null;
    expr = null;
    confetti = [];
  }

  $effect(() => {
    if (open) untrack(play);
  });

  const celebratingPremium = $derived(open && kind === 'premium');
  const activationBurstDue = $derived(isPaid && !celebratedActivation && !confettiPending);

  $effect(() => {
    if (celebratingPremium && activationBurstDue) {
      celebratedActivation = true;
      burst();
    }
  });
</script>

<Modal {open} onClose={close} {label}>
  <div class="celebrate">
    <div class="celebrate-badge" class:celebrate-badge--gift={kind === 'gift'}>
      <Bolota
        name={page.data.displayName ?? page.data.login ?? 'ItsBagelBot'}
        size={58}
        active={open}
        cycle={false}
        sequence={seq}
        sequenceKey={seqKey}
        sequenceFor={seq === 'entrance' ? SWIRL_MS : BURST_MS}
        expression={expr}
      />
    </div>

    {#if kind === 'gift'}
      <div class="celebrate-title"><Heading level={3}>{t('billing.giftSent')}</Heading></div>
      <div class="celebrate-body">
        <Text size="sm" tone="muted">
          {#if recipient}
            {t('billing.giftSentNamedPre')}<Text as="span" size="sm" tone="accent"><strong>@{recipient}</strong></Text>{t('billing.giftSentNamedPost')}
          {:else}
            {t('billing.giftSentBody')}
          {/if}
        </Text>
      </div>
    {:else if isPaid}
      <div class="celebrate-title"><Heading level={3}>{t('billing.premiumActivated')}</Heading></div>
      <div class="celebrate-body"><Text size="sm" tone="muted">{t('billing.premiumActivatedBody')}</Text></div>
    {:else if activationSlow}
      <div class="celebrate-title"><Heading level={3}>{t('billing.paymentReceived')}</Heading></div>
      <div class="celebrate-body"><Text size="sm" tone="muted">{t('billing.paymentSlowBody')}</Text></div>
    {:else}
      <div class="celebrate-title"><Heading level={3}>{t('billing.paymentReceivedTitle')}</Heading></div>
      <div class="celebrate-body"><Text size="sm" tone="muted">{t('billing.paymentReceivedBody')}</Text></div>
      <div class="celebrate-spinner"><Spinner size="md" /></div>
    {/if}

    <div class="bb-modal__actions celebrate-actions">
      <Button variant="primary" onclick={close}>
        {kind === 'gift' ? t('common.done') : isPaid ? t('billing.explorePremium') : t('common.gotIt')}
      </Button>
    </div>
  </div>
</Modal>

{#if confetti.length}
  <div
    class="confetti-layer"
    aria-hidden="true"
    style="--ox:{confettiOrigin.x}px; --oy:{confettiOrigin.y}px;"
    use:portal
  >
    {#each confetti as p}
      <span
        class="confetti-piece"
        style="--tx:{p.tx}px; --peak:{p.peak}px; --fall:{p.fall}px; --rot:{p.rot}deg; --delay:{p.delay}ms; --dur:{p.dur}ms; background:{p.color}; width:{p.w}px; height:{p.h}px;"
      ></span>
    {/each}
  </div>
{/if}

<style>
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
    background: radial-gradient(circle at 50% 40%, rgba(var(--bb-tan-rgb), 0.28), rgba(var(--bb-tan-rgb), 0.06));
    border: 1px solid rgba(var(--bb-tan-rgb), 0.4);
    animation: pop 620ms var(--bb-ease-out-back) both;
  }
  .celebrate-badge--gift {
    color: var(--bb-green-light);
    background: radial-gradient(circle at 50% 40%, rgba(var(--bb-green-glow-rgb), 0.28), rgba(var(--bb-green-glow-rgb), 0.06));
    border-color: rgba(var(--bb-green-glow-rgb), 0.4);
  }
  .celebrate-title {
    margin: 0 0 10px;
    animation: rise 500ms var(--bb-ease-out-expo) both;
    animation-delay: 80ms;
  }
  .celebrate-body {
    margin: 0 auto;
    max-width: 42ch;
    animation: rise 500ms var(--bb-ease-out-expo) both;
    animation-delay: 140ms;
  }
  .celebrate-spinner {
    margin-top: 18px;
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

  .confetti-layer {
    position: fixed;
    inset: 0;
    z-index: calc(var(--bb-z-overlay) + 100);
    pointer-events: none;
    overflow: hidden;
  }
  .confetti-piece {
    position: absolute;
    top: var(--oy, 50%);
    left: var(--ox, 50%);
    border-radius: var(--bb-radius-xs);
    opacity: 0;
    animation: confetti var(--dur, 3000ms) linear var(--delay, 0ms) forwards;
  }
  @keyframes confetti {
    0% {
      transform: translate(-50%, -50%) rotate(0deg) scale(0.6);
      opacity: 0;
      animation-timing-function: cubic-bezier(0.12, 0.7, 0.35, 1);
    }
    6% {
      opacity: 1;
    }
    42% {
      transform: translate(calc(-50% + var(--tx) * 0.42), calc(-50% - var(--peak)))
        rotate(calc(var(--rot) * 0.45)) scale(1);
      animation-timing-function: cubic-bezier(0.45, 0, 0.75, 0.55);
    }
    88% {
      opacity: 1;
    }
    100% {
      transform: translate(calc(-50% + var(--tx)), calc(-50% + var(--fall))) rotate(var(--rot))
        scale(1);
      opacity: 0;
    }
  }

  @media (prefers-reduced-motion: reduce) {
    .celebrate-badge,
    .celebrate-title,
    .celebrate-body,
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
</style>
