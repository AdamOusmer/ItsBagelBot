<script lang="ts">
  // The things a streamer actually comes here to do. "New command" is the page's
  // single primary CTA; everything else on the page stays quieter so it never
  // competes. A Settings shortcut appears as a third action ONLY while the bot
  // needs attention (anything but a healthy, online connection); a healthy board
  // leaves it out.
  import { ButtonLink, getI18n } from '@bagel/shared';

  const { t } = getI18n();

  let { needsAttention = false }: { needsAttention?: boolean } = $props();
</script>

<section class="ov-quick" aria-labelledby="ov-quick-h">
  <h2 id="ov-quick-h" class="ov-section-h">{t('overview.quickHeading')}</h2>
  <div class="ov-quick__row">
    <ButtonLink href="/commands" variant="primary" icon="plus" class="ov-quick__btn">{t('overview.quickNewCommand')}</ButtonLink>
    <ButtonLink href="/modules" variant="ghost" icon="modules" class="ov-quick__btn">{t('overview.quickModules')}</ButtonLink>
    {#if needsAttention}
      <ButtonLink href="/settings" variant="ghost" icon="settings" class="ov-quick__btn">{t('overview.quickSettings')}</ButtonLink>
    {/if}
  </div>
</section>

<style>
  .ov-quick {
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
  .ov-quick__row {
    display: flex;
    gap: 10px;
    flex-wrap: wrap;
  }
  .ov-quick__row :global(.ov-quick__btn) {
    min-height: 44px;
  }
  @media (max-width: 480px) {
    .ov-quick__row :global(.ov-quick__btn) {
      flex: 1 1 100%;
    }
  }
</style>
