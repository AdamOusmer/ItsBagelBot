// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Command-builder catalog + UI copy, both locales. This file is the marketing
// site's single source of truth for what the bot actually expands, verified
// against the worker: custom-command tokens in app/twitch/sesame/engine/scope/
// (one file per scope; the old engine/vars.go switch this used to name was
// deleted when the scope chain replaced it), dynamic tokens in
// app/twitch/sesame/module/vars.go, module reply tokens in each
// app/twitch/sesame/modules/*.go, and limits in internal/domain/validate/validate.go
// (mirrored by web/kit/lib/engine/commands-validate.ts). If a token isn't
// expanded there, it doesn't belong here, the bot leaves unknown braces
// as literal text.

// Lang/defaultLang come from lang.ts, not ui.ts: ui.ts's module body runs
// import.meta.glob, which only Vite/Astro implement. bun test (the golden
// test in lib/variables/reference.golden.test.ts) evaluates this file
// directly, so the glob-bearing module must stay off this import path.
import { defaultLang, type Lang } from './lang';
import { SITE } from '@bagel/kit/site-links';
import { COMMAND_NAME_MAX, RESPONSE_MAX, RESPONSE_MAX_LINES, COOLDOWN_MAX } from '@bagel/kit/engine/commands-validate';
import { staticText } from '@bagel/kit/i18n/static';
import { VARIABLES as KIT_VARIABLES, variableById, type VariableDef, type VariableForm } from '@bagel/kit/variables';
import { moduleDef } from '@bagel/kit/catalog';
import { BUILTIN_COMMANDS } from '@bagel/kit/catalog/builtin-commands';
import type { ReplyToken } from '@bagel/kit/catalog/module-def';

type L10n = Record<Lang, string>;

/** One localized string, falling back to the default locale for a new language. */
const pick = (m: L10n, lang: Lang): string => m[lang] ?? m[defaultLang];

// Kit now owns the variable and reply-token manifest and its copy
// (docs/specs/variables-catalog.md D2/D3): this builder derives every var's
// name/desc from kit's locale catalogs instead of carrying its own. Astro
// renders this module at build time, and SURFACES below is a module-level
// constant holding BOTH locales at once (like the old per-family arrays it
// replaces). staticText (kit/lib/i18n/static.ts) reads its en/fr catalogs
// via a plain static import, so it is ready the moment this module runs,
// no top-level await or catalog-loading step needed: that used to sit here
// waiting on @bagel/kit/i18n's ensureCatalog(), a Vite-glob-backed lazy
// loader that also runs under bun's `astro build` fine but cannot run under
// plain `bun test` at all (`import.meta.glob is not a function`), which is
// what the marketing golden test (lib/variables/reference.golden.test.ts)
// needs.
const KIT_LOCALES = new Set(['en', 'fr']);

/** Resolve a kit locale key for a marketing Lang (an open string, unlike
 * kit's closed 'en' | 'fr' static-catalog set); falls back to English for
 * any other lang. */
export function kitText(lang: Lang, key: string): string {
  const locale = KIT_LOCALES.has(lang) ? (lang as 'en' | 'fr') : 'en';
  return staticText(locale, key);
}

/** Both locales of one kit key, for a builder record that (like the old
 * per-family arrays) holds every language at once. */
function l10n(key: string): L10n {
  return { en: kitText('en', key), fr: kitText('fr', key) };
}

/** One scope choice for a counter var's picker (see COUNTER_SCOPES below). */
interface ScopeDef {
  id: string;
  label: L10n;
  hint: L10n;
}

export interface VarDef {
  token: string;
  sample: string;
  name: L10n;
  desc: L10n;
  /** Counter vars only: the four scope choices shown in the builder's picker. */
  scopes?: ScopeDef[];
  /** Set only for a VarDef built from a kit VariableForm (kitVarDef): the
   * owning VariableDef's id, straight from the source, so the marketing
   * catalog (lib/variables/index.ts buildCatalog) can fold every form of one
   * variable onto its one record without re-deriving ownership from the
   * form's own lexer head. That re-derivation is exactly what broke on
   * positional's {:m} form: its head is "" (an empty name, not "positional"),
   * so a head-keyed lookup minted it a bogus record of its own instead of
   * folding it into {positional}'s. A hand-written module-reply VarDef (v(),
   * no kit variable behind it) leaves this unset, and the marketing catalog
   * falls back to head-matching for those, unchanged. */
  kitId?: string;
}

export interface SurfaceDef {
  id: string;
  group: L10n;
  label: L10n;
  /** Where this template is edited in the dashboard. */
  dashPath: string;
  hint: L10n;
  /** Starter template shown when the surface is selected. */
  example: L10n;
  /** The viewer/system line shown in the rehearsal. */
  prompt: L10n;
  vars: VarDef[];
}

const v = (token: string, sample: string, name: L10n, desc: L10n): VarDef => ({
  token,
  sample,
  name,
  desc,
});

