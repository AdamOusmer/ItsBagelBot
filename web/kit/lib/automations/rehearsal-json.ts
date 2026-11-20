// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { trimSpace } from './text';

type Text = string;
type JsonNode = { scalar?: Text; fields?: Map<Text, JsonNode>; items?: JsonNode[] };

class JsonReader {
  position = 0;
  constructor(readonly tokens: Text[]) {}

  read(): JsonNode {
    const token = this.tokens[this.position++]!;
    if (token === '{') return this.object();
    if (token === '[') return this.array();
    if (token === 'null') return {};
    return { scalar: token.startsWith('"') ? JSON.parse(token) : token };
  }

  object(): JsonNode {
    const fields = new Map<Text, JsonNode>();
    while (this.tokens[this.position] !== '}') {
      const key = JSON.parse(this.tokens[this.position++]!);
      this.position++;
      fields.set(key, this.read());
      this.skipComma();
    }
    this.position++;
    return { fields };
  }

  array(): JsonNode {
    const items: JsonNode[] = [];
    while (this.tokens[this.position] !== ']') {
      items.push(this.read());
      this.skipComma();
    }
    this.position++;
    return { items };
  }

  skipComma(): void {
    if (this.tokens[this.position] === ',') this.position++;
  }
}

function field(node: JsonNode | undefined, key: Text): JsonNode | undefined {
  return /^\d+$/.test(key) ? node?.items?.[Number(key)] : node?.fields?.get(key);
}

function stringEnd(response: Text, start: number): number {
  let position = start + 1;
  while (response[position] !== '"') {
    position += response[position] === '\\' ? 2 : 1;
  }
  return position + 1;
}

function tokenEnd(response: Text, start: number): number {
  if (response[start] === '"') return stringEnd(response, start);
  if ('{}[],:'.includes(response[start]!)) return start + 1;
  let position = start + 1;
  while (position < response.length && !' \t\r\n{}[],:'.includes(response[position]!)) position++;
  return position;
}

function jsonTokens(response: Text): Text[] {
  const tokens: Text[] = [];
  let position = 0;
  while (position < response.length) {
    if (' \t\r\n'.includes(response[position]!)) {
      position++;
      continue;
    }
    const end = tokenEnd(response, position);
    tokens.push(response.slice(position, end));
    position = end;
  }
  return tokens;
}

export function extractJson(response: Text, path: Text[]): Text {
  if (path.length === 0) return trimSpace(response);
  JSON.parse(response);
  const tokens = jsonTokens(response);
  let node: JsonNode | undefined = new JsonReader(tokens).read();
  for (const key of path) node = field(node, key);
  if (node?.scalar === undefined) throw new Error('bad_def');
  return node.scalar;
}
