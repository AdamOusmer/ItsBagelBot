import type { IconName } from './icons';
// Wire types mirroring the Go NATS RPC contracts (JSON over core NATS).
export type Perm = 'everyone' | 'sub' | 'vip' | 'mod' | 'lead_mod' | 'broadcaster';
export type Tier = 'premium' | 'standard';
export type Role = 'streamer' | 'mod';

// Ordered low -> high privilege; drives the access <select> in the dashboard.
export const PERMS: readonly Perm[] = ['everyone', 'sub', 'vip', 'mod', 'lead_mod', 'broadcaster'];
export const PERM_LABELS: Record<Perm, string> = {
  everyone: 'Everyone',
  sub: 'Subscribers',
  vip: 'VIPs',
  mod: 'Moderators',
  lead_mod: 'Lead moderators',
  broadcaster: 'Broadcaster'
};

export interface CommandView {
  name: string;
  // Alternate names the command also answers to in chat.
  aliases?: string[];
  response: string;
  is_active: boolean;
  stream_online_only?: boolean;
  perm?: Perm;
  // Cooldown in seconds; 0 or undefined means no cooldown.
  cooldown?: number;
  // Twitch id of the only user allowed to run the command; '' or undefined = unrestricted.
  allowed_user_id?: string;
  // Lifetime execution counter. The backend sends a number; older sample data
  // used human-formatted strings ('1.2k'), so both are accepted for display.
  uses?: number | string;
  // When true this is a built-in command: its behavior is baked into the bot,
  // it has no editable response, and its on/off state is stored in the modules
  // service (not the commands service). The dashboard renders it read-only with
  // a toggle + preview. See BUILTIN_COMMANDS.
  builtin?: boolean;
}

// --- Built-in command catalog --------------------------------------------
// Built-in commands are behaviors baked into the bot (not user text). They show
// on the commands page alongside custom commands, flagged builtin, but they
// cannot be renamed, deleted, or given a custom response — only toggled on/off.
// Their per-user on/off state lives in the modules service under `id` (a missing
// row means defaultActive). Adding one is a row here + the matching sesame
// built-in module. They are deliberately NOT in MODULE_CATALOG (never shown on
// the modules page).

export interface BuiltinCommandDef {
  // id is both the chat trigger and the modules-service key for the toggle.
  id: string;
  label: string;
  // summary is shown in the command row where a custom command shows its
  // response (built-ins have no response).
  summary: string;
  description: string; // longer copy for the inspector
  // usage lists example invocations shown in the inspector.
  usage: string[];
  // preview is the bot REPLY template, rendered through ChatPreview so the
  // inspector shows the same rehearsal as a custom command (viewer line + the
  // ItsBagelBot name/logo). previewArgs is what the viewer types after the
  // trigger; previewSamples fills tokens ChatPreview does not know by default.
  preview: string;
  previewArgs?: string;
  previewSamples?: Record<string, string>;
  defaultActive: boolean;
  defaultPerm: Perm;
  defaultCooldown: number; // seconds
  // liveOnly commands run only while the broadcaster is streaming.
  liveOnly: boolean;
  // editable: the reply template can be customized on the dashboard. When true
  // the inspector shows a ResponseEditor (with the `tokens` palette) and a
  // rehearsal, and saves the template into the modules-service config under
  // `replyKey`. The bot expands the tokens when it posts the reply (e.g. {clip}
  // → the clip URL, resolved by outgress once the clip exists). Non-editable
  // built-ins stay a read-only preview. `preview` doubles as the default
  // template when no custom reply is set.
  editable?: boolean;
  // replyKey is the Configs key the custom reply template is stored under (only
  // meaningful when editable).
  replyKey?: string;
  // tokens is the reply editor's insert palette (token names without braces).
  tokens?: string[];
}

