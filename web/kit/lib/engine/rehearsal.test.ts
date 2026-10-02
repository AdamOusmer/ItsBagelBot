// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import { rehearseCommand, rehearseReply, rehearseTimer, resolvedWithoutSample, timerOwns, type ChainKind, type Samples, type Seg, type SegKind } from './rehearsal';

function textOf(segments: Seg[]): string {
  return segments.map((s) => s.text).join('');
}

describe('token expansion (module/vars.go Expand mirror)', () => {
  const segmentsOf = (template: string, samples: Samples) => rehearseReply(template, samples, { dynamic: false })[0].segments;

  test('substitutes known tokens and marks them as samples', () => {
    expect(segmentsOf('hi {user}!', { user: 'sam' })).toEqual([
      { text: 'hi ', kind: 'plain' },
      { text: 'sam', kind: 'sample' },
      { text: '!', kind: 'plain' }
    ]);
  });

  test('token names are case-insensitive, payloads keep their case', () => {
    expect(segmentsOf('{User}', { user: 'sam' })).toEqual([{ text: 'sam', kind: 'sample' }]);
    expect(segmentsOf('{CHOICE:Hi,Yo}', { 'choice:Hi,Yo': 'Hi' })).toEqual([{ text: 'Hi', kind: 'sample' }]);
  });

  test('any brace span is a token, unknown ones stay literal but marked', () => {
    expect(segmentsOf('{touser2} {foo bar}', { user: 'sam' })).toEqual([
      { text: '{touser2}', kind: 'unknown' },
      { text: ' ', kind: 'plain' },
      { text: '{foo bar}', kind: 'unknown' }
    ]);
  });

  test('a "{" with no closing brace is copied literally', () => {
    expect(segmentsOf('oops {user', { user: 'sam' })).toEqual([{ text: 'oops {user', kind: 'plain' }]);
  });

  test('an empty-string resolution drops the token from the output', () => {
    expect(textOf(segmentsOf('[{gone}]', { gone: '' }))).toBe('[]');
  });
});

type Overrides = Parameters<typeof rehearseCommand>[1];

const commandText = (template: string, overrides?: Overrides) =>
  textOf(rehearseCommand(template, overrides)[0].segments);

