// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

/** Keep JSON inert even when a prop contains an HTML script closing tag. */
export function serializeClientProps(value: unknown): string {
  return JSON.stringify(value).replace(/</g, '\\u003c');
}

/** Props belong to this component, never to a nested component or another page. */
export function readClientProps<T>(host: Element): T {
  const payload = host.matches('script.bb-client-props') ? host : host.querySelector(':scope > script.bb-client-props');
  if (!payload) throw new Error('Missing component client props');
  return JSON.parse(payload.textContent ?? '') as T;
}
