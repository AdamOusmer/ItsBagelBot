<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { ButtonLink, Card, Chip, EmptyState, Icon, PageHead, getI18n } from '@bagel/shared';
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
        <EmptyState title={t('discord.pickEmptyTitle')} body={t('discord.pickEmptyBody')}>
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
              <!-- Three outcomes, three controls. A server this broadcaster
                   already bound opens straight onto its settings; one bound to
                   a different channel offers nothing, because the install can
                   only end at the refusal that put it in this state; only a
                   fresh one gets the invite. -->
              <span class="server-actions">
                {#if c.badge === 'mine'}
                  <ButtonLink variant="secondary" href={c.openURL}>
                    {t('discord.openCta')}
                  </ButtonLink>
                {:else if c.badge === 'elsewhere'}
                  <Chip disabled aria-disabled="true">{t('discord.pickElsewhereChip')}</Chip>
                {:else}
                  <ButtonLink variant="primary" href={c.installURL} data-sveltekit-reload>
                    {t('discord.pickCta')}
                  </ButtonLink>
                {/if}
              </span>
            </li>
          {/each}
        </ul>
        <!-- Not an AlertBanner: that component defaults to the danger tone and
             role="alert", and a note about what we did with the guild list is
             reassurance, not a problem the reader has to act on. -->
        <p class="note"><Icon name="lock" size={13} />{t('discord.pickPrivacy')}</p>
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

  .note {
    display: flex;
    align-items: center;
    gap: 8px;
    margin: 0;
    padding: 10px 14px;
    border-radius: 8px;
    border: 1px solid var(--glass-border);
    font-family: var(--bb-font-body);
    font-size: 12.5px;
    line-height: 1.5;
    color: var(--bb-muted);
  }

  .server-actions { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }

  @media (max-width: 600px) {
    /* See the server list: crest and name on one line, the badge and the
       button indented under the name on the next. 58px = crest plus gap. */
    .server { flex-wrap: wrap; }
    .server-actions { flex-basis: 100%; padding-left: 58px; }
  }
</style>