const COMMAND_TEXT: [name: string, template: string, want: string, overrides?: Overrides][] = [
  ['positional words come from the {args} sample, so the two agree', '{1} / {2} / {2:} / {1:}', 'ferret_king / good / good luck / ferret_king good luck'],
  ['an override of {args} moves the positional samples with it', '{1} then {2:}', 'alex then two three', { args: 'alex two three' }],
  ['a word past the end renders its fallback, or nothing', '[{9}] {9|nobody}', '[] nobody'],
  ['{n:m} is a real slice; {:m} anchors it at word 1', '{1:2} / {:2}', 'ferret_king good / ferret_king good'],
  ['{sender}/{target} are aliases; an override of the canonical covers them', '{sender} waves at {target}', 'maya_live waves at alex', { user: 'maya_live', touser: 'alex' }],
  ['resolves dynamic tokens deterministically', '{random} {random:10-20} {choice:a,b,c}', '57 15 a'],
  ['invalid random ranges stay literal, like ParseDynamic ok=false', '{random:5-1} {random:x-y}', '{random:5-1} {random:x-y}'],
  ['a conditional picks a branch from the sample of the token it names', '{if:touser:hi you:hi everyone} - {touser}', 'hi you - ferret_king'],
  ['a conditional on a missing word tests emptiness', '{if:9:word nine:no ninth word}', 'no ninth word'],
  ['a conditional equality is true on a match', '{if:command=hug:hugs:waves}', 'hugs'],
  ['a conditional equality is case-sensitive', '{if:command=HUG:hugs:waves}', 'waves'],
  ['a conditional on a counter tests emptiness', '{if:count:deaths:some deaths:none}', 'some deaths'],
  ['a conditional on a counter supports equality', '{if:count:deaths=42:exactly 42:something else}', 'exactly 42'],
  ['a branch is literal text: no token expands inside it', '{if:user:hi {user}}', 'hi {user}'],
  ['a present value ignores its fallback', 'hi {user|everyone}', 'hi sesame_sam'],
  ['the pure scope answers first, so a sample cannot shadow the dice', '{random}', '57', { random: 'nope' }],
  ['the utility scope computes {math:…} for real', 'that is {math:(1+2)*3} bagels', 'that is 9 bagels'],
  ['a refused expression renders its fallback, like chat', '{math:1/0|no idea}', 'no idea'],
  ['the URL encoders show the bytes the request will carry', '{queryescape:a b&c} {pathescape:a b&c}', 'a+b%26c a%20b&c'],
  ['{querystring} is the args sample, URL-encoded', '?q={querystring}', '?q=bagel+%26+lox', { args: 'bagel & lox' }],
  ['{repeat:…} repeats within its cap', '{repeat:3:yay}', 'yay yay yay'],
  ['{repeat:…} past its cap renders the fallback', '{repeat:21:x|too many}', 'too many'],
  ['{countdown} shows a fixed sample', '{countdown:2026-12-25}', '3 days, 4 hours'],
  ['{countup} shows a fixed sample', '{countup:2020-01-01T00:00:00Z}', '3 days, 4 hours'],
  ['a bad countdown date renders its fallback', '{countdown:next tuesday|soon}', 'soon'],
  ['a named viewer previews the same stand-in as the bare form', '{points:alex} vs {points}', '1280 vs 1280'],
  ['the message scope still answers first for the names it owns', '{user} has {points}', 'sesame_sam has 1280'],
  ['two chatter draws preview as the same name', '{random.chatter} and {random.chatter}', 'maya_live and maya_live'],
  ['a named channel previews with the same stand-in', 'go watch {game:@Pokimane}', 'go watch Just Chatting'],
  ['bare {channel} is still the display name', '{channel} plays {game}', 'bagel_bakery plays Just Chatting'],
  ['two emote draws preview as the same code', '{random.emote} {random.emote}', 'KEKW KEKW'],
  ['the song halves preview as the halves of the whole', '{song.title}: {song.artist}', 'Everything In Its Right Place: Radiohead'],
  ['a numbered quote previews the same stand-in as the random draw', '{quote:7}', 'Quote #12: bagels are just savoury donuts (2026-01-31)'],
  ['{count:name} previews like {counter:name}', '{counter:deaths} deaths, {count:Deaths} today', '42 deaths, 42 today'],
  ['bare {count} is the {uses} alias, not a counter read', '{count}', '317'],
  ['a bot-addressed counter read resolves', '{count:bot:feeds}', '42'],
  ['a target-addressed read previews like the bump', '{count:target:shutups}', '42']
];

const COMMAND_RESOLVED: [name: string, template: string, want: string][] = [
  ['the viewer lookups preview with a stand-in', '{followage} / {accountage} / {points} {pointsname} / {watchtime}', '3 months / 4 years, 2 months / 1280 bagels / 2 hours, 30 minutes'],
  ['the room previews with a stand-in', '{chatters} here, hi {random.chatter}', '37 here, hi maya_live'],
  ['the command run count previews with a stand-in', 'hugged {uses} times', 'hugged 317 times'],
  ['the channel facts preview with a stand-in', '{title} / {game} / {channel.viewers} / {uptime}', 'bagel baking and chill / Just Chatting / 128 / 2 hours, 15 minutes'],
  ['each provider list previews with its own stand-in', '{7tvemotes} / {bttvemotes} / {ffzemotes}', 'PagMan Clap peepoHappy / KEKW monkaS catJAM / LUL ZULUL AYAYA'],
  ['the module facts preview with a stand-in', '{quote} / {time} / {song}', 'Quote #12: bagels are just savoury donuts (2026-01-31) / 3:04 PM / Everything In Its Right Place by Radiohead'],
  ['{time:<place>} previews a value, not the literal span', '{time} in {touser}, {time:Paris} in Paris', '3:04 PM in ferret_king, 11:04 PM in Paris']
];

