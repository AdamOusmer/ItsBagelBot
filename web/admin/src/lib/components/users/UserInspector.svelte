<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import type { SubmitFunction } from '@sveltejs/kit';
  import { enhance } from '$app/forms';
  import Bolota from '@bagel/kit/components/Bolota.svelte';
  import Input from '@bagel/ui/svelte/Input.svelte';
  import Chip from '@bagel/ui/svelte/Chip.svelte';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import Scroller from '@bagel/ui/svelte/Scroller.svelte';
  import Heading from '@bagel/ui/svelte/Heading.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import TextLink from '@bagel/ui/svelte/TextLink.svelte';
  import Cluster from '@bagel/ui/svelte/Cluster.svelte';
  import FactList from '@bagel/ui/svelte/FactList.svelte';
  import Fact from '@bagel/ui/svelte/Fact.svelte';
  import { statusTone, type StatusTone } from '@bagel/kit/status-tone';
  import { ago, fmtDate } from '@bagel/kit';
  import { getI18n } from '@bagel/kit/i18n/context';
  import type { AccessKey } from '$lib/access';
  import type { AdminUserWire, AuditEntry, ChannelSubState } from '$lib/server/services';
  import type { GiveawayWinnerWire } from '$lib/server/giveaways';
  import StatusDot from '@bagel/ui/svelte/StatusDot.svelte';
  import StatePill from '../StatePill.svelte';
  import { stateOf } from './user-state';
  import { actionLabel, actionsFor, type UserActionDef } from './user-actions';

  let {
    user,
    tokenPresent,
    subState,
    viewAsUrl,
    history,
    historyError,
    prizeHistory,
    prizeHistoryError,
    canReadHistory,
    busy,
    can,
    onAction,
    onStatus,
    onMessage,
    creatorSubmit
  }: {
    user: AdminUserWire;
    tokenPresent: boolean | null;
    subState: ChannelSubState | null;
    viewAsUrl: string;
    history: AuditEntry[] | null;
    historyError: string;
    prizeHistory: GiveawayWinnerWire[] | null;
    prizeHistoryError: string;
    canReadHistory: boolean;
    busy: string | null;
    can: (key: AccessKey) => boolean;
    onAction: (def: UserActionDef) => void;
    onStatus: (status: string) => void;
    onMessage: () => void;
    creatorSubmit: SubmitFunction;
  } = $props();

  const { t } = getI18n();

  const TIERS = ['free', 'paid', 'vip'] as const;
  const state = $derived(stateOf(user));
  const locked = $derived(busy !== null);

  const servingTone = $derived<StatusTone>(
    user.banned ? statusTone('reauth_required') : statusTone(user.is_active ? 'online' : 'disabled')
  );
  const servingLabel = $derived(
    user.banned
      ? t('admin.users.connectionBanned')
      : user.is_active
        ? t('admin.users.connectionServing')
        : t('admin.users.connectionInactive')
  );

  const SUB_TONE = {
    ok: 'online',
    pending: 'connecting',
    failing: 'degraded',
    revoked: 'reauth_required',
    chat_banned: 'bot_banned',
    unknown: 'sub_unknown'
  } as const;
  const subTone = $derived<StatusTone>(
    subState === null ? statusTone('connecting') : statusTone(SUB_TONE[subState.state])
  );

  const hasPlanFacts = $derived(
    Boolean(
      user.subscription_expires_at ||
        user.subscription_source ||
        user.subscription_ref ||
        user.subscription_cancel_pending
    )
  );
</script>

