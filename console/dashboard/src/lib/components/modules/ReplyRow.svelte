<script lang="ts">
  // One reply slot in a module's page — the module twin of CommandRow. Selecting
  // it loads the message into the docked builder inspector; the row owns the
  // per-reply on/off toggle (when the reply has one). The page passes the toggle
  // handler so all optimistic state stays in one place.
  import { SaveStatus, getI18n, type ModuleReply } from '@bagel/shared';
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
  // Blank shows the default the bot actually posts.
  const preview = $derived(message.trim() ? message : reply.defaultMessage);
  const off = $derived(enabled === false);

  function rowKey(e: KeyboardEvent) {
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault();
      onExpand();
    }
  }
</script>

<div
  class="row-shell reveal {expanded ? 'selected' : ''} {off ? 'off' : ''}"
  class:flash-save={status === 'saved'}
  style="--i: {(index ?? 1) - 1}"
>
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div class="trow" role="button" tabindex="0" aria-pressed={expanded} onclick={onExpand} onkeydown={rowKey}>
    {#if idx}<span class="idx" aria-hidden="true">{idx}</span>{/if}
    <span class="cmd">
      <span class="cmd-name">{reply.label}</span>
      <span class="resp">{preview}</span>
    </span>
    <span class="state"><SaveStatus state={status} /></span>
    {#if enabled !== undefined}
      <!-- svelte-ignore a11y_click_events_have_key_events a11y_no_static_element_interactions -->
      <span class="row-act" onclick={(e) => e.stopPropagation()}>
        <button
          class="toggle {enabled ? 'on' : ''}"
          type="button"
          aria-label={t('modules.toggleAria', { label: reply.label })}
          onclick={onToggle}
        ></button>
      </span>
    {:else}
      <span class="mini-spacer" aria-hidden="true"></span>
    {/if}
  </div>
</div>

<style>
  .row-shell {
    border-bottom: 1px solid var(--rule, rgba(240, 236, 228, 0.08));
    transition: background var(--bb-dur-fast, 140ms) ease, border-color var(--bb-dur-fast, 140ms) ease;
  }
  .row-shell.selected {
    background: rgba(201, 168, 124, 0.05);
  }
  .row-shell.off .trow > :not(.row-act) { opacity: 0.5; }

  .trow {
    display: grid;
    grid-template-columns: 28px minmax(0, 1fr) auto auto;
    align-items: center;
    gap: 14px;
    padding: 13px 14px;
    cursor: pointer;
    user-select: none;
  }
  .trow:hover { background: rgba(201, 168, 124, 0.045); }
  .trow:focus-visible { outline: 1px solid var(--bb-tan, #c9a87c); outline-offset: -1px; }

  .idx { font-family: var(--bb-font-mono); font-size: 10px; color: var(--bb-muted); opacity: 0.55; }
  .row-shell.selected .idx { color: var(--bb-tan); opacity: 1; }

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
  .row-act { display: inline-flex; align-items: center; }
  .mini-spacer { width: 42px; }

  @media (max-width: 760px) {
    .trow { grid-template-columns: minmax(0, 1fr) auto auto; }
    .idx { display: none; }
    .state { display: none; }
    .row-act { min-width: 44px; min-height: 44px; display: inline-flex; align-items: center; justify-content: center; }
  }
</style>