const COMMAND_SAMPLED: [name: string, template: string, want: string, samples: number][] = [
  ['substitutes exactly the expandCommand token set', '{user} {target} {args} {channel}', 'sesame_sam ferret_king ferret_king good luck bagel_bakery', 4],
  ['the identity tokens substitute too', '{userid} {user.login} !{command}', '48291057 sesame_sam !hug', 3],
  ['a span that only looks like a word number stays literal', '{0} {31} {01} {+1} {2:1}', '{0} {31} {01} {+1} {2:1}', 0]
];

const LITERAL_SPANS: [name: string, spans: string[]][] = [
  ['a cond naming something no scope owns keeps the whole span', ['{if:missing:x}', '{if:missing:x:y}', '{if}', '{if:user}']],
  ['a fallback never rescues a name no scope owns', ['{typo|rescued}']],
  ['an identity token with a payload stays literal, like Message.Get', ['{user:bob}']],
  ['a span that addresses nobody stays literal, like refOf', ['{points:}', '{followage:}', '{pointsname:alex}']],
  ['the chatter tokens take no payload', ['{chatters:5}', '{random.chatter:mods}', '{chatter}']],
  ['the uses token takes no payload', ['{uses:hug}']],
  ['a channel span addressing nobody stays literal', ['{title:}', '{channel.viewers:pokimane}', '{channel.followers}']],
  ['no emote token takes a payload', ['{7tvemotes:100}', '{random.emote:7tv}', '{twitchemotes}']],
  ['a payload the Go scope refuses stays literal', ['{quote:}', '{quote:seven}', '{quote:0}', '{time:}', '{song:2}']],
  ['a degenerate counter payload stays literal', ['{count:}', '{count:target:}']]
];

const LINE_CASES: [name: string, template: string, want: string[]][] = [
  ['a line a false conditional empties is dropped, its siblings are not', 'hi {user}\n{if:9: and word nine}\nlast line', ['hi sesame_sam', 'last line']],
  ['an emptied line does not eat a slot in the 5-message cap', 'one\n{if:9:two}\nthree\nfour\nfive', ['one', 'three', 'four', 'five']]
];

describe('rehearseCommand', () => {
  test.each(COMMAND_TEXT)('%s', (_name, template, want, ...overrides) => {
    expect(commandText(template, overrides[0])).toBe(want);
  });

  test.each(COMMAND_SAMPLED)('%s', (_name, template, want, samples) => {
    const [line] = rehearseCommand(template);
    expect(textOf(line.segments)).toBe(want);
    expect(line.segments.filter((s) => s.kind === 'sample')).toHaveLength(samples);
  });

  test.each(COMMAND_RESOLVED)('%s', (_name, template, want) => {
    const [line] = rehearseCommand(template);
    expect(textOf(line.segments)).toBe(want);
    expect(line.segments.every((seg) => seg.kind !== 'unknown')).toBe(true);
  });

  test.each(LITERAL_SPANS)('%s', (_name, spans) => {
    for (const span of spans) {
      expect(rehearseCommand(span)[0].segments).toEqual([{ text: span, kind: 'unknown' }]);
    }
  });

  test.each(LINE_CASES)('%s', (_name, template, want) => {
    expect(rehearseCommand(template).map((line) => textOf(line.segments))).toEqual(want);
  });

  test('alert-only tokens do NOT expand in a command (the bot leaves them literal)', () => {
    const [line] = rehearseCommand('{bits} {tier} {raider}');
    expect(line.segments.every((s) => s.kind === 'unknown' || s.text === ' ')).toBe(true);
  });

  test.each([
    ['counters resolve with engine normalization; only an empty name stays literal', '{counter:Deaths} {counter:bot:feeds} {counter:}', ['sample', 'plain', 'sample', 'plain', 'unknown'], '42 42 {counter:}'],
    ['target-addressed counters rehearse like the engine (issue #479)', '{counter:target:shutups} {counter:target:} {counter:target:bot:x}', ['sample', 'plain', 'unknown', 'plain', 'sample'], '42 {counter:target:} 42']
  ] as [string, string, SegKind[], string][])('%s', (_name, template, kinds, want) => {
    const [line] = rehearseCommand(template);
    expect(line.segments.map((s) => s.kind)).toEqual(kinds);
    expect(textOf(line.segments)).toBe(want);
  });

  test('caps at 5 messages, one per line, like emitCommand', () => {
    expect(rehearseCommand('a\nb\nc\nd\ne\nf')).toHaveLength(5);
  });

  test('routes each line its own slash verb', () => {
    const [a, b] = rehearseCommand('/announcegreen go {user}!\n/me waves');
    expect(a.mode).toBe('announce');
    expect(a.color).toBe('green');
    expect(textOf(a.segments)).toBe('go sesame_sam!');
    expect(b.mode).toBe('me');
    expect(textOf(b.segments)).toBe('waves');
  });

  test('longest announce verb wins ("/announceblue" is not "/announce")', () => {
    const [line] = rehearseCommand('/announceblue hi');
    expect(line.verb).toBe('/announceblue');
    expect(line.color).toBe('blue');
  });

  test('shoutout consumes the target (leading @ dropped)', () => {
    const [line] = rehearseCommand('/shoutout @{target} go watch');
    expect(line.mode).toBe('shoutout');
    expect(line.target).toBe('ferret_king');
    expect(textOf(line.segments)).toBe('go watch');
  });

  test('verbs route AFTER expansion, matching the engine order', () => {
    const [line] = rehearseCommand('{choice:/pin read the rules,/announce hi}');
    expect(line.mode).toBe('pin');
    expect(textOf(line.segments)).toBe('read the rules');
  });

  test('the pipe fallback renders when a token resolves to empty', () => {
    const [line] = rehearseCommand('shout out to {args|everyone}', { args: '' });
    expect(textOf(line.segments)).toBe('shout out to everyone');
    expect(line.segments.at(-1)?.kind).toBe('sample');
  });

  test('a utility with no payload stays literal', () => {
    const [line] = rehearseCommand('{math} {repeat}');
    expect(line.segments.filter((s) => s.kind === 'unknown').length).toBe(2);
  });
});