export const BUILTIN_COMMANDS: readonly BuiltinCommandDef[] = [
  {
    id: 'clip',
    label: 'Clip',
    summary: 'Built-in · clips the last moments of the stream and posts the link.',
    description:
      'Viewers create a clip of the recent stream and the bot replies in chat with the clip link. Add an optional title after the command. Only works while you are live.',
    usage: ['!clip', '!clip <title>'],
    // Real reply format: "<clipper> clipped: <title> → <url>" (see
    // app/outgress/internal/worker clipReplyText). {user} = the clipper, {target}
    // = the title argument (standard command token).
    preview: '{user} clipped: {target} → {clip}',
    previewArgs: 'That is amazing',
    previewSamples: { target: 'That is amazing', clip: 'clips.twitch.tv/AbCdEf' },
    defaultActive: true,
    defaultPerm: 'everyone',
    defaultCooldown: 15,
    liveOnly: true,
    // The reply is customizable: {clip} is the clip link, {user} the clipper,
    // {target} the title the viewer typed. Stored under the "reply" config key,
    // read by sesame and expanded by outgress (see app/sesame/modules/clip.go).
    editable: true,
    replyKey: 'reply',
    tokens: ['clip', 'user', 'target']
  }
];

export function builtinDef(id: string): BuiltinCommandDef | undefined {
  return BUILTIN_COMMANDS.find((b) => b.id === id);
}

export const BUILTIN_NAMES: ReadonlySet<string> = new Set(BUILTIN_COMMANDS.map((b) => b.id));

export interface AdminUser {
  user_id: string;
  username: string;
  display_name?: string;
  status?: string;
}

export interface UserStats {
  total_users: number;
  active_users: number;
  premium_users: number;
  vip_users: number;
  paid_users: number;
}

export interface Shard {
  shard_id: number;
  state: string;
  node: string;
  // Worker node (machine) name the shard runs on. Falls back to the host part
  // of `node` when unset (e.g. local dev without the downward-API env).
  host?: string;
  session_id?: string;
  bound: boolean;
  handshake_in_flight?: boolean;
  keepalive_ms?: number;
  attempts?: number;
  load?: number;
}

export interface ShardSnapshot {
  generated_at: string;
  reporter: string;
  nodes: string[];
  shard_count: number;
  conduit_manager?: { state: string; node: string; conduit_id?: string };
  shards: Shard[];
  desired_count: number;
  target: number;
  min_shards: number;
  autoscale: boolean;
}

export interface NavLink {
  href: string;
  icon: IconName;
  label: string;
  active?: boolean;
  locked?: boolean;
  count?: string | number;
}

export interface NavGroupDef {
  label?: string;
  items: NavLink[];
}

// A dashboard the signed-in user has been granted access to (a delegation
// received). Rendered in the topbar account menu as a quick-switch link into
// the owner's board via /delegate/enter.
export interface DashboardLink {
  // Full href to enter the board, e.g. /delegate/enter?owner=<id>.
  href: string;
  // Owner's Twitch login, shown as the row name + gradient-badge initial.
  name: string;
}

// --- Module catalog -------------------------------------------------------
// The user-facing modules the dashboard lets a broadcaster toggle/configure.
// Core, hidden modules (the command processor, the live tracker, and the system
// module that owns !ping/!itsbagelbot and the bagel greeting) are deliberately
// NOT listed here: they are always on and never shown.

export type ModuleFieldType = 'text' | 'textarea' | 'number' | 'select' | 'toggle';

export interface ModuleField {
  // key is the JSON property written into the module's Configs blob.
  key: string;
  label: string;
  type: ModuleFieldType;
  placeholder?: string;
  help?: string;
  // options drive a 'select' field.
  options?: { value: string; label: string }[];
  // followsLevel marks a 'toggle' whose unset state follows the module's
  // "level" select (see automodToggleDefault): the blob only stores an
  // explicit "on"/"off" once the user flips it.
  followsLevel?: boolean;
}

