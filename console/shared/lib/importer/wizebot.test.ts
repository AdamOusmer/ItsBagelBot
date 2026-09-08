// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Behaviour contract for the Wizebot parser and its session replay. No Go
// implementation ever existed for this source, so these expectations ARE the
// contract: change one only when the mapping itself is meant to change.
//
// testdata/wizebot-fixture.json is REAL: 23 rows copied out of the published
// command lists of two public Wizebot streaming websites (kenth-s and
// nawielalmyugar, read 2026-09-07), covering every row type, permission chip,
// cost, entity and multi-alias case those two channels emit. Three synthetic
// rows are appended, recognizable by their 9999xxx ids, for the cases neither
// channel happened to have: a currency cost, a bits cost, a $(nick) tag,
// [b]bold[/b] markup and a multi-line SAY_RANDOM.
//
// testdata/wizebot-golden.json pins the manifest AND the diagnostic sequence
// that fixture produces. It is COMMITTED OUTPUT, generated once by hand and
// reviewed line by line; nothing in this suite rewrites it. Regenerating it is
// a deliberate act, run from console/ and followed by reading the diff:
//
//	bun -e 'import {parseWizebot} from "./shared/lib/importer/wizebot";
//	  const p="shared/lib/importer/testdata/";
//	  const r=parseWizebot(new Uint8Array(await Bun.file(p+"wizebot-fixture.json").arrayBuffer()));
//	  await Bun.write(p+"wizebot-golden.json", JSON.stringify({manifest:r.manifest,
//	    diagnostics:r.diagnostics.map(d=>({severity:d.severity,item_index:d.item_index,code:d.code}))},null,2)+"\n")'
//
// A golden that regenerates itself proves nothing, which is why that command
// lives in a comment instead of behind an env var this suite reads.

import { describe, expect, test } from 'bun:test';
import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import {
  decodeEntities,
  detectWizebot,
  fetchWizebot,
  parseWizebot,
  translateTags,
  WB_CODE,
  WizebotExportError,
  WizebotFetchError
} from './wizebot';
import { CODE } from './validate';
import type { ImportDiagnostic } from './types';

const TESTDATA = join(import.meta.dir, 'testdata');
const utf8 = (s: string): Uint8Array => new TextEncoder().encode(s);
const fixtureBytes = (): Uint8Array => new Uint8Array(readFileSync(join(TESTDATA, 'wizebot-fixture.json')));

const codesOf = (diags: ImportDiagnostic[]): string[] => diags.map((d) => d.code);

// Row is one published command in the shape the tests write it, folded onto
// the positional array the streaming website actually serves.
interface Row {
  aliases: string;
  text?: string;
  cost?: string;
  perm?: string;
  type?: string;
  sound?: number;
}

const listBytes = (rows: Row[]): Uint8Array =>
  utf8(
    JSON.stringify({
      data: rows.map((r) => [
        r.aliases,
        '2138083',
        r.text ?? 'hello',
        r.cost ?? '-',
        r.perm ?? '',
        r.type ?? 'SAY',
        r.sound ?? 0,
        'd41d8cd98f00b204e9800998ecf8427e'
      ])
    })
  );

const chip = (label: string): string => `<span class="label label-warning">${label}</span>`;

describe('decodeEntities', () => {
  test('named Latin-1 and typographic references decode', () => {
    expect(decodeEntities('caf&eacute; &agrave; c&ocirc;t&eacute;')).toBe('café à côté');
    expect(decodeEntities('Twitch &amp; Youtube &laquo; ici &raquo;&hellip;')).toBe(
      'Twitch & Youtube « ici »…'
    );
    expect(decodeEntities('&lt;3 &gt;_&lt; &quot;quoted&quot;')).toBe('<3 >_< "quoted"');
  });

  test('numeric references decode in decimal and hex', () => {
    expect(decodeEntities('l&#39;autre &#x27;un&#x27;')).toBe("l'autre 'un'");
    expect(decodeEntities('&#128512;')).toBe('\u{1f600}');
  });

  test('unknown named references survive verbatim rather than vanishing', () => {
    expect(decodeEntities('&angmsdaw; &notreal;')).toBe('&angmsdaw; &notreal;');
  });

  test('a bare ampersand in running text is left alone', () => {
    expect(decodeEntities('rock & roll, 5 & 6')).toBe('rock & roll, 5 & 6');
  });

  test('out-of-range and surrogate code points stay literal', () => {
    expect(decodeEntities('&#x110000; &#xd800; &#0;')).toBe('&#x110000; &#xd800; &#0;');
  });
});

