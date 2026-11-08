// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { fromMarkdown } from 'mdast-util-from-markdown';
import GithubSlugger from 'github-slugger';
import { DOC_TYPES, type DocType } from '../src/lib/content-types';

type Node = {
  type: string;
  value?: string;
  url?: string;
  identifier?: string;
  alt?: string | null;
  depth?: number;
  lang?: string | null;
  children?: Node[];
};
export interface Document {
  title: string;
  docType?: DocType;
  anchors: Set<string>;
  links: string[];
  assets: string[];
  diagrams: string[];
  errors: string[];
}

export function parseDocument(text: string): Document {
  const result: Document = { title: '', anchors: new Set(), links: [], assets: [], diagrams: [], errors: [] };
  const frontmatter = text.match(/^---\r?\n([\s\S]*?)\r?\n---(?:\r?\n|$)/);
  if (!frontmatter) { result.errors.push('YAML frontmatter is required'); return result; }
  try {
    const meta = Bun.YAML.parse(frontmatter[1]) as Record<string, unknown>;
    for (const key of ['title', 'description']) {
      if (typeof meta?.[key] !== 'string' || !(meta[key] as string).trim()) result.errors.push(`${key} must be a non-empty string`);
    }
    if (typeof meta?.title === 'string') result.title = meta.title;
    if (!DOC_TYPES.includes(meta?.docType as DocType)) result.errors.push(`docType must be one of ${DOC_TYPES.join(', ')}`);
    else result.docType = meta.docType as DocType;
  } catch (error) { result.errors.push(`Invalid YAML: ${String(error)}`); }
  const tree = fromMarkdown(text.slice(frontmatter[0].length));
  const slugger = new GithubSlugger();
  const definitions = new Map<string, string>();
  const references: { identifier: string; image: boolean }[] = [];
  let previousDepth = 1;
  const label = (node: Node): string => node.value ?? node.alt ?? node.children?.map(label).join('') ?? '';
  function visit(node: Node) {
    if (node.type === 'heading') {
      if (node.depth === 1) result.errors.push('Body headings must start at level two; the layout renders the title');
      if (node.depth! > previousDepth + 1) result.errors.push(`Heading skips a level: ${label(node)}`);
      previousDepth = node.depth!;
      result.anchors.add(slugger.slug(label(node)));
    }
    if (node.type === 'code' && node.lang === 'mermaid') result.diagrams.push(node.value!);
    if (node.type === 'link') result.links.push(node.url!);
    if (node.type === 'image') result.assets.push(node.url!);
    if ((node.type === 'image' || node.type === 'imageReference') && !node.alt?.trim()) result.errors.push(`Image needs descriptive alt text: ${node.url}`);
    if (node.type === 'definition') definitions.set(node.identifier!, node.url!);
    if (node.type === 'linkReference' || node.type === 'imageReference') references.push({ identifier: node.identifier!, image: node.type === 'imageReference' });
    node.children?.forEach(visit);
  }
  visit(tree);
  for (const reference of references) {
    const href = definitions.get(reference.identifier);
    if (href) (reference.image ? result.assets : result.links).push(href);
    else result.errors.push(`Missing link definition: ${reference.identifier}`);
  }
  return result;
}

/** Check published routes and heading anchors without making network requests. */
export function pageLinkError(href: string, currentId: string, pages: Map<string, Document>): string | undefined {
  const url = new URL(href, `https://docs.itsbagelbot.com/${currentId}/`);
  if (url.origin !== 'https://docs.itsbagelbot.com') return;
  const targetId = decodeURIComponent(url.pathname).replace(/^\//, '').replace(/\/$/, '').replace(/\.md$/, '').replace(/\/index$/, '');
  if (!targetId) return; // The generated home page has no Markdown entry.
  const target = pages.get(targetId);
  if (!target) return `Missing page: ${href}`;
  if (url.hash && !target.anchors.has(decodeURIComponent(url.hash.slice(1)))) return `Missing heading anchor: ${href}`;
}
