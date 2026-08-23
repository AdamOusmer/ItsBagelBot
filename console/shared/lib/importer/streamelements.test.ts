// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Parity suite for streamelements.ts, the port of
// app/importer/source/streamelements. Three layers:
//
//  1. Golden replay: testdata/se-golden.json pins the parser's full manifest +
//     diagnostics for the Go suite's committed envelope fixtures (decoded from
//     its golden.txt during the port). A diff means every future StreamElements
//     import's translation changed on purpose.
//  2. Unit vectors lifted verbatim from the Go package's tests (variables,
//     accessLevel table, flexText shapes, detect, parse assertions).
//  3. Fetch-flow tests against a local HTTP server (Bun.serve standing in for
//     the Go httptest.Server), asserting paths, headers and error prose.

import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { describe, expect, test } from 'bun:test';
import {
  DEFAULT_API_BASE,
  MAX_CREDENTIAL_LEN,
  SE_CODE,
  StreamElementsError,
  detectStreamElements,
  fetchStreamElements,
  mapAccessLevel,
  parseStreamElements,
  translateVariables
} from './streamelements';

const here = dirname(import.meta.path);

interface GoldenCase {
  file: string;
  manifest: Record<string, unknown>;
  diags: { severity: string; item_index: number; code: string; message: string }[];
}
const golden: GoldenCase[] = JSON.parse(readFileSync(join(here, 'testdata/se-golden.json'), 'utf8'));

describe('golden replay', () => {
  for (const want of golden) {
    test(`${want.file}: full manifest + diagnostics byte-exact vs the Go fixture`, () => {
      const raw = readFileSync(join(here, `testdata/se-envelope-${want.file}.json`), 'utf8');
      const { manifest, diagnostics } = parseStreamElements(raw);
      // Key order differs (Go struct fields vs JS objects); content is the
      // contract, so compare deep-equal — which bun's toEqual provides.
      expect(manifest).toEqual(want.manifest as never);
      expect(diagnostics).toEqual(want.diags as never); // messages included
    });
  }

  test('parse is deterministic', () => {
    const raw = readFileSync(join(here, 'testdata/se-envelope-full.json'), 'utf8');
    const a = JSON.stringify(parseStreamElements(raw));
    const b = JSON.stringify(parseStreamElements(raw));
    expect(a).toBe(b);
  });

  test('non-envelopes throw parse_failed material', () => {
    for (const raw of ['"string"', '[1,2,3]', '<xml/>']) {
      expect(() => parseStreamElements(raw)).toThrow(/not a commands\/timers envelope/);
    }
  });
});

describe('translateVariables (vectors from variables_test)', () => {
  const cases: [string, string, string[] | null][] = [
    ['$(user)!', '{user}!', null],
    ['${user.name} x $(sender) $(source)', '{user} x {sender} {sender}', null],
    ['$(USER.NAME)', '{user}', null],
    ['$(touser) $(target.user) {target}', '{touser} {touser} {touser}', null],
    ['$(1:) words', '{args} words', null],
    ['$(2:) tail', '$(2:) tail', ['$(2:)']],
    ['$(1) solo', '$(1) solo', ['$(1)']],
    ['$(:3)', '$(:3)', ['$(:3)']],
    ['$(1|everyone)', '$(1|everyone)', ['$(1|everyone)']],
    ['$(1 username)', '$(1 username)', ['$(1 username)']],
    ['$(channel)/${channel.alias}/$(channel.display_name)', '{channel}/{channel}/{channel}', null],
    ['$(channel.viewers)', '$(channel.viewers)', ['$(channel.viewers)']],
    ['$(getcount deaths)', '{counter:deaths}', null],
    ['$(getcount !Deaths)', '{counter:deaths}', null],
    ['$(count deaths)', '$(count deaths)', ['$(count deaths)']],
    ['{count hugs} hugs', '{count hugs} hugs', ['{count hugs}']],
    ['$(random)', '{random}', null],
    ['$(random.5-10)', '{random:5-10}', null],
    ['${random.-5--1}', '{random:-5--1}', null],
    ['$(random.number 1-100)', '{random:1-100}', null],
    ['$(random.chatter)', '$(random.chatter)', ['$(random.chatter)']],
    ['$(random.pick a b c)', '{choice:a,b,c}', null],
    ["$(random.pick pizza,pasta,'garlic bread')", '{choice:pizza,pasta,garlic bread}', null],
    ["$(random.pick 'a,b')", "$(random.pick 'a,b')", ["$(random.pick 'a,b')"]],
    ['{choose tea,coffee}', '{choice:tea,coffee}', null],
    ['{random 1-6}', '{random:1-6}', null],
    ['$(game $(touser))', '$(game {touser})', ['$(game {touser})']],
    ['$(weather ${1:})', '$(weather {args})', ['$(weather {args})']],
    ['{target.user} is live', '{touser} is live', null],
    ['{notavariable} and {} stay', '{notavariable} and {} stay', null],
    ['oops $(user', 'oops $(user', null],
    ['smile :) ($(user))', 'smile :) ({user})', null],
    ['$(uptime) $(title)', '$(uptime) $(title)', ['$(uptime)', '$(title)']],
    ['$(args)', '$(args)', ['$(args)']]
  ];
  for (const [input, want, warns] of cases) {
    test(`${input}`, () => {
      const { text, warns: got } = translateVariables(input);
      expect(text).toBe(want);
      expect(got).toEqual(warns ?? []);
    });
  }
});