// The four broadcaster-facing counter scopes (data.CounterScope*, minus the
// admin-only "bot" scope, which never appears outside the console). Chosen
// once, when the counter is created (dashboard Counters page, or !counter
// create <name> [scope]). The token itself never carries the scope, so this
// only powers the builder's counter picker (name + scope) and its hint text.
const COUNTER_SCOPES: ScopeDef[] = [
  {
    id: 'channel',
    label: { en: 'One total for the channel', fr: 'Un total pour toute la chaîne' },
    hint: { en: 'Every use adds to the same shared number.', fr: 'Chaque utilisation ajoute au même nombre partagé.' },
  },
  {
    id: 'viewer',
    label: { en: 'Per user', fr: 'Par spectateur' },
    hint: { en: 'Each viewer gets their own number.', fr: 'Chaque spectateur a son propre nombre.' },
  },
  {
    id: 'command',
    label: { en: 'Per command or reward, all users pooled', fr: 'Par commande ou récompense, tous les spectateurs regroupés' },
    hint: { en: 'One shared total for this command or reward.', fr: 'Un seul total partagé pour cette commande ou récompense.' },
  },
  {
    id: 'viewer_command',
    label: { en: 'Per user, per command or reward', fr: 'Par spectateur, par commande ou récompense' },
    hint: { en: 'Each viewer gets a separate number for this command or reward.', fr: 'Chaque spectateur a un nombre séparé pour cette commande ou récompense.' },
  },
];

// Every var the custom-command surface offers now comes straight from kit's
// manifest (docs/specs/variables-catalog.md D2/D3, phase 3 section A): one
// VarDef per kit VariableForm, copy resolved through kitText from
// vars.<id>.{name,desc} rather than carried here. MULTI_FORM_IDS is the
// short list of variables whose builder chip has always shown more than one
// form (matches what the deleted per-family arrays actually rendered:
// {random} and {random:1-6}, {1} and {2:}); every other kit variable offers
// several forms in its guide entry but only ever showed its first, canonical
// form as a builder chip.
const MULTI_FORM_IDS = new Set(['positional', 'random']);
// Counter vars are the only ones carrying a scope picker (see COUNTER_SCOPES).
// 'count' used to be its own manifest id (the {count:<name>} read-only
// alias); it merged away when {counter:x} itself stopped writing and picked
// up the alias's forms, so 'counter' is the one id left to carry the picker
// — the scope a broadcaster reads FROM, not one they write to any more (the
// write moved to the command-run "bump a counter" option).
const SCOPED_IDS = new Set(['counter']);

function kitVarDef(def: VariableDef, form: VariableForm): VarDef {
  const built = v(form.example, form.output, l10n(`vars.${def.id}.name`), l10n(`vars.${def.id}.desc`));
  const withId = { ...built, kitId: def.id };
  return SCOPED_IDS.has(def.id) ? { ...withId, scopes: COUNTER_SCOPES } : withId;
}

function customSurfaceVars(): VarDef[] {
  return KIT_VARIABLES.flatMap((def) => {
    const forms = MULTI_FORM_IDS.has(def.id) ? def.forms : def.forms.slice(0, 1);
    return forms.map((form) => kitVarDef(def, form));
  });
}

// A module reply's token copy (name/desc) now lives in kit locales under
// replyVars.<hintKey-without-".hint">.{name,desc}, set alongside each
// ReplyToken's hintKey in catalog/*.ts (docs/specs/variables-catalog.md
// phase 3 section A.3). Falling back to the bare token name/no description
// only guards a future catalog entry that forgets to wire hintKey; every
// token this builder reaches for today has one.
function replyTokenVarDef(token: ReplyToken): VarDef {
  const base = token.hintKey?.replace(/\.hint$/, '');
  const name = base ? l10n(`${base}.name`) : { en: token.name, fr: token.name };
  const desc = base ? l10n(`${base}.desc`) : { en: '', fr: '' };
  return v(`{${token.name}}`, token.sample, name, desc);
}

// {random}, {random:1-6} and {choice:…} work in custom commands and in
// every module reply template (module.ParseDynamic is each module's
// fallback), so every module surface below appends this same trio.
function dynamicFormVars(): VarDef[] {
  const random = variableById('random')!;
  const choice = variableById('choice')!;
  return [...random.forms.map((form) => kitVarDef(random, form)), ...choice.forms.map((form) => kitVarDef(choice, form))];
}

/** A module reply named the way its kit hintKey namespace is: `<moduleId>.<replyKey>`. */
type ReplyRef = `${string}.${string}`;

function moduleReplyTokens(ref: ReplyRef): readonly ReplyToken[] {
  const [moduleId, replyKey] = ref.split('.');
  const tokens = moduleDef(moduleId)?.replies.find((reply) => reply.key === replyKey)?.tokens;
  if (!tokens) throw new Error(`builder.ts: no ${ref} reply tokens in the kit catalog`);
  return tokens;
}

/** A module reply surface's vars: the reply's own tokens plus the shared
 * dynamic trio every module reply accepts. */
function moduleSurfaceVars(ref: ReplyRef): VarDef[] {
  return [...moduleReplyTokens(ref).map(replyTokenVarDef), ...dynamicFormVars()];
}

/** A built-in command's reply surface (!clip): unlike a module reply, a
 * built-in's ParseDynamic pass has never carried the dynamic trio, so this
 * does not append dynamicFormVars() (matches the deleted 'clip' VarDef list,
 * which never appended the shared dynamic trio either). */
function builtinSurfaceVars(id: string): VarDef[] {
  const tokens = BUILTIN_COMMANDS.find((cmd) => cmd.id === id)?.tokens;
  if (!tokens) throw new Error(`builder.ts: no builtin command "${id}" tokens in the kit catalog`);
  return tokens.map(replyTokenVarDef);
}

