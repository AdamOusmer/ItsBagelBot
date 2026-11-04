<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { enhance } from '$app/forms';
  import { Bolota, getI18n } from '@bagel/kit';
  import { webHref } from '@bagel/kit/site-links';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import HaloPanel from '@bagel/ui/svelte/HaloPanel.svelte';
  import IconTile from '@bagel/ui/svelte/IconTile.svelte';
  import TextLink from '@bagel/ui/svelte/TextLink.svelte';
  import { FLOWS_DOCS_PATH } from '#lib/flow/links';
  import Icon from './FlowIcon.svelte';

  let { name, ondismiss }: { name: string; ondismiss: () => void } = $props();

  const { t, locale } = getI18n();

  const GREEN = { head: '#62a593', eye: '#111113' };

  const title = $derived(t('flows.introTitle', { name }));
  const titleWords = $derived(title.split(' '));

  const tools = $derived([
    { href: '/commands', icon: 'commands', tone: 'green', color: 'var(--bb-green-glow)', title: t('flows.introCommands'), body: t('flows.introCommandsBody') },
    { href: '/modules/alerts', icon: 'bell', tone: 'gold', color: 'var(--bb-tan)', title: t('flows.introAlerts'), body: t('flows.introAlertsBody') },
    { href: '/timers', icon: 'clock', tone: 'green', color: 'var(--bb-green-glow)', title: t('flows.introTimers'), body: t('flows.introTimersBody') }
  ] as const);
</script>

