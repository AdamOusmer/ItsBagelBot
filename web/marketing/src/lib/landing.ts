// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { moduleDef } from '@bagel/kit/catalog';
import type { ModuleDef, ModuleReply } from '@bagel/kit/catalog/module-def';
import { defaultLang, type Lang } from '../i18n/lang';

type Translate = (key: string) => string;

export interface CommandRow {
    cells: string[];
    copy: string;
}

interface Overlay {
    replies?: Record<string, { defaultMessage?: string }>;
}

const overlays = import.meta.glob<Overlay>(
    '../../../../locales/*/console/modules/catalog/{valorant,govee}.json',
    { eager: true, import: 'default' },
);
const chatTables = import.meta.glob<Record<string, string>>(
    '../../../../locales/*/chat/{songqueue,govee}.json',
    { eager: true, import: 'default' },
);

const SLOT = /\{(?:[a-z]+:)?([a-z_.]+)(?:\|[^}]*)?\}/g;
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
