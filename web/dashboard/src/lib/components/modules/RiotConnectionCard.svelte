<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import Card from '@bagel/ui/svelte/Card.svelte';
  import Heading from '@bagel/ui/svelte/Heading.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import ButtonLink from '@bagel/ui/svelte/ButtonLink.svelte';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import { getI18n } from '@bagel/kit';
  import type { RiotConnection } from '#lib/server/riot-store';

  let { connection }: { connection: RiotConnection } = $props();
  const { t } = getI18n();
  const linked = $derived(connection.account !== null);
  const canSignIn = $derived(connection.configured && !connection.unavailable);
  const errors: Readonly<Partial<Record<string, string>>> = $derived({
    unconfigured: t('modules.catalog.valorant.connection.unconfigured'),
    declined: t('modules.catalog.valorant.connection.declined'),
    state: t('modules.catalog.valorant.connection.state'),
    oauth: t('modules.catalog.valorant.connection.oauth'),
    store: t('modules.catalog.valorant.connection.store'),
    unlink: t('modules.catalog.valorant.connection.unlinkFailed')
  });
  const fallbackError = $derived(connection.unavailable ? 'store' : !connection.configured && !linked ? 'unconfigured' : '');
  const errorText = $derived(Object.hasOwn(errors, connection.errorSlug) ? errors[connection.errorSlug] : errors[fallbackError]);
</script>

<Card>
  <div class="connection">
    <div class="details" aria-live="polite">
      <Heading level={6} as="h2">
        <span class="fit-line bb-fit-box"><bb-fit>{t('modules.catalog.valorant.connection.title')}</bb-fit></span>
      </Heading>
      <Text size="sm" tone="muted">
        <span class="fit-line bb-fit-box"><bb-fit>
          {linked
            ? t('modules.catalog.valorant.connection.linked', { account: connection.account ?? '', shard: connection.shard })
            : t('modules.catalog.valorant.connection.prompt')}
        </bb-fit></span>
      </Text>
    </div>
    <div class="controls">
      <div class="control" class:faded={linked} inert={linked}>
        <ButtonLink href="/riot/connect" disabled={!canSignIn} block data-sveltekit-reload>
          <span class="action-label bb-fit-box"><bb-fit>{t('modules.catalog.valorant.connection.signIn')}</bb-fit></span>
        </ButtonLink>
      </div>
      <form class="control" class:faded={!linked} inert={!linked} method="POST" action="/riot/unlink" data-sveltekit-reload>
        <Button type="submit" tone="danger" block>
          <span class="action-label bb-fit-box"><bb-fit>{t('modules.catalog.valorant.connection.unlink')}</bb-fit></span>
        </Button>
      </form>
    </div>
  </div>
  <Text size="xs" tone="danger" role="status">
    <span class="error-slot bb-fit-box"><bb-fit mode="block">{errorText ?? ''}</bb-fit></span>
  </Text>
</Card>

<style>
  .connection { display: grid; grid-template-columns: minmax(0, 1fr) 11rem; gap: 12px; align-items: center; }
  .details { display: grid; gap: 6px; min-width: 0; }
  .fit-line { display: block; overflow: hidden; min-height: 1lh; }
  .controls { display: grid; }
  .control { grid-area: 1 / 1; transition: opacity 160ms ease; }
  .faded { opacity: 0; pointer-events: none; }
  .action-label { display: block; width: 8rem; text-align: center; }
  .error-slot { display: block; height: 2lh; margin-top: 6px; }
  @media (max-width: 560px) {
    .connection { grid-template-columns: minmax(0, 1fr); }
    .controls { width: 11rem; max-width: 100%; }
  }
  @media (prefers-reduced-motion: reduce) {
    .control { transition: none; }
  }
</style>