describe('translateTags', () => {
  test('the mapped tags become substitution tokens', () => {
    expect(translateTags('Hi $(nick) / $(display_name) in $(channel_name): $(message_clear)')).toEqual({
      text: 'Hi {user} / {user} in {channel}: {args}',
      warns: []
    });
  });

  test('bold markup is unwrapped without a warning', () => {
    expect(translateTags('[b]loud[/b] and clear')).toEqual({ text: 'loud and clear', warns: [] });
  });

  test('unmapped tags stay literal and are reported once each', () => {
    const out = translateTags('$(uptime) playing $(current_game), $(uptime) again, $currency(1) $arg(2)');
    expect(out.text).toBe('$(uptime) playing $(current_game), $(uptime) again, $currency(1) $arg(2)');
    expect(out.warns).toEqual(['$(uptime)', '$(current_game)', '$currency(1)', '$arg(2)']);
  });
});

describe('detectWizebot', () => {
  test('accepts a published command list', () => {
    expect(detectWizebot(fixtureBytes())).toBe(true);
  });

  test('rejects another source export and junk', () => {
    expect(detectWizebot(utf8(JSON.stringify({ commands: [{ name: '!hello', message: 'hi' }] })))).toBe(
      false
    );
    expect(detectWizebot(utf8('not json'))).toBe(false);
    expect(detectWizebot(utf8(JSON.stringify({ data: [['hello', '1', 'hi', '-', '', 'MAGIC', 0, 'x']] })))).toBe(
      false
    );
  });
});

describe('envelope shapes', () => {
  test('a bare array of rows is read like the keyed list', () => {
    const doc = JSON.parse(new TextDecoder().decode(listBytes([{ aliases: '!hi' }])));
    const { manifest } = parseWizebot(utf8(JSON.stringify(doc.data)));
    expect(manifest.commands).toHaveLength(1);
  });

  test('a list that is not one at all throws', () => {
    expect(() => parseWizebot(utf8('{'))).toThrow(WizebotExportError);
    expect(() => parseWizebot(utf8(JSON.stringify({ hello: 'world' })))).toThrow(WizebotExportError);
    expect(() => parseWizebot(utf8(JSON.stringify({ data: [] })))).toThrow(WizebotExportError);
  });

  test('rows too short to carry columns are counted, not swallowed', () => {
    const { manifest, diagnostics } = parseWizebot(
      utf8(JSON.stringify({ data: [['!hi', '1', 'hello', '-', '', 'SAY', 0, 'x'], ['!broken'], 'nope'] }))
    );
    expect(manifest.commands).toHaveLength(1);
    expect(codesOf(diagnostics)).toEqual([WB_CODE.rowUnparseable]);
    expect(diagnostics[0].message).toContain('2 Wizebot row(s)');
  });
});