// Mirrors levelSections in app/sesame/automod/config.go: which automod sections
// each level preset enables. Renders the resting state of a follows-level
// toggle; the authoritative resolution happens in Go.
export const AUTOMOD_LEVEL_DEFAULTS: Record<string, Record<string, boolean>> = {
  none: { harassment: false, sexual: false, profanity: false, style: false, links: false },
  basic: { harassment: true, sexual: false, profanity: false, style: false, links: false },
  moderate: { harassment: true, sexual: true, profanity: false, style: true, links: true },
  strict: { harassment: true, sexual: true, profanity: true, style: true, links: true }
};

// automodToggleDefault resolves a follows-level toggle's resting state for the
// currently selected level.
export function automodToggleDefault(level: string, key: string): boolean {
  return (AUTOMOD_LEVEL_DEFAULTS[level] ?? AUTOMOD_LEVEL_DEFAULTS.moderate)[key] ?? false;
}

// One chat line a module can post, rendered as a row on the module page. Clicking
// the row opens the exact same builder as a custom command's response (the shared
// ResponseEditor + ChatPreview, standard {user}/{target}/… tokens). messageKey/
// enableKey are the Configs JSON keys the matching sesame module reads (see
// app/sesame/modules).
export interface ModuleReply {
  key: string; // stable row id
  label: string; // 'Follow alert'
  tagline: string; // short row description
  // Preview context: what fires this line, e.g. 'on follow'.
  event: string;
  messageKey: string; // Configs key holding the template
  // Configs key for this reply's own on/off toggle; omit when the reply has no
  // per-reply switch (it fires whenever the module is on). Stored "on"/"off";
  // empty/absent means on, matching sesame's alertOn semantics.
  enableKey?: string;
  defaultMessage: string; // sesame default template (placeholder + preview fallback)

  // --- command-style replies (gateway modules: urchin, mcsr) ---------------
  // command is the chat trigger without '!' (e.g. 'daily'). When set, the
  // inspector rehearses the reply exactly like a custom command: the border
  // reads "Chat rehearsal", a viewer line types the trigger, and the bot
  // answers with previewSamples substituted into the template.
  command?: string;
  // previewArgs is what the sample viewer types after the trigger.
  previewArgs?: string;
  // previewSamples maps this reply's tokens to sample values. When command is
  // set the preview substitutes ONLY these (samplesOnly): sesame expands only
  // the module's own tokens here, so the generic {user}/{uptime} samples would
  // preview values the bot will never produce.
  previewSamples?: Record<string, string>;
  // tokens is the editor's insert palette (without braces), replacing the
  // default command tokens with the ones this reply actually supports.
  tokens?: string[];
}

export interface ModuleDef {
  // id is the ModuleView.name key in the modules service.
  id: string;
  label: string;
  tagline: string; // one-liner for the tile
  description: string; // longer copy for the module page
  icon: IconName;
  category: string;
  defaultEnabled: boolean;
  // The module's configurable chat lines (the "commands" of the module page).
  replies: ModuleReply[];
  // Plain non-reply settings (rendered in the settings strip). Optional; the
  // current modules have none beyond their master enable + per-reply toggles.
  settings?: ModuleField[];
}

// A module's current state as shown on the dashboard: catalog metadata merged
// with the broadcaster's stored row.
export interface ModuleState {
  def: ModuleDef;
  enabled: boolean;
  config: Record<string, string>;
}

// Shared token palette + preview samples for the Bedwars session commands
// (!daily / !weekly / !monthly) — same template surface, one source of truth.
const BW_SESSION_TOKENS = [
  'player',
  'wins',
  'losses',
  'finals',
  'finaldeaths',
  'beds',
  'games',
  'levels',
  'fkdr'
];
const BW_SESSION_SAMPLES: Record<string, string> = {
  player: 'Technoblade',
  wins: '5',
  losses: '2',
  finals: '21',
  finaldeaths: '3',
  beds: '9',
  games: '8',
  levels: '1',
  fkdr: '7.00'
};