describe('rehearseReply', () => {
  test('substitutes only the given samples; command tokens stay unknown', () => {
    const [line] = rehearseReply('{user} {args}', { user: 'sam' });
    expect(line.segments).toEqual([
      { text: 'sam', kind: 'sample' },
      { text: ' ', kind: 'plain' },
      { text: '{args}', kind: 'unknown' }
    ]);
  });

  test('resolves dynamic tokens by default (module ExpandString fallback)', () => {
    const [line] = rehearseReply('{choice:hey,yo} {random}', {});
    expect(textOf(line.segments)).toBe('hey 57');
  });

  test('dynamic=false mirrors bare replacers (govee, clip): nothing but samples', () => {
    const [line] = rehearseReply('{user} {random}', { user: 'sam' }, { dynamic: false });
    expect(textOf(line.segments)).toBe('sam {random}');
    expect(line.segments.at(-1)?.kind).toBe('unknown');
  });

  test('routes slash verbs like any emitted output', () => {
    const [line] = rehearseReply('/announce big news', {});
    expect(line.mode).toBe('announce');
    expect(line.color).toBe('primary');
    expect(textOf(line.segments)).toBe('big news');
  });

  test('routes verbs after expansion, matching the emit order', () => {
    const [line] = rehearseReply('{opener} everyone', { opener: '/pin welcome' });
    expect(line.mode).toBe('pin');
    expect(textOf(line.segments)).toBe('welcome everyone');
  });

  test('/me renders as an italic action on reply surfaces too', () => {
    const [line] = rehearseReply('/me thanks {user} warmly', { user: 'sam' });
    expect(line.mode).toBe('me');
    expect(textOf(line.segments)).toBe('thanks sam warmly');
  });

  test.each([
    ['a module reply mounts no utility scope either', '{math:1+1}'],
    ['a module reply has no use count, so it stays literal there', '{uses}']
  ])('%s', (_name, span) => {
    expect(rehearseReply(span, {})[0].segments).toEqual([{ text: span, kind: 'unknown' }]);
  });

  test('a module reply mounts no message or counter scope', () => {
    const [line] = rehearseReply('{args} {counter:deaths}', {});
    expect(line.segments.every((s) => s.kind === 'unknown' || s.text === ' ')).toBe(true);
  });

  test('is a single message: no multi-line fan-out', () => {
    expect(rehearseReply('a\nb', {})).toHaveLength(1);
  });

  test('an empty template rehearses nothing', () => {
    expect(rehearseReply('  \n ', {})).toHaveLength(0);
  });

  test('matches dotted token names like {raider.login}', () => {
    const [line] = rehearseReply('twitch.tv/{raider.login}', { 'raider.login': 'crustycrumbs' });
    expect(textOf(line.segments)).toBe('twitch.tv/crustycrumbs');
  });
});