describe('mapAccessLevel table', () => {
  const cases: [number, string, boolean][] = [
    [100, 'everyone', true],
    [250, 'sub', true],
    [300, 'everyone', false], // Regular widens, like Moobot regulars
    [400, 'vip', true],
    [500, 'mod', true],
    [1000, 'lead_mod', true], // Super Moderator ↔ senior-mod tier
    [1500, 'broadcaster', true],
    [777, 'everyone', false],
    [0, 'everyone', false],
    [-5, 'everyone', false]
  ];
  for (const [level, perm, recognized] of cases) {
    test(`accessLevel ${level}`, () => expect(mapAccessLevel(level)).toEqual({ perm, recognized }));
  }
  test('levelLabel uses SE vocabulary in warnings', () => {
    const { manifest, diagnostics } = parseStreamElements(
      '{"commands":[{"command":"x","reply":"y","accessLevel":300,"cooldown":{"global":0}}],"timers":[]}'
    );
    expect(diagnostics.some((d) => d.code === 'command_permission_unmapped' && d.message.includes('Regular'))).toBe(true);
    void manifest;
  });
});

describe('flexText shapes', () => {
  function timerWith(message: unknown): string {
    return parseStreamElements(JSON.stringify({ timers: [{ name: 't', message }] })).manifest.timers?.[0]?.message ?? '';
  }
  test('plain string', () => expect(timerWith('plain')).toBe('plain'));
  test('string array joins with newline', () => expect(timerWith(['one', 'two'])).toBe('one\ntwo'));
  test('{text} object array joins with newline', () =>
    expect(timerWith([{ text: 'alpha' }, { text: 'beta' }])).toBe('alpha\nbeta'));
  test('null message becomes empty', () => expect(timerWith(null)).toBe(''));
  test('numeric message skips the entry with the Go diagnostic code', () => {
    const { manifest, diagnostics } = parseStreamElements('{"timers":[{"name":"t","message":42}]}');
    expect(manifest.timers).toBeUndefined();
    expect(diagnostics[0].code).toBe(SE_CODE.timerUnparseable);
  });
  test('bad array element names the shape rule verbatim', () => {
    const { diagnostics } = parseStreamElements('{"timers":[{"name":"t","message":[42]}]}');
    expect(diagnostics[0].message).toContain('timer message array elements must be strings or {text} objects');
  });
});

