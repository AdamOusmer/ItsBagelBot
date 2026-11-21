<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { enhance, type SubmitFunction } from '$app/forms';
  import { getI18n } from '@bagel/kit/i18n/context';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import CardHead from '@bagel/ui/svelte/CardHead.svelte';
  import DeckList from '@bagel/ui/svelte/DeckList.svelte';
  import EmptyState from '@bagel/ui/svelte/EmptyState.svelte';
  import ManagementRow from '@bagel/ui/svelte/ManagementRow.svelte';
  import Scroller from '@bagel/ui/svelte/Scroller.svelte';
  import Badge from '@bagel/ui/svelte/Badge.svelte';
  import { giveawayExclusionLabel } from '#lib/giveaway-workflow';
  import type { GiveawayExcludedWire } from '#lib/server/giveaways';

  let { accounts, editable, ready, busy, submit }: {
    accounts: GiveawayExcludedWire[];
    editable: boolean;
    ready: boolean;
    busy: boolean;
    submit: SubmitFunction;
  } = $props();
  const { t } = getI18n();
</script>

<DeckList as="section">
  <div class="section-head">
    <CardHead title={t('admin.giveaways.excludedAccounts')} headingLevel={2} />
  </div>
  <div class="accounts">
    {#if accounts.length}
      <Scroller maxHeight="100%" role="region" tabindex={0} aria-label={t('admin.giveaways.excludedAccounts')}>
        <ul class="account-list">
          {#each accounts as account (account.userId)}
            {@const actionable = editable && account.includable}
            {@const actionLabel = t(account.included ? 'admin.giveaways.undoAddBack' : 'admin.giveaways.addBack')}
            <ManagementRow
              as="li"
              href={`/users?q=${encodeURIComponent(account.userId)}`}
              title={`@${account.login}`}
              meta={`${giveawayExclusionLabel(account.reason, t)} · ${account.userId}`}
              stackActions
            >
              {#snippet marks()}
                <span class="account-status">
                  <Badge shape="status" tone={account.included ? 'success' : 'neutral'}>
                    <bb-fit><span data-fit>{t(account.included ? 'admin.giveaways.addedBack' : 'admin.giveaways.stillExcluded')}</span></bb-fit>
                  </Badge>
                </span>
              {/snippet}
              {#snippet actions()}
                <form
                  class="account-action"
                  class:available={actionable}
                  inert={!actionable}
                  method="POST"
                  action={account.included ? '?/exclude' : '?/include'}
                  use:enhance={submit}
                >
                  <input type="hidden" name="user_id" value={account.userId} />
                  <Button type="submit" variant="secondary" size="sm" block disabled={!actionable || busy} aria-label={`${actionLabel} · @${account.login}`}>
                    <bb-fit><span data-fit>{actionLabel}</span></bb-fit>
                  </Button>
                </form>
              {/snippet}
            </ManagementRow>
          {/each}
        </ul>
      </Scroller>
    {:else}
      <EmptyState title={t(ready ? 'admin.giveaways.noExcluded' : 'admin.giveaways.excludedPreviewHint')} />
    {/if}
  </div>
</DeckList>

<style>
  .section-head {
    padding: var(--bb-space-3) var(--bb-space-4);
    border-block-end: 1px solid var(--bb-border);
  }

  .accounts {
    block-size: 13rem;
  }

  .account-list {
    padding: 0;
    margin: 0;
    list-style: none;
  }

  .account-action {
    inline-size: 7rem;
    opacity: 0;
    transition: opacity var(--bb-dur-fast);
  }

  .account-status {
    display: inline-grid;
    inline-size: 6rem;
  }

  .available {
    opacity: 1;
  }
</style>