describe('rehearseTimer', () => {
  test('{user} stays literal: a tick has no chatter behind it', () => {
    const [line] = rehearseTimer('hi {user}!');
    expect(textOf(line.segments)).toBe('hi {user}!');
    expect(line.segments.find((s) => s.text === '{user}')?.kind).toBe('unknown');
  });

  test('resolves the channel-fact and dice families, like a tick actually can', () => {
    const [line] = rehearseTimer('{uptime} / {random}');
    expect(textOf(line.segments)).not.toContain('{uptime}');
    expect(textOf(line.segments)).not.toContain('{random}');
  });

  test('{urlfetch:…} resolves to the shared external stand-in, not "unknown"', () => {
    const [line] = rehearseTimer('weather: {urlfetch:weather}');
    const seg = line.segments.find((s) => s.text !== 'weather: ');
    expect(seg?.kind).toBe('sample');
    expect(timerOwns('urlfetch')).toBe(true);
  });

  test('an empty message rehearses nothing', () => {
    expect(rehearseTimer('  \n ')).toHaveLength(0);
  });
});

describe('resolvedWithoutSample', () => {
  test.each([
    { name: 'a dice token needs no sample in a command', token: 'random', kind: 'command', want: true },
    { name: 'a conditional needs no sample in a command', token: 'if', kind: 'command', want: true },
    { name: 'a message token needs its sample in a command', token: 'user', kind: 'command', want: false },
    { name: 'an unknown token is not resolved in a command', token: 'nosuchtoken', kind: 'command', want: false },
    { name: 'a dice token needs no sample in a reply', token: 'random', kind: 'reply', want: true },
    { name: 'a message token needs its sample in a reply', token: 'user', kind: 'reply', want: false }
  ] satisfies { name: string; token: string; kind: ChainKind; want: boolean }[])('$name', ({ token, kind, want }) => {
    expect(resolvedWithoutSample(token, kind)).toBe(want);
  });
});

describe('module state (engine/namespaced_vars.go mirror)', () => {
  test('a variable of a module that is off stays literal, fallback included', () => {
    const [line] = rehearseCommand('{valorant:rank:tier|unranked}', undefined, { valorant: false });
    expect(line.segments).toEqual([{ text: '{valorant:rank:tier|unranked}', kind: 'unknown' }]);
  });

  test('a condition on a module that is off stays literal', () => {
    const span = '{if:valorant:rank:tier=Gold 2:yes:no}';
    const [line] = rehearseCommand(span, undefined, { valorant: false });
    expect(line.segments).toEqual([{ text: span, kind: 'unknown' }]);
  });

  test('a module that is on, or absent from the flags, previews its sample', () => {
    for (const modules of [{ valorant: true }, {}]) {
      const [line] = rehearseCommand('{valorant:tier}', undefined, modules);
      expect(line.segments).toEqual([{ text: 'Immortal 2', kind: 'sample' }]);
    }
  });

  test('a module whose parent is off stays literal', () => {
    const [line] = rehearseCommand('{gamble:roll}', undefined, { gamble: true, loyalty: false });
    expect(line.segments).toEqual([{ text: '{gamble:roll}', kind: 'unknown' }]);
  });

  test('timers apply the same gate', () => {
    const [line] = rehearseTimer('{valorant:tier}', { valorant: false });
    expect(line.segments).toEqual([{ text: '{valorant:tier}', kind: 'unknown' }]);
  });
});
