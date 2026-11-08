// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { expect, test } from 'bun:test';
import { parseDocument, pageLinkError } from '../../scripts/content-policy';

const page = (body: string, metadata = '') => parseDocument(`---\ntitle: "A page"\ndescription: "A useful description"\ndocType: reference\n${metadata}---\n\n${body}`);

test('the policy rejects missing metadata and malformed YAML', () => {
  expect(parseDocument('# No metadata').errors).toContain('YAML frontmatter is required');
  expect(parseDocument('---\ntitle: [\n---\n').errors.join()).toContain('Invalid YAML');
  expect(parseDocument('---\ntitle: Title\n---\n').errors.join()).toContain('description must be a non-empty string');
  expect(parseDocument('---\ntitle: Title\ndescription: Description\ndocType: mystery\n---\n').errors.join()).toContain('docType must be one of');
});

test('Markdown parsing separates code samples, links, and actual headings', () => {
  const result = page('## Read `user_id`\n\n[Contract][rpc]\n\n[rpc]: /reference/rpc-contracts/\n\n```sh\n# A shell comment\necho "[not a link](/missing/)"\n```\n\n## Read `user_id`\n\n![State diagram](/diagram.png)\n');
  expect(result.errors).toEqual([]);
  expect([...result.anchors]).toEqual(['read-user_id', 'read-user_id-1']);
  expect(result.links).toEqual(['/reference/rpc-contracts/']);
  expect(result.assets).toEqual(['/diagram.png']);
});

test('links must resolve to an existing page and heading, including French accents', () => {
  const pages = new Map([['reference/example', page('## Delivery behavior')], ['fr/reference/example', page('## État partagé')]]);
  expect(pageLinkError('/reference/example/#delivery-behavior', 'start', pages)).toBeUndefined();
  expect(pageLinkError('/fr/reference/example/#%C3%A9tat-partag%C3%A9', 'start', pages)).toBeUndefined();
  expect(pageLinkError('/reference/missing/', 'start', pages)).toContain('Missing page');
  expect(pageLinkError('#unknown', 'reference/example', pages)).toContain('Missing heading anchor');
  expect(pageLinkError('https://example.com/#external', 'start', pages)).toBeUndefined();
});

test('the policy catches inaccessible images and skipped heading levels', () => {
  expect(page('## Overview\n\n#### Skipped\n\n![](/image.png)').errors.join()).toContain('Heading skips a level');
  expect(page('![](/image.png)').errors.join()).toContain('Image needs descriptive alt text');
});
