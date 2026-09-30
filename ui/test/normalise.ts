// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export function normalise(html: string): string {
  const stripped = stripHtmlComments(
    html
      .replace(/<script type="module" src="[^"]*"><\/script>/g, '')
      .replace(/\s*\/>/g, '>'),
  );
  return stripped
    .replace(/<!>/g, '')
    .replace(/\s*=\s*(""|'')/g, '')
    .replace(/>\s+</g, '><')
    .replace(/\s+/g, ' ')
    .trim();
}

export function removeAll(text: string, pattern: RegExp): string {
  let prev;
  do {
    prev = text;
    text = text.replace(pattern, '');
  } while (text !== prev);
  return text;
}

function stripHtmlComments(html: string): string {
  return removeAll(html, /<!--[\s\S]*?-->/g);
}