/** A surface with no kit reply behind it at all (Trigger Words' rule
 * response, a Channel Points reward line): the token list stays
 * hand-written here, but its copy still moves to kit locales under
 * replyVars.<surfaceId>.<token>.{name,desc} (section A.2's "MUST NOT carry
 * inline copy"), same as every mapped surface above. */
function explicitVars(surfaceId: string, tokens: readonly ReplyToken[]): VarDef[] {
  return tokens.map((token) => replyTokenVarDef({ ...token, hintKey: `replyVars.${surfaceId}.${token.name}.hint` }));
}

export const SURFACES: SurfaceDef[] = [
  {
    id: 'custom',
    group: { en: 'Commands', fr: 'Commandes' },
    label: { en: 'Custom command', fr: 'Commande personnalisée' },
    dashPath: '/commands',
    hint: { en: 'A reply viewers trigger with !yourcommand.', fr: 'Une réponse que les spectateurs déclenchent avec !votrecommande.' },
    example: { en: 'Welcome in, {user}! Grab a seat 🥯', fr: 'Bienvenue, {user}! Installe-toi 🥯' },
    prompt: { en: '!welcome', fr: '!bienvenue' },
    vars: customSurfaceVars(),
  },
  {
    id: 'follow',
    group: { en: 'Alerts', fr: 'Alertes' },
    label: { en: 'Follow alert', fr: 'Alerte de follow' },
    dashPath: '/modules/alerts',
    hint: { en: 'Chat Alerts module → follow message.', fr: 'Module Alertes de chat → message de follow.' },
    example: { en: 'Thanks for the follow, {user}!', fr: 'Merci pour le follow, {user}!' },
    prompt: { en: 'maya_live followed the channel', fr: 'maya_live suit maintenant la chaîne' },
    vars: moduleSurfaceVars('alerts.follow'),
  },
  {
    id: 'subscribe',
    group: { en: 'Alerts', fr: 'Alertes' },
    label: { en: 'Subscription alert', fr: "Alerte d'abonnement" },
    dashPath: '/modules/alerts',
    hint: { en: 'Chat Alerts module → subscription message.', fr: "Module Alertes de chat → message d'abonnement." },
    example: { en: 'Welcome, {user}! Thanks for the tier {tier} sub!', fr: 'Bienvenue, {user}! Merci pour le sub palier {tier}!' },
    prompt: { en: 'maya_live subscribed', fr: "maya_live s'est abonnée" },
    vars: moduleSurfaceVars('alerts.sub'),
  },
  {
    id: 'cheer',
    group: { en: 'Alerts', fr: 'Alertes' },
    label: { en: 'Cheer alert', fr: 'Alerte de cheer' },
    dashPath: '/modules/alerts',
    hint: { en: 'Chat Alerts module → cheer message.', fr: 'Module Alertes de chat → message de cheer.' },
    example: { en: 'Thanks for the {bits} bits, {user}! 💎', fr: 'Merci pour les {bits} bits, {user}! 💎' },
    prompt: { en: 'maya_live cheered 250 bits', fr: 'maya_live a envoyé 250 bits' },
    vars: moduleSurfaceVars('alerts.cheer'),
  },
  {
    id: 'raid',
    group: { en: 'Alerts', fr: 'Alertes' },
    label: { en: 'Raid alert', fr: 'Alerte de raid' },
    dashPath: '/modules/alerts',
    hint: { en: 'Chat Alerts module → raid message.', fr: 'Module Alertes de chat → message de raid.' },
    example: { en: '{user} raided with {viewers} viewers! Welcome!', fr: '{user} raid avec {viewers} spectateurs! Bienvenue!' },
    prompt: { en: 'CoolStreamer raided with 42 viewers', fr: 'CoolStreamer raid avec 42 spectateurs' },
    vars: moduleSurfaceVars('alerts.raid'),
  },
  {
    id: 'shoutout',
    group: { en: 'Chat tools', fr: 'Outils de chat' },
    label: { en: 'Auto Shoutout', fr: 'Shoutout automatique' },
    dashPath: '/modules/shoutout',
    hint: { en: 'Auto Shoutout module → raid shoutout message.', fr: 'Module Shoutout automatique → message de shoutout.' },
    example: { en: 'Go follow {raider} → twitch.tv/{raider.login} · {viewers} friends came over!', fr: 'Allez suivre {raider} → twitch.tv/{raider.login} · {viewers} amis sont arrivés!' },
    prompt: { en: 'CoolStreamer raided the channel', fr: 'CoolStreamer a raid la chaîne' },
    vars: moduleSurfaceVars('shoutout.shoutout'),
  },
  {
    id: 'triggers',
    group: { en: 'Chat tools', fr: 'Outils de chat' },
    label: { en: 'Trigger Words reply', fr: 'Réponse de mots déclencheurs' },
    dashPath: '/modules/triggers',
    hint: { en: 'Trigger Words module → the response side of a rule.', fr: 'Module Mots déclencheurs → la partie réponse d’une règle.' },
    example: { en: 'Hey {user}! {choice:Welcome in,Good to see you}!', fr: 'Salut {user}! {choice:Bienvenue,Contente de te voir}!' },
    prompt: { en: 'hello everyone', fr: 'bonjour tout le monde' },
    // Trigger Words rules have no fixed kit reply (the rule text is
    // broadcaster-written, docs/specs/variables-catalog.md phase 3 section
    // A.2), so {user} stays a hand-written token here; its copy still moved
    // to kit locales (replyVars.triggers.user).
    vars: [...explicitVars('triggers', [{ name: 'user', sample: 'maya_live' }]), ...dynamicFormVars()],
  },
  {
    id: 'clip',
    group: { en: 'Chat tools', fr: 'Outils de chat' },
    label: { en: '!clip reply', fr: 'Réponse de !clip' },
    dashPath: '/commands',
    hint: { en: 'Built-in !clip command → reply template (Commands page).', fr: 'Commande intégrée !clip → modèle de réponse (page Commandes).' },
    example: { en: '{user} clipped: {target} → {clip}', fr: '{user} a créé un clip: {target} → {clip}' },
    prompt: { en: '!clip That clutch', fr: '!clip Quel finish' },
    vars: builtinSurfaceVars('clip'),
  },
  {
    id: 'time',
    group: { en: 'Chat tools', fr: 'Outils de chat' },
    label: { en: '!time reply', fr: 'Réponse de !time' },
    dashPath: '/modules/time',
    hint: { en: 'Local Time module → !time reply.', fr: 'Module Heure locale → réponse de !time.' },
    example: { en: "It's {time} where I live ({timezone}).", fr: 'Il est {time} chez moi ({timezone}).' },
    prompt: { en: '!time', fr: '!time' },
    vars: moduleSurfaceVars('time.time'),
  },
  {
    id: 'channelpoints',
    group: { en: 'Chat tools', fr: 'Outils de chat' },
    label: { en: 'Channel Points reward reply', fr: 'Réponse de récompense (points de chaîne)' },
    dashPath: '/channelpoints',
    hint: { en: 'Channel Points page → the chat line a redemption posts.', fr: 'Page Points de chaîne → la ligne publiée lors d’un échange.' },
    example: { en: '{user} redeemed {reward} ({cost} pts): {input}', fr: '{user} a échangé {reward} ({cost} pts): {input}' },
    prompt: { en: 'maya_live redeemed Hydrate!', fr: 'maya_live a échangé Hydrate!' },
    // Channel Points has no reply on the kit module catalog (rewards are
    // broadcaster-created, RewardEditor owns their tokens on the dashboard),
    // so this stays a hand-written token list; copy moved to kit locales
    // (replyVars.channelpoints.*).
    vars: [
      ...explicitVars('channelpoints', [
        { name: 'user', sample: 'maya_live' },
        { name: 'input', sample: 'stay hydrated!' },
        { name: 'reward', sample: 'Hydrate!' },
        { name: 'cost', sample: '500' },
        { name: 'channel', sample: 'your_channel' },
        { name: 'counter', sample: '129' },
        { name: 'points', sample: '50' }
      ]),
      ...dynamicFormVars(),
    ],
  },
  {
    id: 'queue-join',
    group: { en: 'Play Queue', fr: "File d'attente" },
    label: { en: 'Queue: join confirmation', fr: 'File: confirmation de !join' },
    dashPath: '/modules/queue',
    hint: { en: 'Play Queue module → the !join confirmation.', fr: "Module File d'attente → la confirmation de !join." },
    example: { en: '{user} joined the queue at spot #{pos}.', fr: '{user} rejoint la file en position #{pos}.' },
    prompt: { en: '!join', fr: '!join' },
    vars: moduleSurfaceVars('queue.join'),
  },
  {
    id: 'queue-next',
    group: { en: 'Play Queue', fr: "File d'attente" },
    label: { en: 'Queue: next player up', fr: 'File: joueur suivant' },
    dashPath: '/modules/queue',
    hint: { en: 'Play Queue module → the !queue next announcement.', fr: "Module File d'attente → l'annonce de !queue next." },
    example: { en: "You're up, {target}! {count} waiting behind you.", fr: 'À toi, {target}! {count} personnes derrière toi.' },
    prompt: { en: '!queue next', fr: '!queue next' },
    vars: moduleSurfaceVars('queue.next'),
  },
  {
    id: 'bw-session',
    group: { en: 'Game stats', fr: 'Stats de jeu' },
    label: { en: 'Bedwars: !daily / !weekly / !monthly', fr: 'Bedwars: !daily / !weekly / !monthly' },
    dashPath: '/modules/urchin',
    hint: { en: 'Bedwars Stats module → session-stats reply.', fr: 'Module Stats Bedwars → réponse des stats de période.' },
    example: { en: '{player}: {wins}W {losses}L · {finals} finals · {beds} beds · {fkdr} FKDR', fr: '{player}: {wins}V {losses}D · {finals} finals · {beds} lits · {fkdr} FKDR' },
    prompt: { en: '!daily Technoblade', fr: '!daily Technoblade' },
    // Represents !daily/!weekly/!monthly, which share this exact token set
    // (BW_SESSION_TOKENS in kit's catalog/rehearsal-tokens.ts).
    vars: moduleSurfaceVars('urchin.daily'),
  },
  {
    id: 'bwstats',
    group: { en: 'Game stats', fr: 'Stats de jeu' },
    label: { en: 'Bedwars: !bwstats (lifetime)', fr: 'Bedwars: !bwstats (à vie)' },
    dashPath: '/modules/urchin',
    hint: { en: 'Bedwars Stats module → lifetime-stats reply.', fr: 'Module Stats Bedwars → réponse des stats à vie.' },
    example: { en: '{player}: {stars}✫ · {wins} wins · {fkdr} FKDR · {wlr} WLR', fr: '{player}: {stars}✫ · {wins} victoires · {fkdr} FKDR · {wlr} WLR' },
    prompt: { en: '!bwstats Technoblade', fr: '!bwstats Technoblade' },
    vars: moduleSurfaceVars('urchin.stats'),
  },
  {
    id: 'sniper',
    group: { en: 'Game stats', fr: 'Stats de jeu' },
    label: { en: 'Bedwars: !sniper', fr: 'Bedwars: !sniper' },
    dashPath: '/modules/urchin',
    hint: { en: 'Bedwars Stats module → sniper-score reply.', fr: 'Module Stats Bedwars → réponse du score sniper.' },
    example: { en: '{player} sniper score: {score} ({mode})', fr: '{player} score sniper: {score} ({mode})' },
    prompt: { en: '!sniper Technoblade', fr: '!sniper Technoblade' },
    vars: moduleSurfaceVars('urchin.sniper'),
  },
  {
    id: 'tags',
    group: { en: 'Game stats', fr: 'Stats de jeu' },
    label: { en: 'Bedwars: !tag / !tagdescription', fr: 'Bedwars: !tag / !tagdescription' },
    dashPath: '/modules/urchin',
    hint: { en: 'Bedwars Stats module → tag-lookup replies.', fr: 'Module Stats Bedwars → réponses de recherche de tags.' },
    example: { en: '{player}: {tags}', fr: '{player}: {tags}' },
    prompt: { en: '!tag Technoblade', fr: '!tag Technoblade' },
    vars: moduleSurfaceVars('urchin.tags'),
  },
  {
    id: 'elo',
    group: { en: 'Game stats', fr: 'Stats de jeu' },
    label: { en: 'MCSR: !elo', fr: 'MCSR: !elo' },
    dashPath: '/modules/mcsr',
    hint: { en: 'MCSR Ranked module → current-standing reply.', fr: 'Module MCSR Ranked → réponse du classement actuel.' },
    example: { en: '{player}: {elo} elo · rank #{rank} · {wins}W {losses}L', fr: '{player}: {elo} elo · rang #{rank} · {wins}V {losses}D' },
    prompt: { en: '!elo Feinberg', fr: '!elo Feinberg' },
    vars: moduleSurfaceVars('mcsr.elo'),
  },
  {
    id: 'mcsr-session',
    group: { en: 'Game stats', fr: 'Stats de jeu' },
    label: { en: 'MCSR: !session', fr: 'MCSR: !session' },
    dashPath: '/modules/mcsr',
    hint: { en: 'MCSR Ranked module → this-stream session reply.', fr: 'Module MCSR Ranked → réponse de la session du stream.' },
    example: { en: '{player}: {elochange} elo ({elo} now) · {wins}W {losses}L {draws}D in {matches} matches', fr: '{player}: {elochange} elo ({elo} maintenant) · {wins}V {losses}D {draws}N en {matches} matchs' },
    prompt: { en: '!session', fr: '!session' },
    vars: moduleSurfaceVars('mcsr.session'),
  },
  {
    id: 'mcsr-pace',
    group: { en: 'Game stats', fr: 'Stats de jeu' },
    label: { en: 'MCSR: !pace', fr: 'MCSR: !pace' },
    dashPath: '/modules/mcsr',
    hint: { en: 'MCSR Ranked module → PaceMan session-splits reply.', fr: 'Module MCSR Ranked → réponse des splits PaceMan de la session.' },
    example: { en: '{player} this session: {nethers} nethers (avg {nether}) · bastion {bastion} · fortress {fortress} · fp {firstportal} · {nph} nph', fr: '{player} cette session: {nethers} nethers (moy {nether}) · bastion {bastion} · forteresse {fortress} · pp {firstportal} · {nph} npu' },
    prompt: { en: '!pace', fr: '!pace' },
    vars: moduleSurfaceVars('mcsr.pace'),
  },
  {
    id: 'mcsr-nethers',
    group: { en: 'Game stats', fr: 'Stats de jeu' },
    label: { en: 'MCSR: !nethers', fr: 'MCSR: !nethers' },
    dashPath: '/modules/mcsr',
    hint: { en: 'MCSR Ranked module → PaceMan nether-count reply.', fr: 'Module MCSR Ranked → réponse du nombre de Nethers PaceMan.' },
    example: { en: '{player}: {nethers} nethers this session (avg {nether}) · {nph} nph', fr: '{player}: {nethers} nethers cette session (moy {nether}) · {nph} npu' },
    prompt: { en: '!nethers', fr: '!nethers' },
    vars: moduleSurfaceVars('mcsr.nethers'),
  },
  {
    id: 'mcsr-lastmatch',
    group: { en: 'Game stats', fr: 'Stats de jeu' },
    label: { en: 'MCSR: !lastmatch', fr: 'MCSR: !lastmatch' },
    dashPath: '/modules/mcsr',
    hint: { en: 'MCSR Ranked module → most-recent-match reply.', fr: 'Module MCSR Ranked → réponse du dernier match.' },
    example: { en: '{player} vs {opponent}: {result} · {time} · {seed} {structure} · {elochange} elo · {ago} ago', fr: '{player} contre {opponent}: {result} · {time} · {seed} {structure} · {elochange} elo · il y a {ago}' },
    prompt: { en: '!lastmatch', fr: '!lastmatch' },
    vars: moduleSurfaceVars('mcsr.lastmatch'),
  },
  {
    id: 'mcsr-record',
    group: { en: 'Game stats', fr: 'Stats de jeu' },
    label: { en: 'MCSR: !record', fr: 'MCSR: !record' },
    dashPath: '/modules/mcsr',
    hint: { en: 'MCSR Ranked module → head-to-head record reply.', fr: 'Module MCSR Ranked → réponse du bilan face-à-face.' },
    example: { en: '{playera} {winsa} - {winsb} {playerb} · {played} played', fr: '{playera} {winsa} - {winsb} {playerb} · {played} matchs joués' },
    prompt: { en: '!record Feinberg lowk3y_', fr: '!record Feinberg lowk3y_' },
    vars: moduleSurfaceVars('mcsr.record'),
  },
  {
    id: 'mcsr-lb',
    group: { en: 'Game stats', fr: 'Stats de jeu' },
    label: { en: 'MCSR: !lb', fr: 'MCSR: !lb' },
    dashPath: '/modules/mcsr',
    hint: { en: 'MCSR Ranked module → leaderboard reply (elo, phase or record).', fr: 'Module MCSR Ranked → réponse de classement (elo, phase ou record).' },
    example: { en: '{board}: {list}', fr: '{board}: {list}' },
    prompt: { en: '!lb', fr: '!lb' },
    vars: moduleSurfaceVars('mcsr.lb'),
  },
  {
    id: 'mcsr-race',
    group: { en: 'Game stats', fr: 'Stats de jeu' },
    label: { en: 'MCSR: !race', fr: 'MCSR: !race' },
    dashPath: '/modules/mcsr',
    hint: { en: 'MCSR Ranked module → weekly-race reply.', fr: 'Module MCSR Ranked → réponse de la course hebdomadaire.' },
    example: { en: '#1 {leader} ({leadertime}) · {player}: {time} (#{rank})', fr: '#1 {leader} ({leadertime}) · {player}: {time} (#{rank})' },
    prompt: { en: '!race', fr: '!race' },
    vars: moduleSurfaceVars('mcsr.race'),
  },
  {
    id: 'mcsr-pb',
    group: { en: 'Game stats', fr: 'Stats de jeu' },
    label: { en: 'MCSR: !pb', fr: 'MCSR: !pb' },
    dashPath: '/modules/mcsr',
    hint: { en: 'MCSR Ranked module → personal-best reply (PaceMan daily/weekly/monthly/all-time, or MCSR Ranked season best).', fr: 'Module MCSR Ranked → réponse du record personnel (PaceMan quotidien/hebdomadaire/mensuel/de tous les temps, ou le meilleur temps de la saison MCSR Ranked).' },
    example: { en: '{player}: {time} ({window} PB)', fr: '{player}: {time} ({window} PB)' },
    prompt: { en: '!pb daily', fr: '!pb daily' },
    vars: moduleSurfaceVars('mcsr.pb'),
  },
  {
    id: 'fn-stats',
    group: { en: 'Game stats', fr: 'Stats de jeu' },
    label: { en: 'Fortnite: !fn / !fn season', fr: 'Fortnite: !fn / !fn season' },
    dashPath: '/modules/fortnite',
    hint: { en: 'Fortnite Stats module → lifetime & season replies.', fr: 'Module Stats Fortnite → réponses à vie et de saison.' },
    example: { en: '{player} ({window}): {wins} wins · {kd} K/D · {winrate}% winrate', fr: '{player} ({window}): {wins} victoires · {kd} K/D · {winrate}% de victoires' },
    prompt: { en: '!fn', fr: '!fn' },
    vars: moduleSurfaceVars('fortnite.stats'),
  },
  {
    id: 'fn-store',
    group: { en: 'Game stats', fr: 'Stats de jeu' },
    label: { en: 'Fortnite: !fn store', fr: 'Fortnite: !fn store' },
    dashPath: '/modules/fortnite',
    hint: { en: 'Fortnite Stats module → item-shop reply.', fr: 'Module Stats Fortnite → réponse de la boutique.' },
    example: { en: 'Item shop {date} ({count} items): {items}', fr: 'Boutique du {date} ({count} objets): {items}' },
    prompt: { en: '!fn store', fr: '!fn store' },
    vars: moduleSurfaceVars('fortnite.store'),
  },
];

