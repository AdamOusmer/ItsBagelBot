// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { moduleDef } from '@bagel/kit/catalog';
import { blankTimer } from '@bagel/kit/timers';
import { blankDiscordConfig } from '../../../kit/lib/discord-config';
import { guildModuleTiles } from '../../../kit/lib/discord-overview';
import type { ModuleDef, ModuleReply } from '@bagel/kit/catalog/module-def';
import { defaultLang, type Lang } from '../i18n/lang';
import { localizePath } from '../i18n/ui';

type Translate = (key: string) => string;

export interface CommandRow {
    cells: string[];
    copy?: string;
}

interface Overlay {
    replies?: Record<string, { defaultMessage?: string }>;
}

const overlays = import.meta.glob<Overlay>(
    '../../../../locales/*/console/modules/catalog/{valorant,govee,fortnite,urchin,mcsr,clashroyale,codm,raffle,queue,gamble,duel}.json',
    { eager: true, import: 'default' },
);
const chatTables = import.meta.glob<Record<string, string>>(
    '../../../../locales/*/chat/{songqueue,govee,loyalty}.json',
    { eager: true, import: 'default' },
);

const deathCounterCopy = import.meta.glob<Record<string, string>>(
    '../../../../locales/*/website/deathCounter.json',
    { eager: true, import: 'default' },
);

const SLOT =/\{(?:[a-z]+:)?([a-z_.]+)(?:\|[^}]*)?\}/g;
const VIEWER = 'PrincessBarney';
const SONG = { title: 'One More Time', artist: 'Daft Punk', pos: '3' };

export const SONG_REQUEST_EXAMPLE = `${SONG.artist} - ${SONG.title}`;
export const VALORANT_EXAMPLE_ID = 'Frosty#EUW1';

function fill(template: string, samples: Record<string, string>): string {
    return template.replace(SLOT, (slot, name: string) => samples[name] ?? slot);
}

function sampleMap(reply: ModuleReply): Record<string, string> {
    return Object.fromEntries((reply.tokens ?? []).map((token) => [token.name.replace(/^[a-z]+:/, ''), token.sample]));
}

function moduleOf(id: string): ModuleDef {
    const def = moduleDef(id);
    if (!def) throw new Error(`catalog module missing: ${id}`);
    return def;
}

export function catalogReplyText(lang: Lang, moduleId: string, reply: ModuleReply, samples: Record<string, string> = {}): string {
    const localized = overlays[`../../../../locales/${lang}/console/modules/catalog/${moduleId}.json`]?.replies?.[reply.key]?.defaultMessage;
    return fill(localized ?? reply.defaultMessage, { ...sampleMap(reply), ...samples });
}

const VALORANT_ALIASES: Record<string, string> = {
    val: '!valrank',
    'val matches': '!valmatches, !valhistory',
    'val account': '!valaccount, !valwho',
    'val lb': '!vallb, !valleaderboard',
    'val shop': '!valshop, !valrotation',
};

export function valorantCommands(): string[] {
    return (moduleOf('valorant').replies.flatMap((reply) => (reply.command ? [`!${reply.command}`] : [])));
}

export function valorantRows(lang: Lang, t: Translate): CommandRow[] {
    const rows = moduleOf('valorant').replies.flatMap((reply): CommandRow[] => {
        if (!reply.command) return [];
        const command = `!${reply.command}`;
        const row: CommandRow = {
            cells: [command, VALORANT_ALIASES[reply.command] ?? t('landing.none'), t('landing.whoEveryone'), catalogReplyText(lang, 'valorant', reply)],
            copy: command,
        };
        if (reply.command !== 'val') return [row];
        const byId = `${command} Name#TAG`;
        return [row, { cells: [byId, '!valrank Name#TAG', t('landing.whoEveryone'), row.cells[3]], copy: `${command} ${VALORANT_EXAMPLE_ID}` }];
    });
    return rows;
}

function chatLine(lang: Lang, file: string, key: string, samples: Record<string, string>): string {
    const at = (code: Lang) => chatTables[`../../../../locales/${code}/chat/${file}.json`];
    return fill(at(lang)?.[key] ?? at(defaultLang)[key], samples);
}

function songLine(lang: Lang, key: string, extra: Record<string, string> = {}): string {
    return chatLine(lang, 'songqueue', key, { user: VIEWER, req: VIEWER, count: '2', limit: '2', ...SONG, ...extra });
}