export const MODULE_CATALOG: readonly ModuleDef[] = [
  {
    id: 'automod',
    label: 'AutoMod',
    tagline: 'Catch scams, IP-grabbers and raid spam before your mods do.',
    description:
      'The bot screens every chat line for harmful content and coordinated raid floods, and warns, deletes, times out or bans the sender. Trusted chatters (VIPs, mods, the broadcaster) are always exempt, and anything borderline is left to your human mods. Pick a level from None to All, then fine-tune each check below. The safety floor (hate slurs and IP-grabber links) is always enforced, on every level and even with the module off: hosting those risks your channel and the bot account platform-wide. Everything else is your call.',
    icon: 'moderation',
    category: 'Moderation',
    defaultEnabled: true,
    // AutoMod is pure configuration: no chat reply lines, only the settings strip.
    replies: [],
    settings: [
      {
        key: 'level',
        label: 'Enforcement level',
        type: 'select',
        placeholder: 'moderate',
        options: [
          { value: 'none', label: 'None - safety floor only' },
          { value: 'basic', label: 'Basic - floor + harassment' },
          { value: 'moderate', label: 'Moderate - recommended (default)' },
          { value: 'strict', label: 'All - every check, family-strict' }
        ],
        help: 'Sets the default for every check below. The safety floor applies at every level.'
      },
      {
        key: 'harassment',
        label: 'Harassment',
        type: 'toggle',
        followsLevel: true,
        help: 'Directed harm ("kys" and friends): warns the sender and removes the message; repeat offenders are timed out, then banned.'
      },
      {
        key: 'sexual',
        label: 'Sexual content',
        type: 'toggle',
        followsLevel: true,
        help: 'Removes messages with explicit sexual terms.'
      },
      {
        key: 'profanity',
        label: 'Profanity',
        type: 'toggle',
        followsLevel: true,
        help: 'Removes plain swearing. Off by default: most channels allow it.'
      },
      {
        key: 'style',
        label: 'Caps & symbol spam',
        type: 'toggle',
        followsLevel: true,
        help: 'Removes shouting, symbol walls and character floods. Emote walls (KEKW spam) are recognized and never flagged.'
      },
      {
        key: 'links',
        label: 'Link-spam radar',
        type: 'toggle',
        followsLevel: true,
        help: 'Watches for the same link template posted by many different accounts and removes the wave. Single links are never touched.'
      },
      {
        key: 'block_terms',
        label: 'Blocked terms',
        type: 'textarea',
        placeholder: 'term one, term two',
        help: 'Extra words or phrases to flag in your channel. Separate with commas or new lines. Matched even through obfuscation (l33t, look-alike letters).'
      },
      {
        key: 'allow_terms',
        label: 'Allowed terms',
        type: 'textarea',
        placeholder: '',
        help: 'Words that are fine in your channel: a line containing one is never flagged by the checks above or your blocked terms. Cannot override the safety floor.'
      }
    ]
  },
  {
    id: 'shoutout',
    label: 'Auto Shoutout',
    tagline: 'Welcome incoming raids with an automatic shoutout.',
    description:
      'When another channel raids in, the bot posts a shoutout pointing your chat at the raider. Turn the module on and customize the shoutout line.',
    icon: 'send',
    category: 'Community',
    defaultEnabled: false,
    replies: [
      {
        key: 'shoutout',
        label: 'Raid shoutout',
        tagline: 'Automated chat shoutout when raided',
        event: 'on raid',
        messageKey: 'message',
        defaultMessage:
          'Massive shoutout to {raider} for the raid with {viewers} viewers! Check them out at twitch.tv/{raider.login}'
      }
    ],
    settings: [
      {
        key: 'native_shoutout',
        label: 'Also send Twitch shoutout',
        type: 'toggle',
        help: "Fires Twitch's own /shoutout on the raider alongside the chat line, which shows their current category and profile card natively. Off by default."
      }
    ]
  },
  {
    id: 'alerts',
    label: 'Chat Alerts',
    tagline: 'Announce follows, subs, cheers and raids in chat.',
    description:
      'The bot posts a chat line when someone follows, subscribes, cheers, or raids. Turn each alert on or off and customize its message. New alerts default on.',
    icon: 'bell',
    category: 'Community',
    defaultEnabled: true,
    replies: [
      {
        key: 'follow',
        label: 'Follow alert',
        tagline: 'When someone follows your channel.',
        event: 'on follow',
        enableKey: 'followEnabled',
        messageKey: 'followMessage',
        defaultMessage: 'Thank you for following the channel, {user}!'
      },
      {
        key: 'sub',
        label: 'Subscribe alert',
        tagline: 'When someone subscribes.',
        event: 'on subscribe',
        enableKey: 'subEnabled',
        messageKey: 'subMessage',
        defaultMessage: 'Welcome to the community, {user}! Thank you for subscribing!'
      },
      {
        key: 'cheer',
        label: 'Cheer alert',
        tagline: 'When someone cheers bits.',
        event: 'on cheer',
        enableKey: 'cheerEnabled',
        messageKey: 'cheerMessage',
        defaultMessage: 'Thank you for the {bits} bits, {user}!'
      },
      {
        key: 'raid',
        label: 'Raid alert',
        tagline: 'When another channel raids in.',
        event: 'on raid',
        enableKey: 'raidEnabled',
        messageKey: 'raidMessage',
        defaultMessage: '{user} is raiding the channel with {viewers} viewers! Welcome everyone!'
      }
    ]
  },
  // External-stats modules: chat commands answered through the gateway service
  // (external API proxy + cache). Config keys must match the sesame module
  // structs (app/sesame/modules/urchin.go, mcsr.go).
  {
    id: 'urchin',
    label: 'Bedwars Stats',
    tagline: 'Hypixel Bedwars stats, urchin score and blacklist tags in chat.',
    description:
      'Viewer commands backed by urchin.gg: daily, weekly and monthly Bedwars sessions, lifetime stats, the Urchin sniper score and active blacklist tags. Commands default to your linked Minecraft account; viewers can also name any player, e.g. "!daily Technoblade".',
    icon: 'pulse',
    category: 'Games',
    defaultEnabled: false,
    replies: [
      {
        key: 'daily',
        label: '!daily',
        tagline: 'Bedwars session since the daily reset.',
        event: '!daily',
        command: 'daily',
        enableKey: 'dailyEnabled',
        messageKey: 'dailyMessage',
        defaultMessage: '{player} today: {wins}W {losses}L · {finals} finals · {beds} beds · {fkdr} FKDR',
        tokens: BW_SESSION_TOKENS,
        previewSamples: BW_SESSION_SAMPLES
      },
      {
        key: 'weekly',
        label: '!weekly',
        tagline: 'Bedwars session since the weekly reset.',
        event: '!weekly',
        command: 'weekly',
        enableKey: 'weeklyEnabled',
        messageKey: 'weeklyMessage',
        defaultMessage: '{player} this week: {wins}W {losses}L · {finals} finals · {beds} beds · {fkdr} FKDR',
        tokens: BW_SESSION_TOKENS,
        previewSamples: BW_SESSION_SAMPLES
      },
      {
        key: 'monthly',
        label: '!monthly',
        tagline: 'Bedwars session since the monthly reset.',
        event: '!monthly',
        command: 'monthly',
        enableKey: 'monthlyEnabled',
        messageKey: 'monthlyMessage',
        defaultMessage: '{player} this month: {wins}W {losses}L · {finals} finals · {beds} beds · {fkdr} FKDR',
        tokens: BW_SESSION_TOKENS,
        previewSamples: BW_SESSION_SAMPLES
      },
      {
        key: 'stats',
        label: '!bwstats',
        tagline: 'Lifetime Bedwars stats.',
        event: '!bwstats',
        command: 'bwstats',
        enableKey: 'statsEnabled',
        messageKey: 'statsMessage',
        defaultMessage: '{player}: {stars} stars · {wins} wins · {finals} finals · {fkdr} FKDR · {beds} beds broken',
        tokens: ['player', 'stars', 'wins', 'losses', 'finals', 'finaldeaths', 'beds', 'fkdr', 'wlr'],
        previewSamples: {
          player: 'Technoblade',
          stars: '402',
          wins: '1000',
          losses: '100',
          finals: '5000',
          finaldeaths: '500',
          beds: '2000',
          fkdr: '10.00',
          wlr: '10.00'
        }
      },
      {
        key: 'sniper',
        label: '!sniper',
        tagline: 'Urchin (Cubelify overlay) score.',
        event: '!sniper',
        command: 'sniper',
        enableKey: 'sniperEnabled',
        messageKey: 'sniperMessage',
        defaultMessage: '{player} urchin score: {score}',
        tokens: ['player', 'score', 'mode', 'tagcount'],
        previewSamples: { player: 'Technoblade', score: '7.5', mode: 'warn', tagcount: '1' }
      },
      {
        key: 'tags',
        label: '!tag',
        tagline: 'Active Urchin blacklist tags.',
        event: '!tag',
        command: 'tag',
        enableKey: 'tagsEnabled',
        messageKey: 'tagsMessage',
        defaultMessage: '{player}: {tags}',
        tokens: ['player', 'tags', 'tagcount'],
        previewSamples: {
          player: 'Technoblade',
          tags: 'Blatant Cheater (added Jul 3, 2024)',
          tagcount: '1'
        }
      },
      {
        key: 'tagdescription',
        label: '!tagdescription',
        tagline: 'Blacklist tags with their reasons.',
        event: '!tagdescription',
        command: 'tagdescription',
        enableKey: 'tagDescriptionEnabled',
        messageKey: 'tagDescriptionMessage',
        defaultMessage: '{player}: {tags}',
        tokens: ['player', 'tags', 'tagcount'],
        previewSamples: {
          player: 'Technoblade',
          tags: 'Blatant Cheater (bhop - added Jul 3, 2024)',
          tagcount: '1'
        }
      }
    ],
    settings: [
      {
        key: 'account',
        label: 'Linked Minecraft account',
        type: 'text',
        placeholder: 'Your Minecraft username',
        help: 'Default player for every command. Leave blank to use your Twitch username.'
      }
    ]
  },
  {
    id: 'mcsr',
    label: 'MCSR Ranked',
    tagline: 'Ranked elo and per-stream session stats for MCSR runners.',
    description:
      'Viewer commands backed by the MCSR Ranked API: !elo shows the current rating and season record; !session shows elo and wins/losses since the stream started, snapshotting your standing the moment you go live. !elo can name any player (e.g. "!elo Feinberg"); !session always tracks your linked account.',
    icon: 'clock',
    category: 'Games',
    defaultEnabled: false,
    replies: [
      {
        key: 'elo',
        label: '!elo',
        tagline: 'Current elo, rank and season record.',
        event: '!elo',
        command: 'elo',
        enableKey: 'eloEnabled',
        messageKey: 'eloMessage',
        defaultMessage: '{player}: {elo} elo · rank #{rank} · {wins}W {losses}L this season',
        tokens: ['player', 'elo', 'rank', 'wins', 'losses', 'matches', 'country'],
        previewSamples: {
          player: 'Feinberg',
          elo: '1650',
          rank: '12',
          wins: '40',
          losses: '20',
          matches: '61',
          country: 'us'
        }
      },
      {
        key: 'session',
        label: '!session',
        tagline: 'Elo and record since the stream started.',
        event: '!session',
        command: 'session',
        enableKey: 'sessionEnabled',
        messageKey: 'sessionMessage',
        defaultMessage: '{player} this stream: {elochange} elo ({elo} now) · {wins}W {losses}L in {matches} matches',
        tokens: ['player', 'elo', 'elochange', 'wins', 'losses', 'matches'],
        previewSamples: {
          player: 'Feinberg',
          elo: '1660',
          elochange: '+24',
          wins: '3',
          losses: '1',
          matches: '4'
        }
      }
    ],
    settings: [
      {
        key: 'account',
        label: 'Linked Minecraft account',
        type: 'text',
        placeholder: 'Your Minecraft username',
        help: 'Default player for every command. Leave blank to use your Twitch username.'
      }
    ]
  }
];

