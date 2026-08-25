<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  // Chat-command reference: a code chip, its description, and an optional
  // "mod only" pill, one per row. Pulled out of songqueue's page-local
  // `.cmd-help` block so any module documenting its own chat commands can
  // reuse it instead of forking the markup again.
  let {
    title,
    commands,
    modLabel
  }: {
    title?: string;
    commands: { cmd: string; desc: string; mod?: boolean }[];
    modLabel?: string;
  } = $props();
</script>

<div class="cmd-help">
  {#if title}<p class="cmd-help-title">{title}</p>{/if}
  <ul class="cmd-list">
    {#each commands as row (row.cmd)}
      <li>
        <span class="cmd-head">
          <code>{row.cmd}</code>
          {#if row.mod && modLabel}<span class="cmd-mod">{modLabel}</span>{/if}
        </span>
        <span class="cmd-desc">{row.desc}</span>
      </li>
    {/each}
  </ul>
</div>

<style>
  .cmd-help { margin-top: 18px; padding-top: 14px; border-top: 1px solid var(--rule, var(--bb-border)); }
  .cmd-help-title {
    margin: 0 0 8px;
    font-family: var(--bb-font-body);
    font-size: 10.5px;
    letter-spacing: 0.05em;
    text-transform: uppercase;
    color: var(--bb-muted);
  }
  .cmd-list { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 6px; }
  .cmd-list li { display: flex; align-items: baseline; gap: 8px; flex-wrap: wrap; }
  /* The chip and its mod pill travel together, so the pill can never wrap
     onto a line of its own away from the command it qualifies. */
  .cmd-head { display: inline-flex; align-items: center; gap: 6px; flex: none; }
  .cmd-list code {
    flex: none;
    min-width: 9ch;
    font-family: var(--bb-font-mono);
    font-size: 11.5px;
    color: var(--bb-tan-light);
    background: rgba(201, 168, 124, 0.08);
    border: 1px solid rgba(201, 168, 124, 0.22);
    border-radius: 999px;
    padding: 2px 9px;
  }
  .cmd-desc { font-family: var(--bb-font-body); font-size: 12px; color: var(--bb-muted); }
  .cmd-mod {
    font-family: var(--bb-font-body);
    font-size: 10px;
    letter-spacing: 0.04em;
    text-transform: uppercase;
    color: var(--bb-green-glow, #52b788);
    border: 1px solid rgba(82, 183, 136, 0.35);
    border-radius: 999px;
    padding: 1px 7px;
  }

  /* Below 520px the chip row and its description stop sharing a baseline:
     the chip (with its pill) sits on its own line so long descriptions get
     the full row width instead of squeezing into whatever space wrap left
     behind. */
  @media (max-width: 520px) {
    .cmd-list li { flex-direction: column; align-items: flex-start; gap: 3px; }
    .cmd-list { gap: 10px; }
  }
</style>
