<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  // The body of the user inspector: what this account IS, followed by what an
  // operator may do to it. Presentational -- every mutation is a hidden form on
  // the page, so this component never owns a POST and the confirmation, the
  // optimistic apply and the rollback all stay in one place.
  //
  // Facts first, actions last, and each action block sits under the facts it
  // acts on: the ban button is beside the serving state, the token wipe beside
  // the token badge. The previous layout put every verb in one "Danger" row at
  // the bottom, which is how an operator ended up clearing a token to fix an
  // EventSub problem the row above had already explained.
  import type { SubmitFunction } from '@sveltejs/kit';
  import { enhance } from '$app/forms';
  import Bolota from '@bagel/shared/components/Bolota.svelte';
  import Chip from '@bagel/shared/components/Chip.svelte';
  import Button from '@bagel/shared/components/Button.svelte';
  import Scroller from '@bagel/shared/components/Scroller.svelte';
  import { statusTone, type StatusTone } from '@bagel/shared/status-tone';
  import { ago, fmtDate } from '@bagel/shared';
  import { getI18n } from '@bagel/shared/i18n/context';
  import type { AccessKey } from '$lib/access';
  import type { AdminUserWire, AuditEntry, ChannelSubState } from '$lib/server/services';
  import StatusDot from '../StatusDot.svelte';
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
    canReadHistory,
    busy,
    can,
    onAction,
    onStatus,
    onMessage,
    creatorSubmit
  }: {
    user: AdminUserWire;
    /** null while the probe is in flight. Never rendered as a guess. */
    tokenPresent: boolean | null;
    subState: ChannelSubState | null;
    viewAsUrl: string;
    history: AuditEntry[] | null;
    historyError: string;
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

  // Serving state, in the same vocabulary the dashboard's connection panel uses,
  // so "banned" and "deactivated" cannot render as two shades of the same word.
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

  // 'unknown' is warning, not error: the read failed, which is not the same as
  // the subscription failing.
  const SUB_TONE = {
    ok: 'online',
    pending: 'connecting',
    failing: 'degraded',
    revoked: 'reauth_required',
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

<Scroller fill padding="18px" data-lenis-prevent>
  <div class="detail">
    <div class="ident">
      <Bolota name={user.username} size={44} active />
      <div>
        <div class="ident-name">{user.username}</div>
        <div class="ident-meta">
          {t('admin.users.identityMeta', {
            id: String(user.id),
            joined: fmtDate(user.created_at),
            updated: ago(user.updated_at)
          })}
        </div>
      </div>
      <StatePill tone={state}>{state}</StatePill>
    </div>

    <section class="block">
      <h3 class="block-label">{t('admin.users.tierTitle')}</h3>
      <div class="tiers">
        {#each TIERS as tier (tier)}
          <Chip
            class="tier-{tier}"
            on={user.status === tier}
            disabled={locked || !can('users.grant')}
            onclick={() => onStatus(tier)}
          >
            {tier}
          </Chip>
        {/each}
      </div>
      {#if hasPlanFacts}
        <dl class="facts">
          {#if user.subscription_expires_at}
            <div>
              <dt>{t('admin.users.planUntil')}</dt>
              <dd>{fmtDate(user.subscription_expires_at)}</dd>
            </div>
          {/if}
          {#if user.subscription_source}
            <div><dt>{t('admin.users.planSource')}</dt><dd>{user.subscription_source}</dd></div>
          {/if}
          {#if user.subscription_ref}
            <div><dt>{t('admin.users.planReference')}</dt><dd>{user.subscription_ref}</dd></div>
          {/if}
          {#if user.subscription_cancel_pending}
            <div>
              <dt>{t('admin.users.planRenewal')}</dt>
              <dd class="warn">{t('admin.users.planCancelPending')}</dd>
            </div>
          {/if}
        </dl>
      {/if}
    </section>

    <section class="block">
      <h3 class="block-label">{t('admin.users.connectionTitle')}</h3>
      <p class="probe"><StatusDot tone={servingTone} /><span>{servingLabel}</span></p>
      <p class="probe">
        <StatusDot tone={tokenPresent === null ? statusTone('connecting') : statusTone(tokenPresent ? 'online' : 'auth_required')} />
        <span>
          {tokenPresent === null
            ? t('admin.users.tokenChecking')
            : tokenPresent
              ? t('admin.users.tokenPresent')
              : t('admin.users.tokenAbsent')}
        </span>
      </p>
      <p class="probe">
        <StatusDot tone={subTone} />
        <span>
          {#if subState === null}
            {t('admin.users.eventsubChecking')}
          {:else}
            {t('admin.users.eventsubState', { state: subState.state })}
            {#if subState.checkedAt}
              · {t('admin.users.eventsubChecked', { when: ago(subState.checkedAt) })}
            {/if}
          {/if}
        </span>
      </p>
      {#if subState?.error}
        <p class="probe-err">{subState.error}</p>
      {/if}
      <div class="acts">
        {#each actionsFor('service', user, can) as def (def.id)}
          <Button
            variant={def.danger ? 'destructive' : 'ghost'}
            disabled={locked}
            loading={busy === def.id}
            onclick={() => onAction(def)}
          >
            {t(actionLabel(def, user))}
          </Button>
        {/each}
      </div>
    </section>

    {#if can('users.grant')}
      <section class="block">
        <h3 class="block-label">{t('admin.users.creatorTitle')}</h3>
        <form class="inline" method="POST" action="?/setCreatorCode" use:enhance={creatorSubmit}>
          <input type="hidden" name="user_id" value={user.id} />
          <input
            class="text-input"
            type="text"
            name="creator_code"
            maxlength="64"
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
      <h3 class="block-label">{t('admin.users.viewAsTitle')}</h3>
      <div class="acts">
        {#each actionsFor('support', user, can) as def (def.id)}
          <Button variant="ghost" disabled={locked} loading={busy === def.id} onclick={() => onAction(def)}>
            {t(def.label)}
          </Button>
        {/each}
        {#if can('notifications.send')}
          <Button variant="ghost" disabled={locked} onclick={onMessage}>
            {t('admin.users.messageCta')}
          </Button>
        {/if}
      </div>
      {#if viewAsUrl}
        <div class="inline">
          <input
            class="text-input"
            type="text"
            readonly
            value={viewAsUrl}
            aria-label={t('admin.users.viewAsLinkLabel')}
          />
        </div>
        <p class="note">{t('admin.users.viewAsNote')}</p>
      {/if}
    </section>

    {#if canReadHistory}
      <section class="block">
        <h3 class="block-label">{t('admin.users.historyTitle')}</h3>
        {#if history === null}
          <p class="note">{t('admin.users.historyLoading')}</p>
        {:else if historyError}
          <p class="probe-err">{historyError}</p>
        {:else if history.length === 0}
          <p class="note">{t('admin.users.historyEmpty')}</p>
        {:else}
          <ul class="bb-list hist">
            {#each history as e (e.id)}
              <li class="hist-row">
                <StatusDot tone={statusTone(e.ok ? 'online' : 'degraded')} />
                <span class="hist-act">{e.action} · @{e.actor_login}</span>
                <span class="hist-when">{ago(e.created_at)}</span>
              </li>
            {/each}
          </ul>
          <a class="note more" href={`/audit?q=${user.id}`}>{t('admin.users.historyMore')}</a>
        {/if}
      </section>
    {/if}

    {#if actionsFor('danger', user, can).length}
      <section class="block danger">
        <h3 class="block-label">{t('admin.users.dangerTitle')}</h3>
        <div class="acts">
          {#each actionsFor('danger', user, can) as def (def.id)}
            <Button
              variant={def.danger ? 'destructive' : 'ghost'}
              disabled={locked}
              loading={busy === def.id}
              onclick={() => onAction(def)}
            >
              {t(def.label)}
            </Button>
          {/each}
        </div>
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

  .ident {
    display: flex;
    align-items: center;
    gap: 12px;
  }
  .ident-name {
    font-family: var(--bb-font-display);
    font-weight: 700;
    font-size: 16px;
    color: var(--bb-white);
  }
  .ident-meta {
    font-family: var(--bb-font-mono);
    font-size: 11.5px;
    color: var(--bb-muted);
    margin-top: 2px;
  }
  .ident :global(.pill) {
    margin-left: auto;
  }

  .block {
    display: flex;
    flex-direction: column;
    gap: 9px;
  }
  .block-label {
    font-family: var(--bb-font-mono);
    font-size: 10px;
    font-weight: 400;
    letter-spacing: 0.12em;
    text-transform: uppercase;
    color: var(--bb-muted);
    margin: 0;
  }
  .note {
    font-family: var(--bb-font-body);
    font-size: 12px;
    color: var(--bb-muted);
    margin: 0;
  }
  .more {
    text-decoration: none;
    color: var(--bb-tan);
  }
  .more:hover {
    color: var(--bb-tan-pale);
  }

  .tiers {
    display: flex;
    gap: 6px;
    flex-wrap: wrap;
  }
  /* The selected tier wears its own colour (VIP silver, never purple); Chip's
     own `is-on` tan is the fallback for anything without a tier token. */
  .tiers :global(.tier-free.is-on) {
    color: var(--bb-tier-free);
    background: var(--bb-tier-free-bg);
    border-color: var(--bb-tier-free-border);
  }
  .tiers :global(.tier-paid.is-on) {
    color: var(--bb-tier-paid);
    background: var(--bb-tier-paid-bg);
    border-color: var(--bb-tier-paid-border);
  }
  .tiers :global(.tier-vip.is-on) {
    color: var(--bb-tier-vip);
    background: var(--bb-tier-vip-bg);
    border-color: var(--bb-tier-vip-border);
  }

  .facts {
    display: flex;
    flex-direction: column;
    gap: 7px;
    margin: 2px 0 0;
  }
  .facts div {
    display: flex;
    justify-content: space-between;
    gap: 12px;
    align-items: baseline;
  }
  .facts dt {
    font-family: var(--bb-font-body);
    font-size: 12px;
    color: var(--bb-muted);
  }
  .facts dd {
    margin: 0;
    font-family: var(--bb-font-mono);
    font-size: 11.5px;
    color: var(--bb-tan-light);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .facts dd.warn {
    color: var(--bb-status-error);
  }

  .probe {
    display: flex;
    align-items: center;
    gap: 9px;
    margin: 0;
    font-family: var(--bb-font-body);
    font-size: 13px;
    color: var(--bb-white);
  }
  .probe-err {
    font-family: var(--bb-font-mono);
    font-size: 11px;
    color: var(--bb-status-error);
    word-break: break-word;
    margin: 0;
  }

  .acts {
    display: flex;
    gap: 8px;
    flex-wrap: wrap;
  }

  .inline {
    display: flex;
    gap: 8px;
  }
  .text-input {
    flex: 1;
    min-width: 0;
    padding: 8px 11px;
    font-family: var(--bb-font-mono);
    font-size: 12.5px;
    border: 1px solid var(--bb-border);
    border-radius: var(--bb-radius-sm);
    background: var(--bb-bg-1, #16130f);
    color: var(--bb-white);
  }
  .text-input:focus {
    outline: none;
    border-color: var(--bb-border-strong);
  }

  .hist {
    display: flex;
    flex-direction: column;
    gap: 7px;
  }
  .hist-row {
    display: flex;
    align-items: center;
    gap: 9px;
    min-width: 0;
  }
  .hist-act {
    font-family: var(--bb-font-mono);
    font-size: 11.5px;
    color: var(--bb-white);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    flex: 1;
  }
  .hist-when {
    font-family: var(--bb-font-mono);
    font-size: 10px;
    color: var(--bb-muted);
    white-space: nowrap;
  }

  .danger {
    padding-top: 14px;
    border-top: 1px solid var(--bb-border);
  }
</style>