const SONG_ROWS: Record<string, { aliases: string; who: 'tier' | 'everyone' | 'mods'; reply: string; copy?: string }> = {
    '!sr': { aliases: '!songrequest, !songreq', who: 'tier', reply: 'add.ok', copy: `!sr ${SONG_REQUEST_EXAMPLE}` },
    '!srlist': { aliases: '!songlist', who: 'everyone', reply: 'status.queued' },
    '!current': { aliases: '!song, !np, !nowplaying', who: 'everyone', reply: 'current.req' },
    '!remove': { aliases: '', who: 'everyone', reply: 'retract.ok' },
    '!skip': { aliases: '!next', who: 'mods', reply: 'next.pending' },
    '!clear': { aliases: '', who: 'mods', reply: 'cleared' },
};

export function songCommands(): string[] {
    return (moduleOf('songqueue').commands ?? []).map((command) => command.trigger);
}

export function songRows(lang: Lang, t: Translate): CommandRow[] {
    const whoLabel = { tier: t('landing.whoTier'), everyone: t('landing.whoEveryone'), mods: t('landing.whoMods') };
    const queued = `1. ${SONG.title} (by ${VIEWER}) · 2. Around the World (by Cardistry)`;
    return songCommands().map((trigger) => {
        const spec = SONG_ROWS[trigger];
        return {
            cells: [trigger, spec.aliases || t('landing.none'), whoLabel[spec.who], songLine(lang, spec.reply, { list: queued })],
            copy: spec.copy ?? trigger,
        };
    });
}

export function songExampleLines(lang: Lang) {
    return [
        { who: 'viewer' as const, name: VIEWER, text: `!sr ${SONG_REQUEST_EXAMPLE}` },
        { who: 'bot' as const, text: songLine(lang, 'add.ok') },
        { who: 'viewer' as const, name: 'Cardistry', text: '!sr Daft Punk - Around the World' },
        { who: 'bot' as const, text: songLine(lang, 'err.no_device', { user: 'Cardistry' }) },
    ];
}

export function goveeRows(lang: Lang, t: Translate): CommandRow[] {
    const reply = moduleOf('govee').replies[0];
    const success = (input: string) => catalogReplyText(lang, 'govee', reply, { input, color: input, user: VIEWER });
    const refusal = (key: string) => chatLine(lang, 'govee', key, { user: VIEWER });
    const row = (typed: string, kind: string, text: string): CommandRow => ({ cells: [typed, t(kind), text], copy: typed });
    return [
        row('blue', 'govee.kindName', success('blue')),
        row('#00ccff', 'govee.kindHex', success('#00ccff')),
        row('#0cf', 'govee.kindShortHex', success('#0cf')),
        row('chartreuse', 'govee.kindUnknown', refusal('refund.colour')),
        row('blue', 'govee.kindOffline', refusal('refund.offline')),
    ];
}

export function goveeExampleLines(lang: Lang, t: Translate) {
    const reply = moduleOf('govee').replies[0];
    const say = (input: string) => catalogReplyText(lang, 'govee', reply, { input, color: input, user: VIEWER });
    return [
        { who: 'system' as const, text: `${VIEWER} ${t('govee.redeemed')} blue` },
        { who: 'bot' as const, text: say('blue') },
        { who: 'system' as const, text: `${VIEWER} ${t('govee.redeemed')} #00ccff` },
        { who: 'bot' as const, text: say('#00ccff') },
    ];
}

export function valorantExampleLines(lang: Lang) {
    const [rank, matches] = moduleOf('valorant').replies;
    return [
        { who: 'viewer' as const, name: VIEWER, text: '!valrank' },
        { who: 'bot' as const, text: catalogReplyText(lang, 'valorant', rank) },
        { who: 'viewer' as const, name: 'Cardistry', text: '!valmatches' },
        { who: 'bot' as const, text: catalogReplyText(lang, 'valorant', matches) },
    ];
}

interface GameSpec {
    module: string;
    ns: string;
    path: string;
    aiDoc: string;
    relKey: string;
    aliases: Readonly<Record<string, string>>;
    lookup: { command: string; arg: string };
    demo: readonly { viewer: string; typed: string; reply: string }[];
}