// First-line chat actions (app/twitch/sesame/engine/slash.go). Values are prefixes
// prepended to the first response line.
export const STYLES: { value: string; label: L10n }[] = [
  { value: '', label: { en: 'Normal message', fr: 'Message normal' } },
  { value: '/me ', label: { en: 'Action (/me)', fr: 'Action (/me)' } },
  { value: '/announce ', label: { en: 'Announcement', fr: 'Annonce' } },
  { value: '/announceblue ', label: { en: 'Blue announcement', fr: 'Annonce bleue' } },
  { value: '/announcegreen ', label: { en: 'Green announcement', fr: 'Annonce verte' } },
  { value: '/announceorange ', label: { en: 'Orange announcement', fr: 'Annonce orange' } },
  { value: '/announcepurple ', label: { en: 'Purple announcement', fr: 'Annonce violette' } },
  { value: '/shoutout ', label: { en: 'Twitch shoutout', fr: 'Shoutout Twitch' } },
  { value: '/pin ', label: { en: 'Pin the message', fr: 'Épingler le message' } },
];

// Access levels, low → high (app/twitch/sesame/module/permission.go, dashboard PERMS).
export const PERMS: { value: string; label: L10n }[] = [
  { value: 'everyone', label: { en: 'Everyone', fr: 'Tout le monde' } },
  { value: 'sub', label: { en: 'Subscribers & up', fr: 'Abonnés et plus' } },
  { value: 'vip', label: { en: 'VIPs & up', fr: 'VIP et plus' } },
  { value: 'mod', label: { en: 'Moderators & up', fr: 'Modérateurs et plus' } },
  { value: 'lead_mod', label: { en: 'Lead moderators & up', fr: 'Modérateurs principaux et plus' } },
  { value: 'broadcaster', label: { en: 'Broadcaster only', fr: 'Diffuseur seulement' } },
];