describe('detect', () => {
  const full = readFileSync(join(here, 'testdata/se-envelope-full.json'), 'utf8');
  const legacy = readFileSync(join(here, 'testdata/se-envelope-legacy.json'), 'utf8');

  const foreign = [
    '',
    '   \n\t ',
    '{"commands":[{"name":"dank","response":"sup"}],"rolePlay":true}',
    '{"timers":[{"name":"x","intervalMinutes":5,"messages":["hi"]}]}',
    '{"commands":[],"timers":[]}',
    '<bot><command name="lol">hi</command></bot>',
    'definitely not json'
  ];

  test('foreign/empty payloads never claim detection', () => {
    for (const raw of foreign) expect(detectStreamElements(raw)).toBe(false);
  });
  test('committed envelopes detect', () => {
    expect(detectStreamElements(full)).toBe(true);
    expect(detectStreamElements(legacy)).toBe(true);
  });
  test('timers-only and commands-only envelopes are valid SE documents', () => {
    expect(detectStreamElements('{"timers":[{"online":{"enabled":true,"interval":5},"message":"hi"}]}')).toBe(true);
    expect(detectStreamElements('{"commands":[{"command":"a","reply":"b","accessLevel":100}]}')).toBe(true);
  });
});

describe('full-fixture parse assertions (from parse_test.go)', () => {
  const raw = readFileSync(join(here, 'testdata/se-envelope-full.json'), 'utf8');
  const { manifest, diagnostics } = parseStreamElements(raw);

  test('collection counts after regex/disabled skips', () => {
    expect(manifest.commands).toHaveLength(12);
    expect(manifest.timers).toHaveLength(3);
    expect(manifest.triggers).toHaveLength(2);
  });

  test('socials translated whole', () => {
    const c = manifest.commands![0];
    expect(c.name).toBe('socials');
    expect(c.permission).toBe('everyone');
    expect(c.cooldown_seconds).toBe(10);
    expect(c.aliases).toEqual(['tw', 'twitter']);
    expect(c.online_only).toBeUndefined();
    expect(c.responses![0]).toBe('Follow me on Twitter and Twitch!');
  });

  test('death counter + touser translation', () => {
    const c = manifest.commands![2];
    expect(c.permission).toBe('mod');
    expect(c.responses![0]).toBe('{counter:deaths} deaths so far — blame {touser} this time');
  });

  test('roll keeps random/choice keys under sub permission', () => {
    const c = manifest.commands![6];
    expect(c.permission).toBe('sub');
    expect(c.responses![0]).toContain('{random:5-10}');
    expect(c.responses![0]).toContain('{choice:pizza,pasta,garlic bread}');
  });

  test('permission tiers land per the documented widening rules', () => {
    expect(manifest.commands![7].online_only).toBeUndefined(); // offline-only upstream → widened, not online-only
    expect(manifest.commands![8].permission).toBe('lead_mod'); // Super Moderator
    expect(manifest.commands![9].permission).toBe('broadcaster');
    expect(manifest.commands![10].permission).toBe('everyone'); // Regular widens
    expect(manifest.commands![11].permission).toBe('everyone'); // unknown level
    expect(['gn', 'dealwithit', 'streamersecret', 'lounge', 'mystatus']).toEqual([
      manifest.commands![7].name,
      manifest.commands![8].name,
      manifest.commands![9].name,
      manifest.commands![10].name,
      manifest.commands![11].name
    ]);
  });

  test('keywords become triggers carrying the same response', () => {
    expect(manifest.triggers!.map((t) => t.phrase)).toEqual(['pog', 'poggers']);
    for (const t of manifest.triggers!) expect(t.response).toBe('{user} triggered the hype counter: $(count hype)');
  });

  test('timer intervals are minutes x 60; rotating messages join lines', () => {
    expect(manifest.timers![0]).toEqual({
      message: 'Enjoying the stream? Follow {channel} so you never miss a live!',
      interval_seconds: 300,
      online_only: true
    });
    expect(manifest.timers![1].interval_seconds).toBe(900);
    expect(manifest.timers![1].online_only).toBeUndefined();
    expect(manifest.timers![2].message).toBe('Line one starring {user}\nLine two with a $(random.chatter) cameo');
  });

  test('diagnostic codes/indexes match the Go expectations table', () => {
    const counts = new Map<string, number>();
    for (const d of diagnostics) {
      const key = `${d.item_index}|${d.code}`;
      counts.set(key, (counts.get(key) ?? 0) + 1);
    }
    const expected: Record<string, number> = {
      '-1|command_regex_skipped': 1,
      '-1|command_disabled_skipped': 1,
      '-1|timer_disabled_skipped': 1,
      '0|command_user_cooldown_dropped': 1,
      '1|command_type_reply': 1,
      '1|command_variable_unmapped': 1,
      '2|command_user_cooldown_dropped': 1,
      '3|command_variable_unmapped': 1,
      '4|command_type_whisper': 1,
      '4|command_cost_unsupported': 1,
      '4|command_user_cooldown_dropped': 1,
      '5|command_variable_unmapped': 1,
      '7|command_offline_only_widened': 1,
      '10|command_permission_unmapped': 1,
      '11|command_permission_unmapped': 1,
      '11|command_variable_unmapped': 2, // $(weather) and $(uptime)
      '0|trigger_variable_unmapped': 1,
      '1|trigger_variable_unmapped': 1,
      '1|timer_offline_only_widened': 1,
      '2|timer_variable_unmapped': 1 // $(random.chatter)
    };
    for (const [key, n] of Object.entries(expected)) {
      expect(counts.get(key) ?? 0).toBe(n);
    }
  });

  test('warnings mirror onto the command for the preview screen', () => {
    expect((manifest.commands![4].warnings ?? []).length).toBeGreaterThanOrEqual(3);
  });
});