export function moduleDef(id: string): ModuleDef | undefined {
  return MODULE_CATALOG.find((m) => m.id === id);
}

// --- Channel points -------------------------------------------------------
// The Channel Points tab (its own dashboard section, NOT a module tile) lets a
// broadcaster create Twitch custom rewards (created under the bot's client id,
// styled natively on Twitch) and bind each one to a bot action that runs when a
// viewer redeems it. The Twitch-side reward is owned by outgress (broadcaster
// token); the action binding is stored in the hidden "channelpoints" module blob
// and read by sesame's channelpoints module.

// The bot action a redemption triggers. 'chat' posts the reward's message;
// 'none' manages the reward only, leaving just the resolution policy to act.
export type RewardActionKind = 'chat' | 'none';
// What to do with the redemption in Twitch's request queue after the action:
// mark it fulfilled, cancel (refund the points), or leave it for a human mod.
export type RewardOnRedeem = 'fulfill' | 'cancel' | 'leave';

export const REWARD_ACTIONS: readonly RewardActionKind[] = ['chat', 'none'];
export const REWARD_ON_REDEEM: readonly RewardOnRedeem[] = ['fulfill', 'cancel', 'leave'];

// One channel-points reward as the dashboard works with it: the Twitch reward
// fields plus the local action binding, merged into a single row.
export interface ChannelPointReward {
  // id is the Twitch-assigned custom reward id; empty only on an unsaved draft.
  id: string;
  title: string;
  cost: number;
  prompt: string;
  backgroundColor: string;
  isEnabled: boolean;
  isPaused: boolean;
  isUserInputRequired: boolean;
  // Limit controls ("claimable once and so on").
  maxPerStreamEnabled: boolean;
  maxPerStream: number;
  maxPerUserPerStreamEnabled: boolean;
  maxPerUserPerStream: number;
  globalCooldownEnabled: boolean;
  globalCooldownSeconds: number;
  // Local action binding (stored in the channelpoints module blob, read by sesame).
  action: RewardActionKind;
  message: string;
  onRedeem: RewardOnRedeem;
}

// blankReward is the default draft for the "new reward" form.
export function blankReward(): ChannelPointReward {
  return {
    id: '',
    title: '',
    cost: 100,
    prompt: '',
    backgroundColor: '',
    isEnabled: true,
    isPaused: false,
    isUserInputRequired: false,
    maxPerStreamEnabled: false,
    maxPerStream: 1,
    maxPerUserPerStreamEnabled: false,
    maxPerUserPerStream: 1,
    globalCooldownEnabled: false,
    globalCooldownSeconds: 60,
    action: 'chat',
    message: '',
    onRedeem: 'fulfill'
  };
}

// One repeating chat message: stream-only (armed on stream.online, stopped on
// stream.offline; see sesame's ValkeyTimerStore). No Twitch-side entity, so
// unlike ChannelPointReward there is nothing to CRUD but this blob's own id.
export interface TimerDef {
  // id is a dashboard-generated id; empty only on an unsaved draft.
  id: string;
  message: string;
  intervalSeconds: number;
  enabled: boolean;
}

// blankTimer is the default draft for the "new timer" form.
export function blankTimer(): TimerDef {
  return { id: '', message: '', intervalSeconds: 600, enabled: true };
}