export const GAMES = {
    fortnite: {
        module: 'fortnite',
        ns: 'fortniteStats',
        path: '/fortnite-stats',
        aiDoc: 'https://ai.itsbagelbot.com/features/fortnite.md',
        relKey: 'landing.relFortnite',
        aliases: {
            fn: '!fn stats, !fnstats, !fortnitestats',
            'fn season': '!fnseason',
            'fn session': '!fnsession',
            'fn store': '!fnstore, !itemshop, !fnshop',
        },
        lookup: { command: 'fn', arg: 'Ninja' },
        demo: [
            { viewer: VIEWER, typed: '!fn Ninja', reply: 'stats' },
            { viewer: 'Cardistry', typed: '!fn store', reply: 'store' },
        ],
    },
    bedwars: {
        module: 'urchin',
        ns: 'bedwarsStats',
        path: '/hypixel-bedwars-stats',
        aiDoc: 'https://ai.itsbagelbot.com/features/urchin.md',
        relKey: 'landing.relBedwars',
        aliases: {
            daily: '!bwdaily',
            weekly: '!bwweekly',
            monthly: '!bwmonthly',
            bwstats: '!bedwars',
            sniper: '!urchin',
            tag: '!tags, !bwtags',
        },
        lookup: { command: 'bwstats', arg: 'Technoblade' },
        demo: [
            { viewer: VIEWER, typed: '!bwstats Technoblade', reply: 'stats' },
            { viewer: 'Cardistry', typed: '!daily', reply: 'daily' },
        ],
    },
    mcsr: {
        module: 'mcsr',
        ns: 'mcsrStats',
        path: '/mcsr-ranked-stats',
        aiDoc: 'https://ai.itsbagelbot.com/features/mcsr.md',
        relKey: 'landing.relMcsr',
        aliases: {
            elo: '!mcsr, !ranked',
            session: '!mcsrsession',
            pace: '!pacesession, !splits',
            nethers: '!nph',
            lastfort: '!lastpace, !previousfort',
            lastmatch: '!rankedmatch',
            record: '!matchrecord',
            lb: '!rankedlb, !mcsrlb',
            race: '!weeklyrace',
            pb: '!personalbest',
        },
        lookup: { command: 'elo', arg: 'Feinberg' },
        demo: [
            { viewer: VIEWER, typed: '!elo Feinberg', reply: 'elo' },
            { viewer: 'Cardistry', typed: '!lastmatch', reply: 'lastmatch' },
        ],
    },
    clashroyale: {
        module: 'clashroyale',
        ns: 'clashRoyaleStats',
        path: '/clash-royale-stats',
        aiDoc: 'https://ai.itsbagelbot.com/features/clashroyale.md',
        relKey: 'landing.relClashRoyale',
        aliases: {
            cr: '!cr stats, !crstats, !clashroyale',
            'cr decks': '!crdecks, !crdeck',
            'cr ranked': '!crranked, !crpol',
            'cr road': '!crroad, !crtrophy',
        },
        lookup: { command: 'cr', arg: '#P2LQ0GR' },
        demo: [
            { viewer: VIEWER, typed: '!cr #P2LQ0GR', reply: 'stats' },
            { viewer: 'Cardistry', typed: '!cr decks', reply: 'decks' },
        ],
    },
    codm: {
        module: 'codm',
        ns: 'codmStats',
        path: '/codm-stats',
        aiDoc: 'https://ai.itsbagelbot.com/features/codm.md',
        relKey: 'landing.relCodm',
        aliases: { codm: '!codmprofile, !codmrank' },
        lookup: { command: 'codm', arg: 'iFerg' },
        demo: [
            { viewer: VIEWER, typed: '!codm iFerg', reply: 'profile' },
            { viewer: 'Cardistry', typed: '!codmrank iFerg', reply: 'profile' },
        ],
    },
} as const satisfies Record<string, GameSpec>;

export type GameId = keyof typeof GAMES;

export function gameRelated(id: GameId): { path: string; relKey: string }[] {
    return Object.entries(GAMES).filter(([key]) => key !== id).map(([, game]) => ({ path: game.path, relKey: game.relKey }));
}

function gameRow(game: GameSpec, lang: Lang, t: Translate, reply: ModuleReply): CommandRow {
    const command = `!${reply.command}`;
    const cells = [command, game.aliases[reply.command ?? ''] ?? t('landing.none'), t('landing.whoEveryone'), catalogReplyText(lang, game.module, reply)];
    return { cells, copy: command };
}

