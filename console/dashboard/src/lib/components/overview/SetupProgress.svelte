<script lang="ts">
  // Incomplete accounts: a short, ordered setup checklist instead of an empty
  // "top commands" card. Each step says what it is, whether it is done (in words,
  // not just a tick), and links to where it gets done. `receiving` is main's honest
  // "online" (grant + active + enroll ok), so a pending/failing connection does not
  // read as connected here.
  import { Card, ButtonLink, Icon, getI18n, type IconName } from '@bagel/shared';

  const { t } = getI18n();

  let {
    receiving,
    hasCommands,
    modulesOn
  }: {
    receiving: boolean;
    hasCommands: boolean;
    modulesOn: boolean;
  } = $props();

  type Step = { id: string; icon: IconName; label: string; href: string; done: boolean };

  const steps = $derived.by<Step[]>(() => [
    { id: 'connect', icon: 'power', label: t('overview.setupConnect'), href: '/settings', done: receiving },
    { id: 'command', icon: 'commands', label: t('overview.setupCommand'), href: '/commands', done: hasCommands },
    { id: 'module', icon: 'modules', label: t('overview.setupModule'), href: '/modules', done: modulesOn }
  ]);

  const doneCount = $derived(steps.filter((s) => s.done).length);
</script>

<section class="ov-setup" aria-labelledby="ov-setup-h">
  <div class="ov-setup__head">
    <h2 id="ov-setup-h" class="ov-section-h">{t('overview.setupHeading')}</h2>
    <span class="ov-setup__count">{t('overview.setupProgress', { done: doneCount, total: steps.length })}</span>
  </div>
  <Card>
    <ol class="ov-setup__list">
      {#each steps as step (step.id)}
        <li class="ov-setup__row" class:done={step.done}>
          <span class="ov-setup__ico" aria-hidden="true">
            <Icon name={step.done ? 'check' : step.icon} size={15} />
          </span>
          <span class="ov-setup__label">{step.label}</span>
          {#if step.done}
            <span class="ov-setup__state">{t('common.done')}</span>
          {:else}
            <ButtonLink href={step.href} variant="ghost" class="ov-setup__cta">{t('common.open')}</ButtonLink>
          {/if}
        </li>
      {/each}
    </ol>
  </Card>
</section>

<style>
  .ov-setup {
    margin-bottom: var(--row-gap);
  }
  .ov-setup__head {
    display: flex;
    align-items: baseline;
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
  .ov-setup__count {
    font-family: var(--bb-font-mono);
    font-size: 11px;
    letter-spacing: 0.08em;
    color: var(--bb-muted);
    white-space: nowrap;
  }
  .ov-setup__list {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
  }
  .ov-setup__row {
    display: flex;
    align-items: center;
    gap: 13px;
    padding: 14px 2px;
    border-bottom: 1px solid var(--bb-border);
  }
  .ov-setup__row:last-child {
    border-bottom: 0;
  }
  .ov-setup__ico {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 32px;
    height: 32px;
    border-radius: 8px;
    flex: none;
    background: rgba(201, 168, 124, 0.1);
    border: 1px solid rgba(201, 168, 124, 0.28);
    color: var(--bb-tan-light);
  }
  .ov-setup__row.done .ov-setup__ico {
    background: var(--bb-status-success-bg);
    border-color: var(--bb-status-success-border);
    color: var(--bb-status-success);
  }
  .ov-setup__ico :global(svg) {
    stroke: currentColor;
    fill: none;
    stroke-width: 1.7;
  }
  .ov-setup__label {
    flex: 1;
    min-width: 0;
    font-family: var(--bb-font-body);
    font-size: 13.5px;
    color: var(--bb-white);
  }
  .ov-setup__row.done .ov-setup__label {
    color: var(--bb-muted);
  }
  .ov-setup__state {
    display: inline-flex;
    align-items: center;
    font-family: var(--bb-font-mono);
    font-size: 11px;
    letter-spacing: 0.08em;
    text-transform: uppercase;
    color: var(--bb-status-success-fg);
    white-space: nowrap;
  }
  .ov-setup__row :global(.ov-setup__cta) {
    flex: none;
    min-height: 44px;
  }
</style>
