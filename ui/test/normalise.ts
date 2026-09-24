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

function stripHtmlComments(html: string): string {
  let prev;
  do {
    prev = html;
    html = html.replace(/<!--[\s\S]*?-->/g, '');
  } while (html !== prev);
  return html;
}

