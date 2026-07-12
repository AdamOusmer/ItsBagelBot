<script lang="ts">
  // At-a-glance summary. Every item is a REAL link whose label spells out both the
  // number and where it goes ("Manage 8 active commands"), so a screen-reader user
  // hears the count and the destination in one breath — no non-interactive tiles
  // dressed up to look clickable.
  //
  // Honesty: a digest whose read failed reports its count as 0, which is
  // indistinguishable from an empty account. Rather than claim "Add your first
  // command" during an outage, a failed read falls back to a neutral "manage"
  // label that makes no count claim; the linked page shows the real state.
  import { ButtonLink, Icon, getI18n, type IconName } from '@bagel/shared';

  const { t } = getI18n();

  let {
    active,
    commandsOk = true,
    modulesOn,
    modulesOk = true,
    planLabel,
    people,
    sharesOk = true
  }: {
    active: number;
    commandsOk?: boolean;
    modulesOn: number;
    modulesOk?: boolean;
    planLabel: string;
    people: number;
    sharesOk?: boolean;
  } = $props();

  type SummaryLink = { id: string; href: string; icon: IconName; label: string };

  const links = $derived.by<SummaryLink[]>(() => [
    {
      id: 'commands',
      href: '/commands',
      icon: 'commands',
      label: !commandsOk
        ? t('overview.allCommands')
        : active > 0
          ? t('overview.summaryCommands', { active })
          : t('overview.summaryCommandsEmpty')
    },
    {
      id: 'modules',
      href: '/modules',
      icon: 'modules',
      label: !modulesOk
        ? t('overview.quickModules')
        : modulesOn > 0
          ? t('overview.summaryModules', { on: modulesOn })
          : t('overview.summaryModulesEmpty')
    },
    {
      id: 'plan',
      href: '/billing',
      icon: 'card',
      label: t('overview.summaryPlan', { plan: planLabel })
    },
    {
      id: 'shares',
      href: '/settings',
      icon: 'users',
      label: !sharesOk
        ? t('overview.manageInSettings')
        : people > 1
          ? t('overview.summaryShares', { n: people })
          : people === 1
            ? t('overview.summarySharesOne')
            : t('overview.summarySharesEmpty')
    }
  ]);
</script>

<section class="ov-summary" aria-labelledby="ov-summary-h">
  <h2 id="ov-summary-h" class="ov-section-h">{t('overview.summaryHeading')}</h2>
  <ul class="ov-summary__grid">
    {#each links as link (link.id)}
      <li>
        <ButtonLink href={link.href} variant="ghost" class="ov-summary__link">
          <span class="ov-summary__ico" aria-hidden="true"><Icon name={link.icon} size={16} /></span>
          <span class="ov-summary__label">{link.label}</span>
        </ButtonLink>
      </li>
    {/each}
  </ul>
</section>

<style>
  .ov-summary {
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
  .ov-summary__grid {
    list-style: none;
    margin: 0;
    padding: 0;
    display: grid;
    grid-template-columns: repeat(2, 1fr);
    gap: 10px;
  }
  /* Reshape the shared pill link into a full-width summary row: left-aligned,
     roomy tap target, count-bearing label that wraps instead of truncating. */
  .ov-summary__grid :global(.ov-summary__link) {
    width: 100%;
    min-height: 56px;
    justify-content: flex-start;
    gap: 12px;
    padding: 14px 18px;
    text-transform: none;
    letter-spacing: 0.01em;
    font-family: var(--bb-font-body);
    font-weight: 600;
    font-size: 13.5px;
    white-space: normal;
    text-align: left;
  }
  .ov-summary__ico {
    display: inline-flex;
    flex: none;
    color: var(--bb-tan-light);
  }
  .ov-summary__ico :global(svg) {
    stroke: currentColor;
    fill: none;
    stroke-width: 1.6;
  }
  .ov-summary__label {
    min-width: 0;
  }

  @media (max-width: 560px) {
    .ov-summary__grid {
      grid-template-columns: 1fr;
    }
  }
</style>