describe('commands', () => {
  test('name, aliases and text translate, entities decoded', () => {
    const { manifest, diagnostics } = parseWizebot(
      listBytes([{ aliases: '!Plava  !Plavastation', text: 'Guides &amp; cha&icirc;ne: ici' }])
    );
    expect(manifest.commands?.[0]).toEqual({
      name: 'plava',
      responses: ['Guides & chaîne: ici'],
      permission: 'everyone',
      aliases: ['plavastation']
    });
    expect(diagnostics).toEqual([]);
  });

  test('rows are sorted by normalized name before they are indexed', () => {
    const { manifest } = parseWizebot(
      listBytes([{ aliases: '!zulu' }, { aliases: '!Alpha' }, { aliases: '!mike' }])
    );
    expect(manifest.commands?.map((c) => c.name)).toEqual(['alpha', 'mike', 'zulu']);
  });

  test('non-chat row types are skipped by name', () => {
    const { manifest, diagnostics } = parseWizebot(
      listBytes([
        { aliases: '!keep' },
        { aliases: '!anim', type: 'SCREEN' },
        { aliases: '!honk', type: 'ONLY_SOUND' },
        { aliases: '!me', type: 'ACTION' },
        { aliases: '!link', type: 'URL' },
        { aliases: '!weird', type: 'SOMETHING_NEW' }
      ])
    );
    expect(manifest.commands?.map((c) => c.name)).toEqual(['keep']);
    expect(codesOf(diagnostics)).toEqual(Array(5).fill(WB_CODE.typeUnsupported));
    expect(diagnostics.map((d) => d.item_index)).toEqual([-1, -1, -1, -1, -1]);
    expect(diagnostics[0].message).toContain('"!anim"');
  });

  test('a case-only duplicate keeps the first row and reports the rest', () => {
    const { manifest, diagnostics } = parseWizebot(
      listBytes([{ aliases: '!Ouf', text: 'first' }, { aliases: '!ouf', text: 'second' }])
    );
    expect(manifest.commands).toHaveLength(1);
    expect(codesOf(diagnostics)).toEqual([WB_CODE.duplicateSkipped]);
  });

  test('a row with no published text is skipped instead of imported mute', () => {
    const { manifest, diagnostics } = parseWizebot(
      listBytes([{ aliases: '!silent', text: '   ' }, { aliases: '!loud', text: 'hi' }])
    );
    expect(manifest.commands?.map((c) => c.name)).toEqual(['loud']);
    expect(codesOf(diagnostics)).toEqual([WB_CODE.responseEmpty]);
  });

  test('CRLF text becomes several response lines', () => {
    const { manifest } = parseWizebot(listBytes([{ aliases: '!multi', text: 'one\r\ntwo\r\n\r\nthree' }]));
    expect(manifest.commands?.[0].responses).toEqual(['one', 'two', 'three']);
  });

  test('a multi-line SAY_RANDOM is flattened with a warning', () => {
    const { manifest, diagnostics } = parseWizebot(
      listBytes([{ aliases: '!roll', text: 'one\r\ntwo', type: 'SAY_RANDOM' }])
    );
    expect(manifest.commands?.[0].responses).toEqual(['one', 'two']);
    expect(codesOf(diagnostics)).toEqual([WB_CODE.randomFlattened]);
    expect(manifest.commands?.[0].warnings).toHaveLength(1);
  });

  test('a single-line SAY_RANDOM says nothing', () => {
    const { diagnostics } = parseWizebot(listBytes([{ aliases: '!roll', type: 'SAY_RANDOM' }]));
    expect(diagnostics).toEqual([]);
  });

  test('an unmapped tag warns on the item that carries it', () => {
    const { manifest, diagnostics } = parseWizebot(
      listBytes([{ aliases: '!a', text: 'hi' }, { aliases: '!b', text: 'up $(uptime)' }])
    );
    expect(codesOf(diagnostics)).toEqual([CODE.variableUnmapped]);
    expect(diagnostics[0].item_index).toBe(1);
    expect(manifest.commands?.[1].warnings).toHaveLength(1);
  });

  test('an alias that normalizes to nothing is dropped with a warning', () => {
    const { manifest, diagnostics } = parseWizebot(listBytes([{ aliases: '!good  !  !good' }]));
    expect(manifest.commands?.[0].aliases).toBeUndefined();
    expect(codesOf(diagnostics)).toEqual([CODE.aliasInvalid]);
  });

  test('a row whose name normalizes to nothing is skipped as an error', () => {
    const { manifest, diagnostics } = parseWizebot(listBytes([{ aliases: '   ' }, { aliases: '!ok' }]));
    expect(manifest.commands?.map((c) => c.name)).toEqual(['ok']);
    expect(diagnostics[0]).toMatchObject({ severity: 'error', item_index: -1, code: CODE.nameInvalid });
  });
});