<section class="intro" data-canvas="fill" aria-labelledby="flows-intro-title">
  <div class="inner">
    <div class="hero">
      <span class="glow">{t('flows.lockedChip')}</span>
      <h1 id="flows-intro-title" class="title" tabindex="-1" aria-label={title}>
        {#each titleWords as word, i (`${i}:${word}`)}
          {#if i > 0}{' '}{/if}<span class="word" aria-hidden="true" style="--w: {i}">{word}</span>
        {/each}
      </h1>

      <div class="pitch rise" style="--i: 1">
        <p class="lead">{t('flows.introLead')}</p>
        <div class="actions">
          <form
            method="POST"
            action="?/dismissIntro"
            use:enhance={() => {
              ondismiss();
              return async () => {};
            }}
          >
            <Button variant="primary" type="submit">{t('flows.introCta')}</Button>
          </form>
          {#if FLOWS_DOCS_PATH}
            <TextLink variant="inline" href={webHref(locale, FLOWS_DOCS_PATH)} external>{t('flows.introDocs')}</TextLink>
          {/if}
        </div>
      </div>

      <ol class="flow rise" style="--i: 2" aria-hidden="true">
        <li class="start"><Icon name="play" size={12} /> {t('flows.introStart')}</li>
        <li class="step" data-kind="trigger">
          <IconTile size="sm" style="--tile: 32px"><Icon name="bolt" size={16} /></IconTile>
          <span class="step-copy"><b>{t('flows.introWhenLabel')}</b>{t('flows.introWhen')}</span>
        </li>
        <li class="step" data-kind="condition">
          <IconTile size="sm" style="--tile: 32px"><Icon name="branch" size={16} /></IconTile>
          <span class="step-copy"><b>{t('flows.introIfLabel')}</b>{t('flows.introIf')}</span>
        </li>
        <li class="step" data-kind="reply">
          <IconTile size="sm" style="--tile: 32px"><Icon name="messageSquare" size={16} /></IconTile>
          <span class="step-copy"><b>{t('flows.introDoLabel')}</b>{t('flows.introDo')}</span>
        </li>
      </ol>

      <div class="mascot" aria-hidden="true">
        <Bolota name="oven_fresh" size={260} palette={GREEN} active follow />
      </div>
    </div>

    <div class="easier">
      <h2 id="flows-intro-easier" class="easier-title rise" style="--i: 3">{t('flows.introEasierTitle')}</h2>
      <nav class="cards" aria-labelledby="flows-intro-easier">
        {#each tools as tool, i (tool.href)}
          <div class="rise" style="--i: {4 + i}; --ink: {tool.color}">
            <HaloPanel href={tool.href} tone={tool.tone} class="tool-card">
              <span class="card-inner">
                <span class="glyph"><Icon name={tool.icon} size={24} /></span>
                <b class="card-name">{tool.title}</b>
                <span class="card-body">{tool.body}</span>
              </span>
            </HaloPanel>
          </div>
        {/each}
      </nav>
    </div>
  </div>
</section>

<style>
  .intro {
    --intro-top: calc(var(--bb-topbar-height) + 6px);
    position: relative;
    display: flex;
    flex: 1;
    align-items: center;
    justify-content: center;
    min-width: 0;
    margin-top: calc(-1 * var(--intro-top));
    height: 100dvh;
    overflow: hidden;
    padding: calc(var(--intro-top) + clamp(12px, 2vh, 24px)) clamp(16px, 4vw, 48px) clamp(12px, 2vh, 24px);
    background-color: var(--bb-black);
    background-image: radial-gradient(circle, rgba(var(--bb-white-rgb), 0.12) 1px, transparent 1.5px);
    background-size: 24px 24px;
  }
  .inner {
    position: relative;
    container-type: inline-size;
    display: flex;
    flex-direction: column;
    gap: clamp(24px, calc(11vh - 36px), 84px);
    width: min(1120px, 100%);
    min-width: 0;
  }

  .hero {
    --mascot: clamp(110px, 19cqw, 220px);
    display: grid;
    grid-template-columns: minmax(0, 1fr) minmax(0, 290px) var(--mascot);
    grid-template-areas:
      'glow glow mascot'
      'title title mascot'
      'pitch flow mascot';
    align-items: start;
    column-gap: clamp(20px, 5cqw, 64px);
  }
  .glow {
    grid-area: glow;
    font-size: clamp(15px, 2.1vh, 18px);
    font-weight: 700;
    letter-spacing: 0.02em;
    color: var(--bb-green-glow);
    text-shadow: 0 0 18px rgba(var(--bb-green-glow-rgb), 0.45);
    animation: fade 800ms 150ms var(--bb-ease-out-expo) both;
  }
  .title {
    grid-area: title;
    margin: clamp(6px, 1.2vh, 12px) 0 clamp(16px, 3.6vh, 36px);
    overflow: hidden;
    padding-bottom: 0.08em;
    font-family: var(--bb-font-display);
    font-size: clamp(34px, min(7cqw, 8.8vh), 78px);
    font-weight: 500;
    letter-spacing: var(--bb-tracking-display);
    line-height: 1.04;
    outline: none;
  }
  .word {
    display: inline-block;
    transform-origin: 20% 90%;
    background: linear-gradient(180deg, var(--bb-white) 40%, rgba(var(--bb-tan-light-rgb), 0.55));
    background-clip: text;
    -webkit-background-clip: text;
    color: transparent;
    animation: word-in 900ms calc(300ms + var(--w, 0) * 110ms) cubic-bezier(0.22, 1, 0.36, 1) both;
  }
  .pitch {
    grid-area: pitch;
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: clamp(16px, 3.4vh, 32px);
    min-width: 0;
  }
  .lead {
    margin: 0;
    max-width: 42ch;
    font-size: clamp(15px, 2.1vh, 18px);
    line-height: 1.6;
    color: rgba(var(--bb-white-rgb), 0.92);
  }
  .actions {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 12px 20px;
  }
  .mascot {
    grid-area: mascot;
    align-self: center;
    position: relative;
    width: var(--mascot);
    height: var(--mascot);
    animation: mascot-in 1.1s 500ms var(--bb-ease-out-back) both;
  }
  .mascot :global(svg.bolota) {
    position: absolute;
    inset: -20%;
    display: block;
    width: 140%;
    height: 140%;
    overflow: visible;
  }

  .flow {
    grid-area: flow;
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: clamp(10px, 1.8vh, 16px);
    min-width: 0;
  }
  .flow > li {
    position: relative;
  }
  .flow > li:not(:last-child)::after {
    content: '';
    position: absolute;
    left: 27px;
    top: 100%;
    width: 2px;
    height: clamp(10px, 1.8vh, 16px);
    background: rgba(var(--bb-green-glow-rgb), 0.7);
  }
  .start {
    display: inline-flex;
    align-self: flex-start;
    align-items: center;
    gap: 6px;
    height: 26px;
    padding: 0 12px;
    border-radius: var(--bb-radius-pill);
    background: var(--bb-card-bg);
    box-shadow: var(--bb-depth-2);
    color: var(--bb-green-glow);
    font-size: 13px;
    font-weight: 600;
  }
  .step {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: clamp(6px, 1.2vh, 12px) 12px;
    border-radius: var(--bb-radius-md);
    background: linear-gradient(rgba(var(--bb-white-rgb), 0.07), rgba(var(--bb-white-rgb), 0.03)), var(--bb-card-bg);
    box-shadow: var(--bb-depth-2);
  }
  .step[data-kind='trigger'] { color: var(--bb-green-glow); }
  .step[data-kind='condition'] { color: var(--bb-tan); }
  .step-copy {
    display: flex;
    flex-direction: column;
    min-width: 0;
    font-size: 13px;
    line-height: 1.3;
    color: var(--bb-white);
  }
  .step-copy b {
    font-size: 12px;
    font-weight: 600;
    color: rgba(var(--bb-white-rgb), 0.92);
  }

  .easier {
    display: flex;
    flex-direction: column;
    gap: clamp(14px, 3vh, 30px);
  }
  .easier-title {
    margin: 0;
    font-family: var(--bb-font-display);
    font-size: clamp(22px, min(3.6cqw, 4.8vh), 38px);
    font-weight: 500;
    letter-spacing: var(--bb-tracking-display);
    line-height: 1.12;
    width: max-content;
    max-width: 100%;
    background: linear-gradient(180deg, var(--bb-white) 40%, rgba(var(--bb-tan-light-rgb), 0.55));
    background-clip: text;
    -webkit-background-clip: text;
    color: transparent;
  }
  .cards {
    display: grid;
    grid-template-columns: repeat(3, minmax(0, 1fr));
    gap: clamp(12px, 2.2cqw, 24px);
  }
  .cards > div {
    display: flex;
    min-width: 0;
  }
  .cards :global(.tool-card) {
    flex: 1;
    min-width: 0;
  }
  .card-inner {
    display: flex;
    flex-direction: column;
    gap: clamp(3px, 0.7vh, 6px);
    padding: clamp(14px, calc(4vh - 12px), 28px) clamp(16px, 2.8vh, 28px);
    color: var(--bb-white);
  }
  .glyph {
    display: inline-flex;
    margin-bottom: clamp(8px, 2vh, 20px);
    color: var(--ink);
  }
  .card-name {
    font-family: var(--bb-font-display);
    font-size: clamp(20px, min(3cqw, 3.8vh), 30px);
    font-weight: 500;
    letter-spacing: var(--bb-tracking-display);
    line-height: 1.14;
  }
  .card-body {
    font-size: clamp(13px, 1.9vh, 16px);
    line-height: 1.35;
    color: rgba(var(--bb-white-rgb), 0.88);
  }

  .rise {
    animation: fade-up 900ms calc(450ms + var(--i, 0) * 120ms) var(--bb-ease-out-expo) both;
  }
  @keyframes fade {
    from { opacity: 0; }
    to { opacity: 1; }
  }
  @keyframes fade-up {
    from { opacity: 0; transform: translateY(24px); }
    to { opacity: 1; transform: none; }
  }
  @keyframes word-in {
    from { opacity: 0; transform: translateY(112%) rotate(3deg); }
    to { opacity: 1; transform: none; }
  }
  @keyframes mascot-in {
    from { opacity: 0; transform: translateY(30%) scale(0.86); }
    to { opacity: 1; transform: none; }
  }

  @container (max-width: 640px) {
    .hero {
      --mascot: clamp(84px, 22cqw, 140px);
      grid-template-columns: minmax(0, 1fr) var(--mascot);
      grid-template-areas:
        'glow mascot'
        'title mascot'
        'pitch pitch';
    }
    .flow {
      display: none;
    }
    .mascot {
      align-self: start;
    }
    .title {
      font-size: clamp(32px, 7.4cqw, 52px);
    }
  }
  @media (max-width: 560px) {
    .inner {
      gap: 26px;
    }
    .pitch {
      gap: 16px;
    }
    .title {
      margin-bottom: 14px;
    }
  }
  @container (max-width: 520px) {
    .cards {
      grid-template-columns: minmax(0, 1fr);
      gap: 10px;
    }
    .card-inner {
      display: grid;
      grid-template-columns: 24px minmax(0, 1fr);
      grid-template-areas: 'glyph name' 'glyph body';
      column-gap: 14px;
      align-items: center;
      padding: 12px 16px;
    }
    .glyph { grid-area: glyph; margin-bottom: 0; }
    .card-name { grid-area: name; font-size: 20px; }
    .card-body { grid-area: body; font-size: 13px; }
  }
  @media (max-width: 760px) {
    .intro {
      --intro-top: calc(var(--bb-topbar-height) + 10px);
      padding-bottom: calc(var(--bb-dock-clearance) + 12px);
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .glow,
    .word,
    .rise,
    .mascot {
      animation: none;
    }
  }
</style>
