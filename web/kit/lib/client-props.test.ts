// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { expect, test } from 'bun:test';
import { serializeClientProps } from './client-props';

test('component props preserve HTML, localized text, and nested values without ending the script', () => {
  const props = {
    title: '</script><script>alert("hello")</script>',
    french: 'Réponse < 5 — 🥯',
    options: [{ label: '"quoted" & <tag>', value: 0 }],
  };
  const serialized = serializeClientProps(props);
  expect(serialized).not.toContain('<');
  expect(JSON.parse(serialized)).toEqual(props);
});
