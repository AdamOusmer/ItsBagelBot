<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  // Needs-attention strip. Surfaces ONLY issues the status panel does not already
  // own (the whole connection story lives there), and only when they are REAL:
  // an empty issue set renders nothing at all. Each row names the problem in plain
  // words and carries its fix as a real link.
  //
  // Honesty: the `ok` flags come from main's digests. A failed read reports
  // active/total/pending as 0, which is indistinguishable from an empty account,
  // so a down read must never manufacture an "all disabled" / "invites pending"
  // row. Guard every issue on its read having actually landed.
  import ButtonLink from '@bagel/ui/svelte/ButtonLink.svelte';
  import { getI18n } from '@bagel/kit/i18n/context';

  const { t } = getI18n();

  let {
    active,
    total,
    commandsOk = true,
    pendingShares = 0,
    sharesOk = true
  }: {
    active: number;
    total: number;
    commandsOk?: boolean;
    pendingShares?: number;
    sharesOk?: boolean;
  } = $props();

  type Issue = { id: string; text: string; cta: string; href: string };

  const issues = $derived.by<Issue[]>(() => {
    const out: Issue[] = [];
    // Commands exist but every one is switched off. The bot stays silent.
    if (commandsOk && total > 0 && active === 0) {
      out.push({
        id: 'all-disabled',
        text: t('overview.issueAllDisabled'),
        cta: t('overview.issueAllDisabledCta'),
        href: '/commands'
      });
    }
    // Shared-access invites nobody has accepted yet.
    if (sharesOk && pendingShares > 0) {
      out.push({
        id: 'pending-invites',
        text: t('overview.invitesPending', { n: pendingShares }),
        cta: t('overview.manageInSettings'),
        href: '/settings'
      });
    }
    return out;
  });
</script>

{#if issues.length}
  <section class="ov-attention" aria-labelledby="ov-attention-h">
    <h2 id="ov-attention-h" class="ov-section-h">{t('overview.attentionHeading')}</h2>
    <ul class="ov-attention__list">
      {#each issues as issue (issue.id)}
        <li class="ov-attention__row">
          <span class="ov-attention__text">{issue.text}</span>
          <ButtonLink href={issue.href} variant="ghost" class="ov-attention__cta">{issue.cta}</ButtonLink>
        </li>
      {/each}
    </ul>
  </section>
{/if}

<style>
  .ov-attention {
    margin-bottom: var(--row-gap);
  }
  .ov-section-h {
    font-family: var(--bb-font-display);
    font-weight: 700;
    font-size: 16px;
    letter-spacing: -0.01em;
    color: var(--bb-white);
    margin: 0 0 12px;
  }
  .ov-attention__list {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  .ov-attention__row {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 12px 16px;
    background: var(--bb-status-warning-bg);
    border: 1px solid var(--bb-status-warning-border);
    border-radius: var(--bb-radius-sm);
  }
  .ov-attention__text {
    flex: 1;
    min-width: 0;
    font-family: var(--bb-font-body);
    font-size: 13.5px;
    line-height: 1.45;
    color: var(--bb-white);
  }
  .ov-attention__row :global(.ov-attention__cta) {
    flex: none;
    min-height: 44px;
  }

  @media (max-width: 560px) {
    .ov-attention__row {
      flex-wrap: wrap;
    }
    .ov-attention__text {
      flex-basis: 100%;
      order: 2;
    }
    .ov-attention__row :global(.ov-attention__cta) {
      order: 3;
    }
  }
</style>
