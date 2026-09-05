<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { AlertBanner, ButtonLink, Card, Chip, EmptyState, PageHead, getI18n } from '@bagel/shared';
  import { DISCORD_BADGE_KEYS } from '$lib/discord-messages';

  let { data } = $props();
  const { t } = getI18n();

  const choices = $derived(data.choices ?? []);
</script>

<section class="screen active">
  <PageHead eyebrow={t('discord.eyebrow')} description={t('discord.pickDescription')}>
    {t('discord.pickTitlePre')} <em>{t('discord.pickTitleEm')}</em>
  </PageHead>

  <section class="block reveal" style="--i:0" aria-labelledby="dc-pick-h">
    <h2 id="dc-pick-h" class="block-title">{t('discord.pickTitle')}</h2>
    <Card>
      {#if choices.length === 0}
        <EmptyState icon="discord" title={t('discord.pickEmptyTitle')} body={t('discord.pickEmptyBody')}>
          <ButtonLink variant="secondary" href="/discord">{t('discord.pickBack')}</ButtonLink>
        </EmptyState>
      {:else}
        <p class="hint">{t('discord.pickHelp')}</p>
        <ul class="servers">
          {#each choices as c (c.guildId)}
            <li class="server">
              <!-- Monogram, not the guild icon: the console CSP is
                   img-src 'self' data:, so a CDN <img> renders as a broken box
                   and leaks the visit to Discord besides. -->
              <span class="crest" aria-hidden="true">{c.monogram}</span>
              <span class="server-copy">
                <span class="server-name">{c.name || t('discord.unknownServer')}</span>
                <span class="tr-help">{t(DISCORD_BADGE_KEYS[c.badge])}</span>
              </span>
              {#if c.badge === 'present'}
                <Chip on>{t('discord.pickPresentChip')}</Chip>
              {/if}
              <ButtonLink variant="primary" icon="discord" href={c.installURL} data-sveltekit-reload>
                {t('discord.pickCta')}
              </ButtonLink>
            </li>
          {/each}
        </ul>
        <AlertBanner icon="lock">{t('discord.pickPrivacy')}</AlertBanner>
      {/if}
    </Card>
  </section>
</section>

<style>
  .screen { display: flex; flex-direction: column; gap: 18px; }
  .block { margin: 0; }
  .block-title {
    font-family: var(--bb-font-display);
    font-weight: 700;
    font-size: 16px;
    letter-spacing: -0.01em;
    color: var(--bb-white);
    margin: 0 0 12px;
  }
  .hint {
    margin: 0 0 14px;
    font-family: var(--bb-font-body);
    font-size: 12.5px;
    line-height: 1.55;
    color: var(--bb-muted);
  }

  .servers { list-style: none; margin: 0 0 18px; padding: 0; display: flex; flex-direction: column; }
  .server {
    display: flex;
    align-items: center;
    gap: 14px;
    padding: 14px 0;
    border-top: 1px solid var(--glass-border);
  }
  .server:first-child { border-top: none; padding-top: 0; }
  .crest {
    flex: none;
    width: 44px;
    height: 44px;
    border-radius: 8px;
    display: grid;
    place-items: center;
    background: rgba(201, 168, 124, 0.12);
    border: 1px solid var(--glass-border);
    color: var(--bb-tan-light);
    font-family: var(--bb-font-display);
    font-weight: 700;
    font-size: 15px;
    letter-spacing: 0.02em;
  }
  .server-copy { display: flex; flex-direction: column; gap: 2px; min-width: 0; flex: 1; }
  .server-name {
    font-family: var(--bb-font-display);
    font-weight: 700;
    font-size: 15px;
    color: var(--bb-white);
    overflow-wrap: anywhere;
  }
  .tr-help { font-family: var(--bb-font-body); font-size: 12.5px; color: var(--bb-muted); line-height: 1.45; }

  @media (max-width: 600px) {
    .server { flex-wrap: wrap; }
    .server-copy { flex-basis: 100%; order: -1; }
  }
</style>