// --- fetch flow --------------------------------------------------------------

const CHANNEL_ID = '5b2e2007760aeb7729487dab';
const TEST_JWT = 'eyJhbGciOiJIUzI1NiJ9.eyJpYXQiOjF9.c2lnbmF0dXJl'; // shape-valid dummy

// newTestServer stands in for the Go httptest.Server: records every request
// (path + headers snapshotted eagerly — Bun recycles Request internals once
// the handler resolves) and answers via `handler`; failPath answers failStatus.
interface Recorded {
  path: string;
  authorization: string | null;
  accept: string | null;
}
async function withTestServer(
  handler: (req: Request) => Response | Promise<Response>,
  run: (baseUrl: string, requests: Recorded[]) => Promise<void>
): Promise<void> {
  const requests: Recorded[] = [];
  const server = Bun.serve({
    port: 0,
    async fetch(req) {
      requests.push({
        path: new URL(req.url).pathname,
        authorization: req.headers.get('authorization'),
        accept: req.headers.get('accept')
      });
      return handler(req);
    }
  });
  try {
    await run(`http://127.0.0.1:${server.port}`, requests);
  } finally {
    server.stop(true);
  }
}

const envelopeBody = (): string => {
  const doc = JSON.parse(readFileSync(join(here, 'testdata/se-envelope-full.json'), 'utf8'));
  return JSON.stringify(doc);
};

