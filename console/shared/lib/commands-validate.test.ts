import { describe, expect, test } from 'bun:test';
import { normalizeCommandResponse, validateCommand } from './commands-validate';

const validFields = {
  name: 'raid',
  aliases: [],
  cooldown: 0,
  allowedUserId: ''
};

describe('command response normalization', () => {
  test('preserves message boundaries as LF separators', () => {
    expect(normalizeCommandResponse('first message\r\nsecond message')).toBe('first message\nsecond message');
    expect(normalizeCommandResponse('first message\rsecond message')).toBe('first message\nsecond message');
  });

  test('drops blank lines and trailing whitespace without joining messages', () => {
    expect(normalizeCommandResponse('first  \n\nsecond\t\n')).toBe('first\nsecond');
  });

  test('rejects non-newline control characters', () => {
    expect(validateCommand({ ...validFields, response: 'first\u0000second' }).response).toBe(
      'Response cannot contain control characters.'
    );
  });
});