export function gameRows(id: GameId, lang: Lang, t: Translate): CommandRow[] {
    const game: GameSpec = GAMES[id];
    const { command, arg } = game.lookup;
    return moduleOf(game.module).replies
        .filter((reply) => reply.command)
        .flatMap((reply): CommandRow[] => {
            const row = gameRow(game, lang, t, reply);
            if (reply.command !== command) return [row];
            const typed = `!${command} ${arg}`;
            return [row, { cells: [typed, ...row.cells.slice(1)], copy: typed }];
        });
}

export function gameExampleLines(id: GameId, lang: Lang) {
    const game: GameSpec = GAMES[id];
    const replies = moduleOf(game.module).replies;
    return game.demo.flatMap((step) => {
        const reply = replies.find((candidate) => candidate.key === step.reply);
        if (!reply) throw new Error(`catalog reply missing: ${game.module}.${step.reply}`);
        return [
            { who: 'viewer' as const, name: step.viewer, text: step.typed },
            { who: 'bot' as const, text: catalogReplyText(lang, game.module, reply) },
        ];
    });
}

interface LandingSchemaInput {
    site: URL;
    url: URL;
    lang: Lang;
    name: string;
    crumb: string;
    homeLabel: string;
    description: string;
    modified: string;
    commands?: { cells: string[] }[];
}

function commandList(pageUrl: string, commands: { cells: string[] }[]): Record<string, unknown> {
    return {
        '@type': 'ItemList',
        '@id': `${pageUrl}#commands`,
        numberOfItems: commands.length,
        itemListElement: commands.map(({ cells }, index) => ({
            '@type': 'ListItem',
            position: index + 1,
            name: cells[0],
            description: cells[cells.length - 1],
        })),
    };
}

export function landingSchema(input: LandingSchemaInput): Record<string, unknown>[] {
    const { site, url, lang, commands } = input;
    const origin = site.origin;
    const pageUrl = new URL(url.pathname, site).href;
    const page = {
        '@type': 'WebPage',
        '@id': `${pageUrl}#webpage`,
        url: pageUrl,
        name: input.name,
        description: input.description,
        inLanguage: lang,
        dateModified: input.modified,
        about: { '@id': `${origin}/#software` },
        isPartOf: { '@id': `${origin}/#website` },
    };
    const breadcrumbs = {
        '@type': 'BreadcrumbList',
        '@id': `${pageUrl}#breadcrumbs`,
        itemListElement: [
            { '@type': 'ListItem', position: 1, name: input.homeLabel, item: new URL(localizePath('/', lang), site).href },
            { '@type': 'ListItem', position: 2, name: input.crumb, item: pageUrl },
        ],
    };
    return commands ? [page, breadcrumbs, commandList(pageUrl, commands)] : [page, breadcrumbs];
}

export const FEATURES = {
    giveaways: { ns: 'giveaways', path: '/giveaways', modules: ['raffle'], ai: 'engagement', guide: '/guides/modules', related: ['viewerQueue', 'loyaltyPoints', 'timers'] },
    loyaltyPoints: { ns: 'loyaltyPoints', path: '/loyalty-points', modules: ['loyalty', 'gamble', 'duel'], ai: 'loyalty', guide: '/guides/counters', related: ['deathCounter', 'giveaways', 'viewerQueue'] },
    deathCounter: { ns: 'deathCounter', path: '/death-counter', modules: ['counters'], ai: 'engagement', guide: '/guides/counters', related: ['loyaltyPoints', 'timers', 'viewerQueue'] },
    timers: { ns: 'timers', path: '/timers', modules: ['timers'], ai: 'engagement', guide: '/guides/commands', related: ['giveaways', 'viewerQueue', 'discordBot'] },
    viewerQueue: { ns: 'viewerQueue', path: '/viewer-queue', modules: ['queue'], ai: 'engagement', guide: '/guides/modules', related: ['giveaways', 'loyaltyPoints', 'discordBot'] },
    discordBot: { ns: 'discordBot', path: '/discord-bot', modules: ['discord'], ai: 'discord', guide: '/guides/discord-setup', related: ['timers', 'viewerQueue', 'giveaways'] },
} as const;

export type FeatureId = keyof typeof FEATURES;

type CatalogCommand = NonNullable<ModuleDef['commands']>[number];
interface FeatureCommand extends CatalogCommand {
    module: string;
}

