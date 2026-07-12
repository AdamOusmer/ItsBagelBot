<script lang="ts">
  // Established accounts: the most-used commands, each a compact ledger line. The
  // section owns its <h2>; the card title role is folded into it so the heading
  // order stays h1 -> h2 with no decorative h3 in between.
  import { Card, Icon, getI18n, type CommandView } from '@bagel/shared';

  const { t } = getI18n();

  let { top }: { top: CommandView[] } = $props();
</script>

<section class="ov-top" aria-labelledby="ov-top-h">
  <div class="ov-top__head">
    <h2 id="ov-top-h" class="ov-section-h">{t('overview.topCommands')}</h2>
    <a class="ov-more" href="/commands">{t('overview.allCommands')}</a>
  </div>
  <Card>
    <ul class="feed ov-feed">
      {#each top as c (c.name)}
        <li class="feed-row">
          <span class="fi green" aria-hidden="true"><Icon name="commands" size={15} /></span>
          <span class="ft">
            <b class="mono">!{c.name}</b>
            <span class="clip">{c.response}</span>
          </span>
          <span class="fw uses">{t('overview.usesN', { n: c.uses ?? '0' })}</span>
        </li>
      {/each}
      <li class="feed-row">
        <span class="fi" aria-hidden="true"><Icon name="plus" size={15} /></span>
        <span class="ft">
          <b>{t('overview.addAnother')}</b>
          <span>{t('overview.addAnotherDesc')}</span>
        </span>
        <a class="fw ov-link" href="/commands">{t('common.open')}</a>
      </li>
    </ul>
  </Card>
</section>

<style>
  .ov-top {
    margin-bottom: var(--row-gap);
  }
  .ov-top__head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    margin-bottom: 12px;
  }
  .ov-section-h {
    font-family: var(--bb-font-display);
    font-weight: 700;
    font-size: 16px;
    letter-spacing: -0.01em;
    color: var(--bb-white);
    margin: 0;
  }
  .ov-more,
  .ov-link {
    font-family: var(--bb-font-body);
    font-weight: 600;
    font-size: 12.5px;
    color: var(--bb-tan);
    text-decoration: none;
  }
  .ov-more:hover,
  .ov-link:hover {
    color: var(--bb-tan-pale);
  }
  /* The list itself is a <ul>; strip default list affordances but keep the
     shared .feed row rhythm. */
  .ov-feed {
    list-style: none;
    margin: 0;
    padding: 0;
  }
  .mono {
    font-family: var(--bb-font-mono);
  }
  .uses {
    font-family: var(--bb-font-mono);
    font-size: 11px;
    color: var(--bb-muted);
    white-space: nowrap;
  }
  .clip {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    display: block;
    max-width: 100%;
  }
</style>