<Scroller fill padding="18px" smooth>
  <div class="detail">
    <Cluster gap={3} nowrap>
      <Bolota name={user.username} size={44} active />
      <div>
        <Heading level={5} as="div" variant="title">{user.username}</Heading>
        <Text as="div" size="xs" mono tone="muted">
          {t('admin.users.identityMeta', {
            id: String(user.id),
            joined: fmtDate(user.created_at),
            updated: ago(user.updated_at)
          })}
        </Text>
      </div>
      <span class="ident-mark">
        <StatePill tone={state}>{state}</StatePill>
      </span>
    </Cluster>

    <section class="block">
      <Heading level={3} variant="label">{t('admin.users.tierTitle')}</Heading>
      <Cluster gap={2}>
        {#each TIERS as tier (tier)}
          <Chip
            tone={tier}
            pressed={user.status === tier}
            disabled={locked || !can('users.grant')}
            onclick={() => onStatus(tier)}
          >
            {tier}
          </Chip>
        {/each}
      </Cluster>
      {#if hasPlanFacts}
        <FactList>
          {#if user.subscription_expires_at}
            <Fact term={t('admin.users.planUntil')} truncate>{fmtDate(user.subscription_expires_at)}</Fact>
          {/if}
          {#if user.subscription_source}
            <Fact term={t('admin.users.planSource')} truncate>{user.subscription_source}</Fact>
          {/if}
          {#if user.subscription_ref}
            <Fact term={t('admin.users.planReference')} truncate>{user.subscription_ref}</Fact>
          {/if}
          {#if user.subscription_cancel_pending}
            <Fact term={t('admin.users.planRenewal')} tone="danger" truncate>
              {t('admin.users.planCancelPending')}
            </Fact>
          {/if}
        </FactList>
      {/if}
    </section>

    {#if can('users.test')}
      <section class="block">
        <Heading level={3} variant="label">{t('admin.users.testAccountTitle')}</Heading>
        <Text size="xs" tone="muted">{t('admin.users.testAccountHint')}</Text>
        <form class="inline" method="POST" action="?/setTestAccount" use:enhance>
          <input type="hidden" name="user_id" value={user.id} />
          <input type="hidden" name="active" value={user.test_account ? 'false' : 'true'} />
          <Button variant="ghost" type="submit" disabled={locked} busy={busy === 'setTestAccount'}>
            {user.test_account ? t('admin.users.markOrdinary') : t('admin.users.markTest')}
          </Button>
        </form>
      </section>
    {/if}

    <section class="block">
      <Heading level={3} variant="label">{t('admin.users.connectionTitle')}</Heading>
      <Cluster gap={2} nowrap>
        <StatusDot tone={servingTone} />
        <Text as="span" size="sm">{servingLabel}</Text>
      </Cluster>
      <Cluster gap={2} nowrap>
        <StatusDot tone={tokenPresent === null ? statusTone('connecting') : statusTone(tokenPresent ? 'online' : 'auth_required')} />
        <Text as="span" size="sm">
          {tokenPresent === null
            ? t('admin.users.tokenChecking')
            : tokenPresent
              ? t('admin.users.tokenPresent')
              : t('admin.users.tokenAbsent')}
        </Text>
      </Cluster>
      <Cluster gap={2} nowrap>
        <StatusDot tone={subTone} />
        <Text as="span" size="sm">
          {#if subState === null}
            {t('admin.users.eventsubChecking')}
          {:else}
            {t('admin.users.eventsubState', { state: subState.state })}
            {#if subState.checkedAt}
              · {t('admin.users.eventsubChecked', { when: ago(subState.checkedAt) })}
            {/if}
          {/if}
        </Text>
      </Cluster>
      {#if subState?.error}
        <div class="wrap"><Text size="xs" mono tone="danger">{subState.error}</Text></div>
      {/if}
      <Cluster gap={2}>
        {#each actionsFor('service', user, can) as def (def.id)}
          <Button
            variant={def.danger ? 'primary' : 'ghost'} tone={def.danger ? 'danger' : 'neutral'}
            disabled={locked}
            busy={busy === def.id}
            onclick={() => onAction(def)}
          >
            {t(actionLabel(def, user))}
          </Button>
        {/each}
      </Cluster>
    </section>

    {#if can('users.grant')}
      <section class="block">
        <Heading level={3} variant="label">{t('admin.users.creatorTitle')}</Heading>
        <form class="inline" method="POST" action="?/setCreatorCode" use:enhance={creatorSubmit}>
          <input type="hidden" name="user_id" value={user.id} />
          <Input
            fill mono
            type="text"
            name="creator_code"
            maxlength={64}
            placeholder={t('admin.users.creatorPlaceholder')}
            aria-label={t('admin.users.creatorTitle')}
            value={user.creator_code ?? ''}
          />
          <Button variant="ghost" type="submit" disabled={locked}>
            {t('admin.users.creatorSave')}
          </Button>
        </form>
      </section>
    {/if}

    <section class="block">
      <Heading level={3} variant="label">{t('admin.users.viewAsTitle')}</Heading>
      <Cluster gap={2}>
        {#each actionsFor('support', user, can) as def (def.id)}
          <Button variant="ghost" disabled={locked} busy={busy === def.id} onclick={() => onAction(def)}>
            {t(def.label)}
          </Button>
        {/each}
        {#if can('notifications.send')}
          <Button variant="ghost" disabled={locked} onclick={onMessage}>
            {t('admin.users.messageCta')}
          </Button>
        {/if}
      </Cluster>
      {#if viewAsUrl}
        <div class="inline">
          <Input
            fill mono
            type="text"
            readonly
            value={viewAsUrl}
            aria-label={t('admin.users.viewAsLinkLabel')}
          />
        </div>
        <Text size="xs" tone="muted">{t('admin.users.viewAsNote')}</Text>
      {/if}
    </section>

    {#if canReadHistory}
      <section class="block">
        <Heading level={3} variant="label">{t('admin.users.historyTitle')}</Heading>
        {#if history === null}
          <Text size="xs" tone="muted">{t('admin.users.historyLoading')}</Text>
        {:else if historyError}
          <div class="wrap"><Text size="xs" mono tone="danger">{historyError}</Text></div>
        {:else if history.length === 0}
          <Text size="xs" tone="muted">{t('admin.users.historyEmpty')}</Text>
        {:else}
          <ul class="bb-list hist">
            {#each history as e (e.id)}
              <li class="hist-row">
                <StatusDot tone={statusTone(e.ok ? 'online' : 'degraded')} />
                <span class="hist-act"><Text as="span" size="xs" mono truncate>{e.action} · @{e.actor_login}</Text></span>
                <span class="hist-when"><Text as="span" size="xs" mono tone="muted">{ago(e.created_at)}</Text></span>
              </li>
            {/each}
          </ul>
          <Text size="xs">
            <TextLink variant="inline" href={`/audit?q=${user.id}`}>{t('admin.users.historyMore')}</TextLink>
          </Text>
        {/if}
      </section>
    {/if}

    {#if can('giveaways.manage')}
      <section class="block">
        <Heading level={3} variant="label">{t('admin.users.prizeHistoryTitle')}</Heading>
        {#if prizeHistory === null}
          <Text size="xs" tone="muted">{t('admin.users.prizeHistoryLoading')}</Text>
        {:else if prizeHistoryError}
          <div class="wrap"><Text size="xs" mono tone="danger">{prizeHistoryError}</Text></div>
        {:else if prizeHistory.length === 0}
          <Text size="xs" tone="muted">{t('admin.users.prizeHistoryEmpty')}</Text>
        {:else}
          <ul class="bb-list hist">
            {#each prizeHistory as award (award.id)}
              <li class="hist-row">
                <StatusDot tone={statusTone(award.awardState === 'completed' ? 'online' : 'degraded')} />
                <span class="hist-act"><Text as="span" size="xs" mono truncate>{t('admin.users.prizeMonths', { n: award.prizeMonths })} · {award.awardState}</Text></span>
                <span class="hist-when"><Text as="span" size="xs" mono tone="muted">{award.startAt ? fmtDate(award.startAt) : t('admin.giveaways.noDate')}</Text></span>
              </li>
            {/each}
          </ul>
        {/if}
      </section>
    {/if}

    {#if actionsFor('danger', user, can).length}
      <section class="block danger">
        <Heading level={3} variant="label">{t('admin.users.dangerTitle')}</Heading>
        <Cluster gap={2}>
          {#each actionsFor('danger', user, can) as def (def.id)}
            <Button
              variant={def.danger ? 'primary' : 'ghost'} tone={def.danger ? 'danger' : 'neutral'}
              disabled={locked}
              busy={busy === def.id}
              onclick={() => onAction(def)}
            >
              {t(def.label)}
            </Button>
          {/each}
        </Cluster>
      </section>
    {/if}
  </div>
</Scroller>

<style>
  .detail {
    display: flex;
    flex-direction: column;
    gap: 20px;
  }

  .ident-mark {
    margin-left: auto;
  }

  .block {
    display: flex;
    flex-direction: column;
    gap: var(--bb-space-2);
  }

  .wrap {
    overflow-wrap: anywhere;
  }

  .inline {
    display: flex;
    gap: var(--bb-space-2);
  }

  .hist {
    display: flex;
    flex-direction: column;
    gap: var(--bb-space-2);
  }
  .hist-row {
    display: flex;
    align-items: center;
    gap: var(--bb-space-2);
    min-width: 0;
  }
  .hist-act {
    flex: 1;
    min-width: 0;
  }
  .hist-when {
    white-space: nowrap;
  }

  .danger {
    padding-top: 14px;
    border-top: 1px solid var(--bb-border);
  }
</style>