// The builder uses the same validation limits as the dashboard.
export const LIMITS = {
  nameMax: COMMAND_NAME_MAX,
  aliasMax: 25,
  lineMax: RESPONSE_MAX,
  linesMax: RESPONSE_MAX_LINES,
  cooldownMax: COOLDOWN_MAX,
} as const;

export const DASHBOARD_ORIGIN = SITE.dashboard;

// Quick-start recipes offered under the response field, both locales.
const RECIPES: { label: L10n; text: L10n }[] = [
  { label: { en: 'Welcome someone', fr: 'Accueillir quelqu’un' }, text: { en: 'Welcome in, {user}! Grab a seat 🥯', fr: 'Bienvenue, {user}! Installe-toi 🥯' } },
  { label: { en: 'Roll a number', fr: 'Lancer un dé' }, text: { en: '{user} rolled {random:1-100} out of 100.', fr: '{user} lance {random:1-100} sur 100.' } },
  { label: { en: 'Pick an option', fr: 'Choisir une option' }, text: { en: "Tonight's vibe: {choice:cozy,chaotic,competitive}.", fr: 'Ambiance du soir: {choice:cosy,chaotique,compétitive}.' } },
  { label: { en: 'Count the falls', fr: 'Compter les chutes' }, text: { en: '{channel} has fallen {counter:falls} times.', fr: '{channel} est tombé {counter:chutes} fois.' } },
];