describe('permissions', () => {
  test('one chip maps onto its tier', () => {
    const { manifest, diagnostics } = parseWizebot(
      listBytes([
        { aliases: '!a', perm: chip('Subscribers') },
        { aliases: '!b', perm: chip('VIPs') },
        { aliases: '!c', perm: chip('Moderators') },
        { aliases: '!d', perm: '' }
      ])
    );
    expect(manifest.commands?.map((c) => c.permission)).toEqual(['sub', 'vip', 'mod', 'everyone']);
    expect(diagnostics).toEqual([]);
  });

  test('several chips are any-of, so the widest audience wins', () => {
    const { manifest } = parseWizebot(
      listBytes([{ aliases: '!a', perm: `${chip('Subscribers')} ${chip('VIPs')} ${chip('Moderators')}` }])
    );
    expect(manifest.commands?.[0].permission).toBe('sub');
  });

  test('the follower tier drops its qualifier and reads as everyone', () => {
    const { manifest, diagnostics } = parseWizebot(
      listBytes([{ aliases: '!a', perm: chip('Followers (0 J.)') }])
    );
    expect(manifest.commands?.[0].permission).toBe('everyone');
    expect(diagnostics).toEqual([]);
  });

  test('a named-user allowlist is unmapped, widened to everyone and reported', () => {
    const { manifest, diagnostics } = parseWizebot(listBytes([{ aliases: '!a', perm: chip('User(s)') }]));
    expect(manifest.commands?.[0].permission).toBe('everyone');
    expect(codesOf(diagnostics)).toEqual([CODE.permissionUnmapped]);
    expect(diagnostics[0].message).toContain('"User(s)"');
  });
});

describe('cost and sound', () => {
  test('a currency or bits price is dropped with a warning', () => {
    const { diagnostics } = parseWizebot(
      listBytes([{ aliases: '!a', cost: 'C100' }, { aliases: '!b', cost: 'B50' }])
    );
    expect(codesOf(diagnostics)).toEqual([WB_CODE.costDropped, WB_CODE.costDropped]);
    expect(diagnostics[0].message).toContain('100 channel currency');
    expect(diagnostics[1].message).toContain('50 bits');
  });

  test('a free command says nothing about cost', () => {
    expect(parseWizebot(listBytes([{ aliases: '!a', cost: '-' }])).diagnostics).toEqual([]);
  });

  test('a sound that also fires is reported on the kept command', () => {
    const { manifest, diagnostics } = parseWizebot(listBytes([{ aliases: '!a', sound: 1 }]));
    expect(codesOf(diagnostics)).toEqual([WB_CODE.soundDropped]);
    expect(manifest.commands?.[0].warnings).toHaveLength(1);
  });
});

describe('golden', () => {
  test('the published fixture translates to the committed golden', () => {
    const { manifest, diagnostics } = parseWizebot(fixtureBytes());
    const golden = JSON.parse(readFileSync(join(TESTDATA, 'wizebot-golden.json'), 'utf8'));
    expect(manifest).toEqual(golden.manifest);
    expect(
      diagnostics.map((d) => ({ severity: d.severity, item_index: d.item_index, code: d.code }))
    ).toEqual(golden.diagnostics);
  });
});

// --- fetch -------------------------------------------------------------------

// Recorded is one call the stand-in site saw, snapshotted eagerly (Bun recycles
// Request internals once the handler resolves).
interface Recorded {
  path: string;
  method: string;
  cookie: string | null;
  body: string;
}

// SiteOptions bends the stand-in away from the happy path one behaviour at a
// time, which is how each failure mode of the three-call replay is exercised.
interface SiteOptions {
  notEnabled?: boolean;
  noCookie?: boolean;
  tokenless?: boolean;
  delayMs?: number;
  oversized?: boolean;
}

const TOKEN = 'a'.repeat(64);
const LIST = JSON.stringify({ data: [['!hi', '1', 'hello', '-', '', 'SAY', 0, 'x']] });

// wizebotSite emulates the streaming website: call 1 hands out the session
// cookie, call 2 answers the table fragment only for the p=commands body, and
// call 3 answers the list only when the cookie came back, which is the exact
// behaviour the fetch layer was written against.
function wizebotSite(opts: SiteOptions, seen: Recorded[]) {
  return async (req: Request): Promise<Response> => {
    const url = new URL(req.url);
    const call: Recorded = {
      path: url.pathname,
      method: req.method,
      cookie: req.headers.get('cookie'),
      body: req.method === 'POST' ? await req.text() : ''
    };
    seen.push(call);
    if (opts.delayMs) await Bun.sleep(opts.delayMs);
    return siteReply(opts, call);
  };
}

function siteReply(opts: SiteOptions, call: Recorded): Response {
  if (call.path === '/') return indexReply(opts);
  if (call.path === '/ajax.php') return tableReply(opts, call);
  if (!call.cookie?.includes('wizebot_network=')) return new Response('');
  return new Response(opts.oversized ? 'x'.repeat(17 << 20) : LIST);
}