describe('fetch flow', () => {
  test('combines commands+timers over three Bearer calls in order', async () => {
    await withTestServer(
      (req) => {
        const path = new URL(req.url).pathname;
        if (path === '/kappa/v2/channels/me') return Response.json({ _id: CHANNEL_ID });
        if (path === `/kappa/v2/bot/commands/${CHANNEL_ID}`)
          return new Response(JSON.stringify(JSON.parse(envelopeBody()).commands));
        if (path === `/kappa/v2/bot/timers/${CHANNEL_ID}`)
          return new Response(JSON.stringify(JSON.parse(envelopeBody()).timers));
        return new Response('not found', { status: 404 });
      },
      async (baseUrl, requests) => {
        const env = await fetchStreamElements(TEST_JWT, { baseUrl });
        expect(env.commands).toHaveLength(JSON.parse(envelopeBody()).commands.length);
        expect(env.timers).toHaveLength(JSON.parse(envelopeBody()).timers.length);

        expect(requests).toHaveLength(3);
        expect(requests.map((r) => r.path)).toEqual([
          '/kappa/v2/channels/me',
          `/kappa/v2/bot/commands/${CHANNEL_ID}`,
          `/kappa/v2/bot/timers/${CHANNEL_ID}`
        ]);
        for (const r of requests) {
          expect(r.authorization).toBe(`Bearer ${TEST_JWT}`);
          expect(r.accept).toContain('application/json');
        }
      }
    );
  });

  test('errors carry endpoint, status and remediation prose', async () => {
    await withTestServer(
      (req) => {
        const path = new URL(req.url).pathname;
        if (path === '/kappa/v2/channels/me' && req.method === 'OPTIONS') return new Response('', { status: 401 });
        return Response.json({ _id: CHANNEL_ID });
      },
      async (baseUrl) => {
        await expect(fetchStreamElements('   ', { baseUrl })).rejects.toThrow(/credential/);
      }
    );

    await withTestServer(
      () => new Response('{"statusCode":401,"error":"err","message":"boom"}', { status: 401 }),
      async (baseUrl) => {
        const err = await fetchStreamElements(TEST_JWT, { baseUrl }).catch((e) => e as Error);
        expect(err.message).toContain('/kappa/v2/channels/me returned 401');
        expect(err.message).toContain('Show secrets');
      }
    );
  });

  test('missing _id fails descriptively', async () => {
    await withTestServer(
      () => Response.json({ display_name: 'someone' }),
      async (baseUrl) => {
        const err = await fetchStreamElements(TEST_JWT, { baseUrl }).catch((e) => e as Error);
        expect(err.message).toContain('_id');
      }
    );
  });

  test('upstream 500 surfaces endpoint and status', async () => {
    await withTestServer(
      (req) => {
        const path = new URL(req.url).pathname;
        if (path === '/kappa/v2/channels/me') return Response.json({ _id: CHANNEL_ID });
        return new Response('<html>boom</html>', { status: 500 });
      },
      async (baseUrl) => {
        const err = await fetchStreamElements(TEST_JWT, { baseUrl }).catch((e) => e as Error);
        expect(err.message).toContain(`bot/commands/${CHANNEL_ID}`);
        expect(err.message).toContain('500');
      }
    );
  });

  test('malformed upstream JSON fails descriptively', async () => {
    await withTestServer(
      (req) => {
        if (new URL(req.url).pathname === '/kappa/v2/channels/me') return new Response('not json');
        return Response.json({});
      },
      async (baseUrl) => {
        const err = await fetchStreamElements(TEST_JWT, { baseUrl }).catch((e) => e as Error);
        expect(err.message).toContain('channels/me');
      }
    );
  });

  test('timeout aborts a slow upstream', async () => {
    await withTestServer(
      async () => {
        await Bun.sleep(200);
        return Response.json({ _id: 'x' });
      },
      async (baseUrl) => {
        const err = await fetchStreamElements(TEST_JWT, { baseUrl, timeoutMs: 20 }).catch((e) => e as Error);
        expect(err).toBeInstanceOf(StreamElementsError);
      }
    );
  });
});

describe('credential gate', () => {
  // The shape gate must reject before any HTTP happens: malformed credentials
  // are either paste accidents or header-injection bait.
  const bad = [
    'eyJhbGciOi.eyJpYXQ yZQ.c2ln',
    'eyJhbGciOi.eyJpYXQ\nc2ln.c2ln',
    'justatoken',
    'eyJhbGciOi.c2ln',
    '..'
  ];
  for (const cred of bad) {
    test(`rejects ${JSON.stringify(cred.slice(0, 16))}`, async () => {
      const err = await fetchStreamElements(cred).catch((e) => e as Error);
      expect(err.message).toContain('does not look like a StreamElements JWT');
    });
  }
  test(`rejects over ${MAX_CREDENTIAL_LEN} chars`, () =>
    expect(fetchStreamElements(`${'a.'.repeat(2100)}b`).catch((e) => (e as Error).message)).resolves.toContain(
      'does not look like a StreamElements JWT'
    ));

  test('default API base is production kappa root', () => {
    expect(DEFAULT_API_BASE).toBe('https://api.streamelements.com');
  });
});
