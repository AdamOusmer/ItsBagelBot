#!/usr/bin/env bun
// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { readFileSync, writeFileSync } from 'node:fs';
import { join, resolve } from 'node:path';

const root = resolve(import.meta.dir, '../..');
const target = join(root, 'deploy/k8s/config/error-pages.html');
const ui = (path: string) => readFileSync(join(root, 'ui', path), 'utf8');

const FONTS = ['syne-latin.woff2', 'dm-sans-latin.woff2', 'dm-mono-400.woff2'];

type Group = { name: string; codes: number[]; action: 'home' | 'signIn' | 'retry' };

// Order matters: the first group whose codes match wins, and the two catch-alls go last.
const GROUPS: Group[] = [
  { name: 'notFound', codes: [404, 410], action: 'home' },
  { name: 'access', codes: [401, 403], action: 'signIn' },
  { name: 'rateLimited', codes: [429], action: 'retry' },
  { name: 'unavailable', codes: [502, 503, 504], action: 'retry' },
  { name: 'server', codes: [], action: 'retry' },
  { name: 'unexpected', codes: [], action: 'retry' },
];

const ACTIONS = {
  home: { key: 'backToDashboard', href: '/' },
  signIn: { key: 'signIn', href: '/login' },
  retry: { key: 'tryAgain', href: '' },
};

const FIELDS = ['Eyebrow', 'Title', 'Description'];
const SHARED = ['aside', 'goBack', ...new Set(Object.values(ACTIONS).map((action) => action.key))];

type Copy = Record<string, string>;

function catalogs(): Record<string, Copy> {
  const codes = JSON.parse(readFileSync(join(root, 'locales/manifest.json'), 'utf8')) as string[];
  const read = (code: string) => JSON.parse(readFileSync(join(root, 'locales', code, 'console/error.json'), 'utf8')) as Copy;
  const en = read('en');
  const wanted = [...GROUPS.flatMap((group) => FIELDS.map((field) => group.name + field)), ...SHARED];
  return Object.fromEntries(
    codes.map((code) => {
      const merged = { ...en, ...read(code) };
      const copy = Object.fromEntries(wanted.map((key) => [key, merged[key].replace('{app}', merged.appDashboard)]));
      return [code, copy];
    }),
  );
}

function fontFaces(): string {
  const css = ui('styles/fonts.css');
  return [...css.matchAll(/@font-face\s*\{[^}]*\}/g)]
    .map(([block]) => block)
    .filter((block) => FONTS.some((file) => block.includes(file)))
    .map((block) =>
      block.replace(/url\("\.\.\/fonts\/([^"]+)"\)/, (_, file: string) => {
        const data = readFileSync(join(root, 'ui/fonts', file)).toString('base64');
        return `url(data:font/woff2;base64,${data})`;
      }),
    )
    .join('\n');
}

function tokens(): string {
  return ui('styles/brand.css').replace(/^@import .*$/m, '');
}

const BASE = `*,*::before,*::after{box-sizing:border-box}
html,body{margin:0;min-height:100%;background:var(--bb-black);color:var(--bb-white);-webkit-font-smoothing:antialiased}`;

async function script(): Promise<string> {
  const built = await Bun.build({ entrypoints: [join(import.meta.dir, 'client.ts')], minify: true, target: 'browser' });
  if (!built.success) throw new AggregateError(built.logs, 'client bundle failed');
  return (await built.outputs[0].text()).trim();
}

function escape(text: string): string {
  return text.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
}

function condition(group: Group): string {
  return group.codes.length === 1
    ? `eq .StatusCode ${group.codes[0]}`
    : `or ${group.codes.map((code) => `(eq .StatusCode ${code})`).join(' ')}`;
}

function branches(render: (group: Group) => string): string {
  const named = GROUPS.filter((group) => group.codes.length > 0);
  const server = GROUPS.find((group) => group.name === 'server')!;
  const fallback = GROUPS.find((group) => group.name === 'unexpected')!;
  const head = named.map((group, i) => `{{ ${i === 0 ? 'if' : 'else if'} ${condition(group)} }}${render(group)}`).join('');
  return `${head}{{ else if ge .StatusCode 500 }}${render(server)}{{ else }}${render(fallback)}{{ end }}`;
}

function primary(group: Group, en: Copy): string {
  const action = ACTIONS[group.action];
  return `<a class="bb-error-scene__action bb-error-scene__action--primary" href="${action.href}" data-copy="${action.key}">${escape(en[action.key])}</a>`;
}

function page(copy: Record<string, Copy>, css: string, js: string): string {
  const en = copy.en;
  const field = (name: string) => branches((group) => escape(en[group.name + name]));
  return `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="robots" content="noindex">
<meta name="color-scheme" content="dark">
<title>{{ .StatusCode }}: ItsBagelBot</title>
<style>${css}</style>
</head>
<body>
<main class="bb-error-scene" aria-labelledby="error-title" data-group="${branches((group) => group.name)}">
<div class="bb-light-field" data-field aria-hidden="true"></div>
<div class="bb-error-scene__glow" aria-hidden="true"></div>
<div class="bb-error-scene__orbits" aria-hidden="true"><span class="bb-error-scene__orbit"></span><span class="bb-error-scene__orbit bb-error-scene__orbit--two"></span></div>
<div class="bb-error-scene__content">
<p class="bb-error-scene__eyebrow"><span>{{ .StatusCode }}</span> · <b data-copy="*Eyebrow">${field('Eyebrow')}</b></p>
<p class="bb-error-scene__code" aria-hidden="true">{{ .StatusCode }}</p>
<h1 class="bb-error-scene__title" id="error-title" data-copy="*Title">${field('Title')}</h1>
<p class="bb-error-scene__desc" data-copy="*Description">${field('Description')}</p>
<div class="bb-error-scene__actions">${branches((group) => primary(group, en))}<a class="bb-error-scene__action bb-error-scene__action--quiet" href="/" data-error-back data-copy="goBack">${escape(en.goBack)}</a></div>
<p class="bb-error-scene__aside" data-copy="aside">${escape(en.aside)}</p>
</div>
</main>
<script type="application/json" id="error-copy">${JSON.stringify(copy).replace(/</g, '\\u003c')}</script>
<script>${js}</script>
</body>
</html>
`;
}

async function render(): Promise<string> {
  const css = [fontFaces(), tokens(), BASE, ui('styles/elements/light-field.css'), ui('styles/elements/error-scene.css'), '.bb-error-scene__eyebrow b{font-weight:inherit}'].join('\n');
  const js = await script();
  const copy = catalogs();
  for (const [name, asset] of [['css', css], ['js', js], ['copy', JSON.stringify(copy)]]) {
    if (asset.includes('{{')) throw new Error(`${name} contains "{{", which error-pages parses as a Go template action`);
  }
  return page(copy, css, js);
}

const next = await render();
if (process.argv.includes('--check')) {
  let current = '';
  try {
    current = readFileSync(target, 'utf8');
  } catch {}
  if (current !== next) {
    console.error('gen-error-page: deploy/k8s/config/error-pages.html is stale. Run `bun scripts/error-page/gen.ts` and commit the result.');
    process.exit(1);
  }
  console.log('error page template is current');
} else {
  writeFileSync(target, next);
  console.log(`gen-error-page: wrote ${target} (${next.length} bytes)`);
}
