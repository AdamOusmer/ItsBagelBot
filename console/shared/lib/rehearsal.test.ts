// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import { expandSegments, rehearseCommand, rehearseReply, type Seg, type Token } from './rehearsal';

/** The rehearsed chat text of a line: what the bot would actually send. */
function textOf(segments: Seg[]): string {
  return segments.map((s) => s.text).join('');
}

describe('token expansion (module/vars.go Expand mirror)', () => {
  const resolve = (token: Token) => (token.name === 'user' ? 'sam' : null);

  test('substitutes known tokens and marks them as samples', () => {
    expect(expandSegments('hi {user}!', resolve)).toEqual([
      { text: 'hi ', kind: 'plain' },
      { text: 'sam', kind: 'sample' },
      { text: '!', kind: 'plain' }
    ]);
  });

  test('token names are case-insensitive, payloads keep their case', () => {
    // module.Expand lowercases the name before the resolver sees it.
    expect(expandSegments('{User}', resolve)).toEqual([{ text: 'sam', kind: 'sample' }]);
    const choice = expandSegments('{CHOICE:Hi,Yo}', (t) => (t.key === 'choice:Hi,Yo' ? 'Hi' : null));
    expect(choice).toEqual([{ text: 'Hi', kind: 'sample' }]);
  });

  test('any brace span is a token, unknown ones stay literal but marked', () => {
    expect(expandSegments('{touser2} {foo bar}', resolve)).toEqual([
      { text: '{touser2}', kind: 'unknown' },
      { text: ' ', kind: 'plain' },
      { text: '{foo bar}', kind: 'unknown' }
    ]);
  });

  test('a "{" with no closing brace is copied literally', () => {
    expect(expandSegments('oops {user', resolve)).toEqual([{ text: 'oops {user', kind: 'plain' }]);
  });

  test('an empty-string resolution drops the token from the output', () => {
    // Mirrors e.g. a reward's {input} with no input: ok=true, value "".
    expect(textOf(expandSegments('[{gone}]', () => ''))).toBe('[]');
  });
});

