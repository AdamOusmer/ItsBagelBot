<script lang="ts">
  // One reply slot in a module's page, on the shared ManagementRow: the clickable
  // primary is a real button and the per-reply on/off switch is its sibling, not
  // nested inside it. The page passes the toggle handler so all optimistic state
  // stays in one place.
  import { SaveStatus, ManagementRow, Switch, getI18n, type ModuleReply } from '@bagel/shared';
  import type { SaveState } from '@bagel/shared/components/SaveStatus.svelte';

  const { t } = getI18n();

  let {
    reply,
    message = '',
    index = undefined as number | undefined,
    status = 'idle' as SaveState,
    expanded = false,
    enabled = undefined as boolean | undefined,
    onExpand,
    onToggle
  }: {
    reply: ModuleReply;
    message?: string;
    index?: number;
    status?: SaveState;
    expanded?: boolean;
    // undefined => the reply has no per-reply toggle (governed by the module enable).
    enabled?: boolean;
    onExpand: () => void;
    onToggle?: () => void;
  } = $props();

  const idx = $derived(index !== undefined ? String(index).padStart(2, '0') : '');
  const preview = $derived(message.trim() ? message : reply.defaultMessage);
</script>

<div class="row-wrap" class:flash-save={status === 'saved'}>
  <ManagementRow
    selected={expanded}
    {expanded}
    ariaLabel={reply.label}
    disabled={enabled === false}
    onselect={onExpand}
  >
    {#snippet primary()}
      <span class="prow">
        {#if idx}<span class="idx" aria-hidden="true">{idx}</span>{/if}
        <span class="cmd">
          <span class="cmd-name">{reply.label}</span>
          <span class="resp">{preview}</span>
        </span>
        <span class="state"><SaveStatus state={status} /></span>
      </span>
    {/snippet}
    {#snippet actions()}
      {#if enabled !== undefined}
        <Switch checked={enabled} label={t('modules.toggleAria', { label: reply.label })} onchange={() => onToggle?.()} />
      {:else}
        <span class="mini-spacer" aria-hidden="true"></span>
      {/if}
    {/snippet}
  </ManagementRow>
</div>

<style>
  .prow {
    display: grid;
    grid-template-columns: 28px minmax(0, 1fr) auto;
    align-items: center;
    gap: 14px;
  }
  .idx { font-family: var(--bb-font-mono); font-size: 10px; color: var(--bb-muted); opacity: 0.55; }

  .cmd { display: flex; flex-direction: column; gap: 3px; min-width: 0; }
  .cmd-name { font-family: var(--bb-font-display); font-weight: 700; font-size: 14px; color: var(--bb-white); }
  .resp {
    font-family: var(--bb-font-body);
    font-size: 12.5px;
    color: var(--bb-muted);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    min-width: 0;
  }
  .state { min-width: 0; }
  .mini-spacer { width: 38px; }

  @media (max-width: 760px) {
    .prow { grid-template-columns: minmax(0, 1fr) auto; }
    .idx { display: none; }
    .state { display: none; }
  }
</style>