/** The quick-start recipes for one locale, default-locale fallback per string. */
export function builderRecipes(lang: Lang): { label: string; text: string }[] {
  return RECIPES.map((r) => ({ label: pick(r.label, lang), text: pick(r.text, lang) }));
}

// UI copy for the builder page chrome, both locales.
const UI = {
  metaTitle: {
    en: 'Command Builder - ItsBagelBot',
    fr: 'Constructeur de commandes - ItsBagelBot',
  },
  metaDesc: {
    en: 'Build powerful ItsBagelBot commands without the syntax: click variables, watch a live chat rehearsal, then send the finished command straight to your dashboard.',
    fr: 'Créez des commandes ItsBagelBot puissantes sans la syntaxe: cliquez les variables, regardez la répétition en direct, puis envoyez la commande dans votre tableau de bord.',
  },
  eyebrow: { en: 'Command builder', fr: 'Constructeur de commandes' },
  title1: { en: 'Powerful commands.', fr: 'Des commandes puissantes.' },
  title2: { en: 'No syntax degree required.', fr: 'Aucun diplôme de syntaxe requis.' },
  lede: {
    en: 'Choose what you are writing, type the message normally, then click variables to add the smart parts. The rehearsal shows exactly what chat will see.',
    fr: 'Choisissez ce que vous écrivez, tapez le message normalement, puis cliquez les variables pour ajouter les parties intelligentes. La répétition montre exactement ce que le chat verra.',
  },
  step1Title: { en: 'What are you building?', fr: 'Que construisez-vous?' },
  modeCustom: { en: 'Custom command', fr: 'Commande personnalisée' },
  modeModule: { en: 'Module message', fr: 'Message de module' },
  surfaceLabel: { en: 'Exact message to customize', fr: 'Message exact à personnaliser' },
  nameLabel: { en: 'Command name', fr: 'Nom de la commande' },
  nameHint: { en: 'One word, no spaces. The ! is added for you.', fr: 'Un seul mot, sans espaces. Le ! est ajouté pour vous.' },
  aliasLabel: { en: 'Alternate names', fr: 'Autres noms' },
  aliasHint: { en: 'Optional, comma-separated. The same command answers to all of them.', fr: 'Optionnel, séparés par des virgules. La même commande répond à tous.' },
  permLabel: { en: 'Who can use it', fr: 'Qui peut l’utiliser' },
  cooldownLabel: { en: 'Cooldown (seconds)', fr: 'Délai (secondes)' },
  cooldownHint: { en: 'Shared by the whole chat. 0 = none.', fr: 'Partagé par tout le chat. 0 = aucun.' },
  styleLabel: { en: 'First-line style', fr: 'Style de la première ligne' },
  responseLabel: { en: 'Bot response', fr: 'Réponse du bot' },
  recipesLabel: { en: 'Quick starts', fr: 'Départs rapides' },
  moreOptions: { en: 'More options: access, cooldown, alternate names', fr: "Plus d'options: accès, délai, autres noms" },
  sendHelp: { en: 'Review the summary that opens, press Create, done.', fr: "Relisez le récapitulatif qui s'ouvre, appuyez sur Créer, c'est fait." },
  step3Title: { en: 'Make it dynamic', fr: 'Rendez-la dynamique' },
  step3Sub: { en: 'Click a variable to insert it at your cursor. Only variables that work here are shown.', fr: 'Cliquez une variable pour l’insérer au curseur. Seules les variables qui fonctionnent ici sont montrées.' },
  // The builder shows FIVE variables and nothing else -- no toggle, no hidden
  // rest-of-catalog -- so there is no "More variables" string to translate.
  // Which five, and why the toggle is not coming back, is written out in
  // @bagel/kit/engine/common-tokens.
  counterNameAria: { en: 'Counter name', fr: 'Nom du compteur' },
  counterScopeAria: { en: 'Counter scope', fr: 'Portée du compteur' },
  bracesSummary: { en: 'What do the braces mean?', fr: 'Que signifient les accolades?' },
  bracesBody: {
    en: 'A variable is a placeholder. Write "Hello {user}", and if Maya uses it, the bot says "Hello Maya". Keep both braces exactly as shown; an unknown variable is left as literal text. Add a "|" and some text inside any variable for a default when it comes back empty: {touser|everyone} says "everyone" when nobody was named.',
    fr: 'Une variable est un espace réservé. Écrivez «Bonjour {user}» et si Maya l’utilise, le bot dit «Bonjour Maya». Gardez les deux accolades telles quelles; une variable inconnue reste du texte littéral. Ajoutez un «|» et du texte dans n’importe quelle variable pour une valeur par défaut quand elle revient vide: {touser|tout le monde} affiche «tout le monde» si personne n’est nommé.',
  },
  previewTitle: { en: 'Live rehearsal', fr: 'Répétition en direct' },
  // Rehearsal chrome: word-for-word the dashboard's chatPreview catalog
  // (web/kit/lib/i18n/{en,fr}.ts), so the builder reads as the same
  // surface reaching out onto the marketing site.
  rehearsal: { en: 'Chat rehearsal', fr: 'Répétition du chat' },
  ariaTyping: { en: 'Bot is typing', fr: "Le bot est en train d'écrire" },
  announcement: { en: 'Announcement', fr: 'Annonce' },
  addMessageAfter: { en: '…add a message after {verb}', fr: '…ajoutez un message après {verb}' },
  shoutsOut: { en: 'Shouts out', fr: 'Fait un shoutout à' },
  nameChannel: { en: '…name a channel after /shoutout', fr: '…nommez une chaîne après /shoutout' },
  pinnedForStream: { en: 'Pinned until the stream ends', fr: 'Épinglé jusqu’à la fin du stream' },
  addActionAfterMe: { en: '…add an action after /me', fr: '…ajoutez une action après /me' },
  nothingToSay: { en: '…the bot has nothing to say yet', fr: "…le bot n'a rien à dire pour le moment" },
  unknownVar: { en: 'Unknown variable', fr: 'Variable inconnue' },
  sendTitle: { en: 'Send it to your dashboard', fr: 'Envoyez-la au tableau de bord' },
  sendCta: { en: 'Open in dashboard', fr: 'Ouvrir le tableau de bord' },
  copyTitleCustom: { en: 'Or paste it in chat', fr: 'Ou collez-la dans le chat' },
  copyTitleModule: { en: 'Copy the message template', fr: 'Copiez le modèle de message' },
  copyBodyModule: {
    en: 'Paste it into the matching field in your dashboard.',
    fr: 'Collez-le dans le champ correspondant du tableau de bord.',
  },
  copyCta: { en: 'Copy', fr: 'Copier' },
  copied: { en: 'Copied!', fr: 'Copié!' },
  copyFail: { en: 'Select and copy the text above', fr: 'Sélectionnez et copiez le texte ci-dessus' },
  openModule: { en: 'Open the module page', fr: 'Ouvrir la page du module' },
  statusName: { en: 'Check the name', fr: 'Vérifiez le nom' },
  statusLines: { en: 'Too many lines (max 5)', fr: 'Trop de lignes (max 5)' },
  statusLineLen: { en: 'A line is over 500 characters', fr: 'Une ligne dépasse 500 caractères' },
  statusEmpty: { en: 'Write a response', fr: 'Écrivez une réponse' },
  learnMore: {
    en: 'New to variables? Read the commands guide first.',
    fr: 'Les variables sont nouvelles pour vous? Lisez d’abord le guide des commandes.',
  },
  learnMoreCta: { en: 'Commands & variables guide', fr: 'Guide des commandes et variables' },
} as const;

