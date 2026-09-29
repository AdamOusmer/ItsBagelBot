<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import {
    ButtonLink,
    Card,
    Chip,
    EmptyState,
    Heading,
    Icon,
    ManagementRow,
    PageHead,
    SearchInput,
    Text,
    getI18n
  } from '@bagel/kit';
  import { DISCORD_BADGE_KEYS } from '$lib/discord-messages';
  import GuildCrest from '$lib/components/discord/GuildCrest.svelte';

  let { data } = $props();
  const { t } = getI18n();

  const BADGE_RANK = { mine: 0, addable: 1, elsewhere: 2 } as const;
  let query = $state('');
  const choices = $derived.by(() => {
    const q = query.trim().toLowerCase();
    return [...(data.choices ?? [])]
      .filter((c) => !q || c.name.toLowerCase().includes(q))
      .sort((a, b) => BADGE_RANK[a.badge] - BADGE_RANK[b.badge] || a.name.localeCompare(b.name));
  });
  const total = $derived((data.choices ?? []).length);
</script>

<section class="screen active">
  <PageHead eyebrow={t('discord.eyebrow')} description={t('discord.pickDescription')}>
    {t('discord.pickTitlePre')} <em>{t('discord.pickTitleEm')}</em>
  </PageHead>

  <section class="block reveal" style="--i:0" aria-labelledby="dc-pick-h">
    <Heading level={6} as="h2" variant="title" id="dc-pick-h">{t('discord.pickTitle')}</Heading>
    <Card>
      {#if total === 0}
        <EmptyState title={t('discord.pickEmptyTitle')} body={t('discord.pickEmptyBody')}>
          <ButtonLink variant="secondary" href="/discord">{t('discord.pickBack')}</ButtonLink>
        </EmptyState>
      {:else}
        <div class="pick">
          <Text size="sm" tone="muted">{t('discord.pickHelp')}</Text>
          <SearchInput bind:value={query} placeholder={t('discord.pickSearchPh')} aria-label={t('discord.pickSearchLabel')} clearLabel={t('modules.searchClear')} autocomplete="off" fill />
          <ul class="servers">
            {#each choices as c (c.guildId)}
              <ManagementRow
                as="li"
                selectable={false}
                stackActions
                title={c.name || t('discord.unknownServer')}
                meta={t(DISCORD_BADGE_KEYS[c.badge])}
              >
                {#snippet lead()}<GuildCrest name={c.name || t('discord.unknownServer')} iconUrl={c.iconUrl} />{/snippet}
                {#snippet actions()}
                  {#if c.badge === 'mine'}
                    <ButtonLink variant="secondary" href={c.openURL}>
                      {t('discord.openCta')}
                    </ButtonLink>
                  {:else if c.badge === 'elsewhere'}
                    <Chip disabled aria-disabled="true" title={t('discord.pickElsewhere')}>{t('discord.pickElsewhereChip')}</Chip>
                  {:else}
                    <ButtonLink variant="primary" href={c.installURL} data-sveltekit-reload>
                      {t('discord.pickCta')}
                    </ButtonLink>
                  {/if}
                {/snippet}
              </ManagementRow>
            {/each}
          </ul>
          {#if choices.length === 0}
            <Text size="sm" tone="muted">{t('discord.pickNoMatch')}</Text>
          {/if}
          <Text size="sm" tone="muted">{t('discord.pickMissingHint')}</Text>
          <div class="note">
            <Icon name="lock" size={13} />
            <Text as="span" size="xs" tone="muted">{t('discord.pickPrivacy')}</Text>
          </div>
        </div>
      {/if}
    </Card>
  </section>
</section>

<style>
  .screen { display: flex; flex-direction: column; gap: 18px; }
  .block {
    display: flex;
    flex-direction: column;
    gap: 12px;
    margin: 0;
  }
  .pick {
    display: flex;
    flex-direction: column;
    gap: 14px;
  }
  .servers { --row-actions-indent: 70px; list-style: none; margin: 0 0 4px; padding: 0; }
  .note {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 10px 14px;
    border-radius: var(--bb-radius-sm);
    border: 1px solid var(--bb-glass-border);
    color: var(--bb-muted);
  }
</style>
