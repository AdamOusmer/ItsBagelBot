<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import Heading from '@bagel/ui/svelte/Heading.svelte';
  import Icon from '@bagel/ui/svelte/Icon.svelte';
  import type { IconName } from '@bagel/ui/lib/icons';
  import { getI18n } from '@bagel/kit/i18n/context';

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
      href: '/settings#access',
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
  <Heading level={6} as="h2" variant="title" id="ov-summary-h">{t('overview.summaryHeading')}</Heading>
  <ul class="ov-summary__grid">
    {#each links as link (link.id)}
      <li>
        <a class="ov-summary__tile" href={link.href}>
          <span class="ov-summary__icon"><Icon name={link.icon} /></span>
          <span class="ov-summary__label">{link.label}</span>
          <span class="ov-summary__go"><Icon name="chevron" /></span>
        </a>
      </li>
    {/each}
  </ul>
</section>

<style>
  .ov-summary {
    display: grid;
    gap: 12px;
    margin-bottom: var(--row-gap);
  }
  .ov-summary__grid {
    list-style: none;
    margin: 0;
    padding: 0;
    display: grid;
    grid-template-columns: repeat(2, 1fr);
    gap: 12px;
  }
  .ov-summary__tile {
    display: flex;
    align-items: center;
    gap: 14px;
    height: 100%;
    min-height: 64px;
    padding: 14px 16px;
    border-radius: var(--bb-radius-md);
    background: var(--bb-card-bg);
    box-shadow: var(--bb-elev-1);
    color: var(--bb-white);
    font-size: var(--bb-text-sm);
    line-height: 1.35;
    text-decoration: none;
    transition: background var(--bb-dur-base) var(--bb-ease-out-expo),
                box-shadow var(--bb-dur-base) var(--bb-ease-out-expo);
  }
  .ov-summary__tile:hover {
    background: color-mix(in srgb, var(--bb-card-bg), var(--bb-tan) 6%);
    box-shadow: var(--bb-elev-2);
  }
  .ov-summary__tile:focus-visible {
    outline: 2px solid var(--bb-focus);
    outline-offset: 2px;
  }
  .ov-summary__icon {
    display: grid;
    place-items: center;
    flex: none;
    width: 36px;
    height: 36px;
    border-radius: var(--bb-radius-sm);
    background: rgba(var(--bb-tan-rgb), 0.1);
    color: var(--bb-tan-light);
  }
  .ov-summary__icon :global(svg) {
    width: 18px;
    height: 18px;
  }
  .ov-summary__label {
    flex: 1;
    min-width: 0;
  }
  .ov-summary__go {
    display: flex;
    flex: none;
    color: var(--bb-muted);
    transform: rotate(-90deg);
    transition: color var(--bb-dur-base) var(--bb-ease-out-expo);
  }
  .ov-summary__go :global(svg) {
    width: 16px;
    height: 16px;
  }
  .ov-summary__tile:hover .ov-summary__go {
    color: var(--bb-tan-light);
  }

  @media (max-width: 560px) {
    .ov-summary__grid {
      grid-template-columns: 1fr;
    }
  }
</style>
