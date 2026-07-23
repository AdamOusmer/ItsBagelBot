import { describe, expect, test } from 'bun:test';
import { expandSegments, rehearseCommand, rehearseReply, type Seg } from './rehearsal';

/** The rehearsed chat text of a line: what the bot would actually send. */
function textOf(segments: Seg[]): string {
  return segments.map((s) => s.text).join('');
}

describe('token expansion (module/vars.go Expand mirror)', () => {
  const resolve = (key: string) => (key === 'user' ? 'sam' : null);

  test('substitutes known tokens and marks them as samples', () => {
    expect(expandSegments('hi {user}!', resolve)).toEqual([
      { text: 'hi ', kind: 'plain' },
      { text: 'sam', kind: 'sample' },
      { text: '!', kind: 'plain' }
    ]);
  });

  test('keys are case-sensitive, exactly like the engine', () => {
    // expandCommand matches "user", not "User": the bot leaves {User} literal.
    expect(expandSegments('{User}', resolve)).toEqual([{ text: '{User}', kind: 'unknown' }]);
  });

  test('any brace span is a token — unknown ones stay literal but marked', () => {
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
    expect(textOf(line.segments)).toBe('sesame_sam ferret_king aaaa bagel_bakery');
    expect(line.segments.filter((s) => s.kind === 'sample')).toHaveLength(4);
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

  test('never routes slash verbs — reply surfaces skip Translate', () => {
    const [line] = rehearseReply('/announce big news', {});
    expect(line.mode).toBe('chat');
    expect(textOf(line.segments)).toBe('/announce big news');
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