type UIKeys = keyof typeof UI;

export function builderUI(lang: Lang): Record<UIKeys, string> {
  const out = {} as Record<UIKeys, string>;
  for (const key of Object.keys(UI) as UIKeys[]) out[key] = pick(UI[key] as L10n, lang);
  return out;
}

/** Everything the builder page needs, resolved for one locale. */
export function builderData(lang: Lang) {
  return {
    lang,
    defaultLang,
    limits: LIMITS,
    dashboardOrigin: DASHBOARD_ORIGIN,
    perms: PERMS.map((p) => ({ value: p.value, label: pick(p.label, lang) })),
    styles: STYLES.map((s) => ({ value: s.value, label: pick(s.label, lang) })),
    surfaces: SURFACES.map((s) => ({
      id: s.id,
      group: pick(s.group, lang),
      label: pick(s.label, lang),
      dashPath: s.dashPath,
      hint: pick(s.hint, lang),
      example: pick(s.example, lang),
      prompt: pick(s.prompt, lang),
      vars: s.vars.map((x) => ({
        token: x.token,
        sample: x.sample,
        name: pick(x.name, lang),
        desc: pick(x.desc, lang),
        scopes: x.scopes?.map((sc) => ({ id: sc.id, label: pick(sc.label, lang), hint: pick(sc.hint, lang) })),
      })),
    })),
  };
}

export type BuilderData = ReturnType<typeof builderData>;
