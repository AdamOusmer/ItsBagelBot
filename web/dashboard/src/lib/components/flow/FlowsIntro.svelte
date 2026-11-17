<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { enhance } from '$app/forms';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import { getI18n } from '@bagel/kit';
  import { webHref } from '@bagel/kit/site-links';
  import ButtonLink from '@bagel/ui/svelte/ButtonLink.svelte';
  import HaloPanel from '@bagel/ui/svelte/HaloPanel.svelte';
  import TextLink from '@bagel/ui/svelte/TextLink.svelte';
  import { FLOWS_DOCS_PATH } from '#lib/flow/links';
  import { STARTERS } from '#lib/flow/starters';
  import Icon from './FlowIcon.svelte';

  let { name, ondismiss, canDismiss = true }: { name: string; ondismiss: () => void; canDismiss?: boolean } = $props();
  const { t, locale } = getI18n();
</script>

<section class="intro bb-canvas-fill" aria-labelledby="flows-intro-title">
  <div class="inner">
    <header class="head">
      <h1 id="flows-intro-title" class="title bb-fit-box"><bb-fit>{t('flows.introTitle', { name })}</bb-fit></h1>
      <p class="lead bb-fit-box"><bb-fit mode="block">{t('flows.introLead')}</bb-fit></p>
    </header>
    <nav class="cards" aria-label={t('flows.starters.title')}>
      {#each STARTERS as starter (starter)}
        <HaloPanel href="/flows/new?starter={starter}" class="starter">
          <span class="card-inner">
            <b class="card-name bb-fit-box"><bb-fit>{t(`flows.starters.${starter}.title`)}</bb-fit></b>
            <span class="card-body bb-fit-box"><bb-fit mode="block">{t(`flows.starters.${starter}.body`)}</bb-fit></span>
            <span class="card-action"><span class="card-action-text bb-fit-box"><bb-fit>{t('flows.starters.open')}</bb-fit></span><Icon name="arrowUpRight" size={16} /></span>
          </span>
        </HaloPanel>
      {/each}
    </nav>
    <div class="actions">
      <ButtonLink variant="ghost" href="/flows/new">{t('flows.introCta')}</ButtonLink>
      {#if canDismiss}
        <form method="POST" action="?/dismissIntro" use:enhance={() => {
          ondismiss();
          return async () => {};
        }}>
          <Button variant="ghost" type="submit">{t('flows.clarity.browse')}</Button>
        </form>
      {/if}
      {#if FLOWS_DOCS_PATH}
        <TextLink variant="inline" href={webHref(locale, FLOWS_DOCS_PATH)} external>{t('flows.introDocs')}</TextLink>
      {/if}
    </div>
  </div>
</section>

<style>
  .intro { display: flex; align-items: center; justify-content: center; min-width: 0; min-height: calc(100dvh - var(--bb-topbar-height) - 24px); padding: 24px; }
  .inner { display: flex; flex-direction: column; gap: 28px; width: min(100%, 1000px); }
  .head { display: flex; flex-direction: column; gap: 12px; }
  .title { margin: 0; height: 64px; font-family: var(--bb-font-display); font-size: clamp(32px, 4vw, 52px); font-weight: 500; letter-spacing: var(--bb-tracking-display); line-height: 1.12; }
  .lead { margin: 0; height: 48px; font-size: 16px; line-height: 1.5; }
  .cards { display: flex; gap: 16px; }
  .cards :global(.starter) { flex: 1; min-width: 0; }
  .card-inner { display: flex; flex-direction: column; gap: 12px; height: 200px; padding: 24px; }
  .card-name { display: block; height: 28px; font-size: 22px; line-height: 1.2; }
  .card-body { display: block; height: 48px; font-size: 15px; line-height: 1.5; }
  .card-action { display: flex; align-items: center; justify-content: space-between; gap: 12px; margin-top: auto; height: 24px; font-size: 14px; }
  .card-action-text { flex: 1; min-width: 0; }
  .actions { display: flex; flex-wrap: wrap; align-items: center; gap: 16px; }
  @media (max-width: 760px) {
    .intro { padding: 12px 16px calc(var(--bb-dock-clearance) + 12px); min-height: 0; }
    .inner { gap: 16px; }
    .title { height: 48px; }
    .lead { height: 48px; font-size: 14px; }
    .cards { flex-direction: column; gap: 10px; }
    .card-inner { height: 112px; padding: 12px 16px; gap: 4px; }
    .card-name { height: 24px; font-size: 19px; }
    .card-body { height: 24px; font-size: 13px; }
  }
</style>