function moduleCommands(id: string): FeatureCommand[] {
    const def = moduleOf(id);
    const commands = [...(def.commands ?? [])];
    const additional = def.replies.filter((reply) => reply.command && !commands.some((command) => command.trigger === `!${reply.command}` || command.trigger.startsWith(`!${reply.command} `)));
    const entries = new Map(commands.map((command) => [command.trigger, command]));
    additional.forEach((reply) => entries.set(`!${reply.command}`, { trigger: `!${reply.command}`, summary: reply.tagline }));
    return [...entries.values()].map((command) => ({ ...command, module: id }));
}

export function featureCommands(id: FeatureId): FeatureCommand[] {
    return FEATURES[id].modules.flatMap(moduleCommands);
}

function featureWho(command: FeatureCommand, k: Translate): string {
    const restricted: Record<string, string> = { '!duel accept': 'whoChallenged', '!duel decline': 'whoChallenged', '!duel cancel': 'whoOpenerMods' };
    const key = restricted[command.trigger] ?? (command.perm === 'mod' ? 'whoMods' : 'whoEveryone');
    return k(key);
}

function featureReply(command: FeatureCommand, lang: Lang): string {
    const examples: Record<string, string> = { '!gamble <amount>': 'won', '!gamble half|all': 'won', '!duel <stake>': 'opened', '!duel @user <stake>': 'challenge' };
    const reply = moduleOf(command.module).replies.find((reply) => reply.key === examples[command.trigger] || command.trigger === reply.event || command.trigger.startsWith(reply.event + ' '));
    return reply ? catalogReplyText(lang, command.module, reply) : command.summary;
}

const DEATH_COUNTER_ROWS = [
    ['!death', 'whoEveryone', 'row1Outcome'],
    ['!death 5', 'whoMods', 'row2Outcome'],
    ['!death -1', 'whoMods', 'row3Outcome'],
] as const;

export function featureRows(id: FeatureId, lang: Lang, t: Translate): CommandRow[] {
    const spec = FEATURES[id];
    const k = (key: string) => t(spec.ns + '.' + key);
    if (id === 'timers') return Object.entries(blankTimer()).filter(([key]) => key !== 'id').map(([key, value]) => ({
        cells: [k('fields.' + key + '.label'), k('fields.' + key + '.value').replace('{value}', String(value)), k('fields.' + key + '.outcome')],
    }));
    if (id === 'discordBot') return guildModuleTiles(blankDiscordConfig()).map((tile) => ({
        cells: [k('tiles.' + tile.id + '.label'), k('tiles.' + tile.id + '.configure'), k('tiles.' + tile.id + '.outcome')],
    }));
    if (id === 'deathCounter') return DEATH_COUNTER_ROWS.map(([trigger, who, outcome]) => ({ cells: [trigger, k(who), k(outcome)], copy: trigger }));
    return featureCommands(id).map((command) => ({
        cells: [command.trigger, featureWho(command, k), featureReply(command, lang)],
        copy: command.trigger === '!raffle [amount] [seconds]' ? command.trigger.split(' [')[0] + ' 1000 60' : command.trigger,
    }));
}

export function featureExampleLines(id: FeatureId, lang: Lang) {
    if (id === 'deathCounter') {
        const at = (code: Lang) => deathCounterCopy[`../../../../locales/${code}/website/deathCounter.json`];
        const reply = (at(lang) ?? at(defaultLang)).demoReply;
        return [
            { who: 'viewer' as const, name: VIEWER, text: '!death' },
            { who: 'bot' as const, text: reply },
            { who: 'mod' as const, name: 'lead_mod', text: '!death 5' },
            { who: 'bot' as const, text: reply },
        ];
    }
    const demos: Partial<Record<FeatureId, { module: string; keys: string[] }>> = {
        giveaways: { module: 'raffle', keys: ['opened', 'joined'] },
        loyaltyPoints: { module: 'gamble', keys: ['won', 'lost'] },
        viewerQueue: { module: 'queue', keys: ['join', 'next'] },
    };
    const demo = demos[id];
    if (!demo) return undefined;
    return demo.keys.flatMap((key) => {
        const reply = moduleOf(demo.module).replies.find((reply) => reply.key === key)!;
        const command = moduleCommands(demo.module).find((command) => command.trigger === reply.event || command.trigger.startsWith(reply.event + ' '));
        const typed = reply.key === 'opened' ? `${reply.event} 10 1 5` : reply.event;
        return [
            { who: command?.perm === 'mod' ? 'mod' as const : 'viewer' as const, name: VIEWER, text: typed },
            { who: 'bot' as const, text: catalogReplyText(lang, demo.module, reply) },
        ];
    });
}