describe('rehearseCommand', () => {
  test('substitutes exactly the expandCommand token set', () => {
    const [line] = rehearseCommand('{user} {target} {args} {channel}');
    expect(textOf(line.segments)).toBe(
      'sesame_sam ferret_king ferret_king good luck bagel_bakery'
    );
    expect(line.segments.filter((s) => s.kind === 'sample')).toHaveLength(4);
  });

  test('the identity tokens substitute too', () => {
    const [line] = rehearseCommand('{userid} {user.login} !{command}');
    expect(textOf(line.segments)).toBe('48291057 sesame_sam !hug');
    expect(line.segments.filter((s) => s.kind === 'sample')).toHaveLength(3);
  });

  test('positional words come from the {args} sample, so the two agree', () => {
    const [line] = rehearseCommand('{1} / {2} / {2:} / {1:}');
    expect(textOf(line.segments)).toBe('ferret_king / good / good luck / ferret_king good luck');
  });

  test('an override of {args} moves the positional samples with it', () => {
    const [line] = rehearseCommand('{1} then {2:}', { args: 'alex two three' });
    expect(textOf(line.segments)).toBe('alex then two three');
  });

  test('a word past the end renders its fallback, or nothing', () => {
    const [line] = rehearseCommand('[{9}] {9|nobody}');
    expect(textOf(line.segments)).toBe('[] nobody');
  });

  test('a span that only looks like a word number stays literal', () => {
    const [line] = rehearseCommand('{0} {31} {01} {+1} {1:2}');
    expect(textOf(line.segments)).toBe('{0} {31} {01} {+1} {1:2}');
    expect(line.segments.every((s) => s.kind !== 'sample')).toBe(true);
  });

  test('{sender}/{target} are aliases; an override of the canonical covers them', () => {
    const [line] = rehearseCommand('{sender} waves at {target}', { user: 'maya_live', touser: 'alex' });
    expect(textOf(line.segments)).toBe('maya_live waves at alex');
  });

  test('alert-only tokens do NOT expand in a command (the bot leaves them literal)', () => {
    const [line] = rehearseCommand('{bits} {viewers} {raider}');
    expect(line.segments.every((s) => s.kind === 'unknown' || s.text === ' ')).toBe(true);
  });

  test('resolves dynamic tokens deterministically', () => {
    const [line] = rehearseCommand('{random} {random:10-20} {choice:a,b,c}');
    expect(textOf(line.segments)).toBe('57 15 a');
  });

  test('invalid random ranges stay literal, like ParseDynamic ok=false', () => {
    const [line] = rehearseCommand('{random:5-1} {random:x-y}');
    expect(textOf(line.segments)).toBe('{random:5-1} {random:x-y}');
  });

  test('counters resolve with engine normalization; bot counters stay literal', () => {
    const [line] = rehearseCommand('{counter:Deaths} {counter:bot:feeds} {counter:}');
    expect(line.segments.map((s) => s.kind)).toEqual(['sample', 'plain', 'unknown', 'plain', 'unknown']);
    expect(textOf(line.segments)).toBe('42 {counter:bot:feeds} {counter:}');
  });

  test('target-addressed counters rehearse like the engine (issue #479)', () => {
    const [line] = rehearseCommand('{counter:target:shutups} {counter:target:} {counter:target:bot:x}');
    expect(line.segments.map((s) => s.kind)).toEqual(['sample', 'plain', 'unknown', 'plain', 'unknown']);
    expect(textOf(line.segments)).toBe('42 {counter:target:} {counter:target:bot:x}');
  });

  test('caps at 5 messages, one per line, like emitResponse', () => {
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
    // emitResponse expands first, then translates: a verb minted by a token
    // still becomes the native action.
    const [line] = rehearseCommand('{choice:/pin read the rules,/announce hi}');
    expect(line.mode).toBe('pin');
    expect(textOf(line.segments)).toBe('read the rules');
  });
});

describe('scope chain (engine/scope mirror)', () => {
  test('the pipe fallback renders when a token resolves to empty', () => {
    const [line] = rehearseCommand('shout out to {args|everyone}', { args: '' });
    expect(textOf(line.segments)).toBe('shout out to everyone');
    expect(line.segments.at(-1)?.kind).toBe('sample');
  });

  test('a present value ignores its fallback', () => {
    const [line] = rehearseCommand('hi {user|everyone}');
    expect(textOf(line.segments)).toBe('hi sesame_sam');
  });

  test('a fallback never rescues a name no scope owns', () => {
    // Treating the fallback as proof the token exists would hide the typo
    // forever, which is the whole reason unknown spans stay literal.
    const [line] = rehearseCommand('{typo|rescued}');
    expect(line.segments).toEqual([{ text: '{typo|rescued}', kind: 'unknown' }]);
  });

  test('an identity token with a payload stays literal, like Message.Get', () => {
    const [line] = rehearseCommand('{user:bob}');
    expect(line.segments).toEqual([{ text: '{user:bob}', kind: 'unknown' }]);
  });

  test('the pure scope answers first, so a sample cannot shadow the dice', () => {
    const [line] = rehearseCommand('{random}', { random: 'nope' });
    expect(textOf(line.segments)).toBe('57');
  });

  test('a module reply mounts no message or counter scope', () => {
    // Only a custom command has {args} and {counter:…}; a reply that names
    // them is naming tokens its module does not have.
    const [line] = rehearseReply('{args} {counter:deaths}', {});
    expect(line.segments.every((s) => s.kind === 'unknown' || s.text === ' ')).toBe(true);
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
    // The bot sends "/me …" as plain chat (Twitch renders the action itself);
    // the rehearsal shows the rendered form: me mode, verb stripped.
    const [line] = rehearseReply('/me thanks {user} warmly', { user: 'sam' });
    expect(line.mode).toBe('me');
    expect(textOf(line.segments)).toBe('thanks sam warmly');
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
