<script lang="ts">
  // Needs-attention strip. Surfaces ONLY issues the status panel does not already
  // own (the whole connection story lives there), and only when they are REAL —
  // an empty issue set renders nothing at all. Each row names the problem in plain
  // words and carries its fix as a real link.
  //
  // Honesty: the `ok` flags come from main's digests. A failed read reports
  // active/total/pending as 0, which is indistinguishable from an empty account,
  // so a down read must never manufacture an "all disabled" / "invites pending"
  // row. Guard every issue on its read having actually landed.
  import { ButtonLink, Icon, getI18n, type IconName } from '@bagel/shared';

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

  type Issue = { id: string; icon: IconName; text: string; cta: string; href: string };

  const issues = $derived.by<Issue[]>(() => {
    const out: Issue[] = [];
    // Commands exist but every one is switched off — the bot stays silent.
    if (commandsOk && total > 0 && active === 0) {
      out.push({
        id: 'all-disabled',
        icon: 'edit',
        text: t('overview.issueAllDisabled'),
        cta: t('overview.issueAllDisabledCta'),
        href: '/commands'
      });
    }
    // Shared-access invites nobody has accepted yet.
    if (sharesOk && pendingShares > 0) {
      out.push({
        id: 'pending-invites',
        icon: 'users',
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
          <span class="ov-attention__ico" aria-hidden="true"><Icon name={issue.icon} size={15} /></span>
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
    border-radius: 8px;
  }
  .ov-attention__ico {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 28px;
    height: 28px;
    flex: none;
    border-radius: 8px;
    background: rgba(201, 168, 124, 0.14);
    color: var(--bb-status-warning);
  }
  .ov-attention__ico :global(svg) {
    stroke: currentColor;
    fill: none;
    stroke-width: 1.7;
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
      margin-left: 40px;
    }
  }
</style>
