<script lang="ts">
  import { Icon, Toggle } from '@bagel/shared';
  import type { IconName } from '@bagel/shared';

  const rules: { icon: IconName; name: string; desc: string; on: boolean; conf: string[] }[] = [
    { icon: 'caps', name: 'Caps filter', desc: 'Times out messages that are mostly uppercase shouting.', on: true, conf: ['≥ 70% caps', 'min 10 chars', '60s timeout'] },
    { icon: 'link', name: 'Link protection', desc: 'Holds links from non-subs for review or auto-delete.', on: true, conf: ['non-subs', 'auto-delete', 'allow clips'] },
    { icon: 'pulse', name: 'Spam & repetition', desc: 'Catches copypasta and repeated-character floods.', on: true, conf: ['≥ 6 repeats', 'warn first', 'then 30s'] },
    { icon: 'blocked', name: 'Blocked terms', desc: '42 phrases on the deny-list, including regex patterns.', on: true, conf: ['42 terms', 'regex on', 'instant ban'] },
    { icon: 'symbol', name: 'Symbol & zalgo', desc: 'Strips combining-character spam and excessive symbols.', on: false, conf: ['≥ 8 symbols', 'delete only'] },
    { icon: 'follower', name: 'Follower-only mode', desc: 'Restricts chat to accounts following for 10+ minutes.', on: false, conf: ['10 min', 'off during raids'] }
  ];

  const log: { icon: IconName; red: boolean; t: string; d: string; w: string; by: string }[] = [
    { icon: 'clock', red: false, t: 'Timeout · 60s', d: 'Caps filter on <code>LOUDGUY99</code>', w: '8m ago', by: 'automod' },
    { icon: 'trash', red: false, t: 'Message deleted', d: 'Blocked term from <code>spam_acct_x</code>', w: '15m ago', by: 'automod' },
    { icon: 'ban', red: true, t: 'Banned', d: '<code>bot_follower_429</code> · suspected bot', w: '26m ago', by: '@kettle' },
    { icon: 'clock', red: false, t: 'Timeout · 600s', d: 'Repeated link spam · <code>dropshipDan</code>', w: '41m ago', by: 'ItsMavey' },
    { icon: 'trash', red: false, t: 'Message deleted', d: 'Link held for non-sub <code>newbie_22</code>', w: '1h ago', by: 'automod' },
    { icon: 'clock', red: false, t: 'Timeout · 30s', d: 'Copypasta flood · <code>emote_gremlin</code>', w: '1h ago', by: 'automod' }
  ];
</script>

<section class="screen active">
  <div class="page-head">
    <span class="eyebrow">Configure</span>
    <h1>Bot <em>modules</em></h1>
    <p>Toggle features on or off for #itsmavey. Each module runs independently.</p>
  </div>

  <div class="section-title-sm">Available modules</div>
  <div class="mod-grid">
    {#each rules as r}
      <div class="card rule">
        <div class="ri"><Icon name={r.icon} size={19} /></div>
        <div class="rb">
          <b>{r.name}</b>
          <p>{r.desc}</p>
          <div class="conf">{#each r.conf as t}<span class="tag">{t}</span>{/each}</div>
        </div>
        <div class="rt"><Toggle on={r.on} /></div>
      </div>
    {/each}
  </div>

  <div class="section-title-sm">Recent moderation actions</div>
  <div class="card">
    <div class="modlog">
      {#each log as l}
        <div class="log-row">
          <div class="li {l.red ? 'red' : ''}"><Icon name={l.icon} size={14} /></div>
          <div class="lt"><b>{l.t}</b><span>{@html l.d}</span></div>
          <div class="lw">{l.w}<span class="by">{l.by}</span></div>
        </div>
      {/each}
    </div>
  </div>
</section>

<style>
  /* Toggle hit area: wrap in a label-like region so touch target >= 44px */
  :global(.rule .rt) {
    display: flex;
    align-items: center;
    justify-content: center;
    min-width: 44px;
    min-height: 44px;
  }

  @media (max-width: 760px) {
    /* mod-grid already drops to 1-col via shared CSS at 1100px; ensure no overflow */
    :global(.mod-grid) {
      gap: 10px;
    }

    /* rule card: tighten layout, tags wrap cleanly */
    :global(.rule) {
      gap: 12px;
    }

    :global(.rule .rb .conf) {
      gap: 6px;
    }

    /* log-row: hide the right-column timestamp on very narrow screens */
    .lw {
      display: none;
    }

    /* modlog rows: two-column (icon + text) at 380px */
    :global(.modlog .log-row) {
      grid-template-columns: 30px 1fr;
    }
  }
</style>