function indexReply(opts: SiteOptions): Response {
  if (opts.notEnabled)
    return new Response('', { status: 302, headers: { Location: '/errors/channel_not_found.html' } });
  const headers = new Headers({ 'Content-Type': 'text/html' });
  if (!opts.noCookie) headers.append('Set-Cookie', 'wizebot_network=sid-42; Path=/; HttpOnly');
  return new Response('<html>ok</html>', { headers });
}

function tableReply(opts: SiteOptions, call: Recorded): Response {
  if (!call.cookie?.includes('wizebot_network=')) return new Response('');
  if (call.body !== 'p=commands') return new Response('');
  const href = opts.tokenless ? 'ajax/list/other_list.php?t=nope' : `ajax/list/commands_list.php?t=${TOKEN}`;
  return new Response(`<div class="table"><a href="${href}">list</a></div>`);
}

async function withSite(
  opts: SiteOptions,
  run: (baseUrl: () => string, seen: Recorded[]) => Promise<void>
): Promise<void> {
  const seen: Recorded[] = [];
  const server = Bun.serve({ port: 0, fetch: wizebotSite(opts, seen) });
  const base = `http://127.0.0.1:${server.port}`;
  try {
    await run(() => base, seen);
  } finally {
    server.stop(true);
  }
}

describe('fetch flow', () => {
  test('three calls, cookie carried by hand, list parseable as-is', async () => {
    await withSite({}, async (baseUrl, seen) => {
      const bytes = await fetchWizebot('Kenth-S', { baseUrl });
      expect(seen.map((c) => c.path)).toEqual(['/', '/ajax.php', '/ajax/list/commands_list.php']);
      expect(seen[0].cookie).toBeNull();
      expect(seen[1].body).toBe('p=commands');
      expect(seen[1].cookie).toBe('wizebot_network=sid-42');
      expect(seen[2].cookie).toBe('wizebot_network=sid-42');
      expect(parseWizebot(bytes).manifest.commands).toHaveLength(1);
    });
  });

  test('a channel with no streaming website is named in the refusal', async () => {
    await withSite({ notEnabled: true }, async (baseUrl, seen) => {
      const err = await fetchWizebot('myth', { baseUrl }).catch((e) => e as Error);
      expect(err).toBeInstanceOf(WizebotFetchError);
      expect(err.message).toBe('wizebot: no Wizebot streaming website is enabled for "myth"');
      expect(seen).toHaveLength(1);
    });
  });

  test('a site that starts no session refuses before the table call', async () => {
    await withSite({ noCookie: true }, async (baseUrl, seen) => {
      await expect(fetchWizebot('kenth-s', { baseUrl })).rejects.toThrow(/did not start a session/);
      expect(seen).toHaveLength(1);
    });
  });

  test('a table fragment without the list token refuses', async () => {
    await withSite({ tokenless: true }, async (baseUrl) => {
      await expect(fetchWizebot('kenth-s', { baseUrl })).rejects.toThrow(/did not expose a command list/);
    });
  });

  test('a malformed channel name never reaches the transport', async () => {
    await withSite({}, async (baseUrl, seen) => {
      await expect(fetchWizebot('   ', { baseUrl })).rejects.toThrow(/a channel name is required/);
      await expect(fetchWizebot('not a name', { baseUrl })).rejects.toThrow(/not a Wizebot channel name/);
      await expect(fetchWizebot('-leading', { baseUrl })).rejects.toThrow(/not a Wizebot channel name/);
      await expect(fetchWizebot('a'.repeat(26), { baseUrl })).rejects.toThrow(/not a Wizebot channel name/);
      expect(seen).toHaveLength(0);
    });
  });

  test('a slow site times out with readable prose', async () => {
    await withSite({ delayMs: 200 }, async (baseUrl) => {
      const err = await fetchWizebot('kenth-s', { baseUrl, timeoutMs: 20 }).catch((e) => e as Error);
      expect(err).toBeInstanceOf(WizebotFetchError);
      expect(err.message).toContain('request timed out');
    });
  });

  test('an oversized list is refused instead of buffered', async () => {
    await withSite({ oversized: true }, async (baseUrl) => {
      await expect(fetchWizebot('kenth-s', { baseUrl })).rejects.toThrow(/exceeds 16777216 bytes/);
    });
  });
});
