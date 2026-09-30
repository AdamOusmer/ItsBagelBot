// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { afterEach, describe, expect, test } from 'bun:test';
import { createRawSnippet } from 'svelte';
import { render } from 'svelte/server';
import { experimental_AstroContainer } from 'astro/container';
import { focusWithin, isRendered, trapFocus, wirePopupAnchor } from '../lib/overlay-stack';
import { wireTooltip } from '../lib/tooltip';
import { normalise } from './normalise';
import SvelteModal from '../svelte/Modal.svelte';
import AstroModal from '../astro/Modal.astro';
import ConfirmDialog from '../svelte/ConfirmDialog.svelte';
import SvelteTooltip from '../svelte/Tooltip.svelte';
import AstroTooltip from '../astro/Tooltip.astro';
import Popover from '../svelte/Popover.svelte';
import PickerPanel from '../svelte/PickerPanel.svelte';
import ProfileMenu from '../svelte/ProfileMenu.svelte';
import SvelteCopySurface from '../svelte/CopySurface.svelte';
import AstroCopySurface from '../astro/CopySurface.astro';

const snippet = (html: string) => createRawSnippet(() => ({ render: () => html }));
const html = (component: never, props: Record<string, unknown>) =>
  normalise(render(component, { props: props as never }).body).replace(/<!--[\s\S]*?-->/g, '');

class FakeElement {
  attrs = new Map<string, string>();
  listeners = new Map<string, (event: never) => void>();
  focused = false;
  focusOptions: unknown;
  hovered = false;
  focusWithinNow = false;
  children: FakeElement[] = [];
  id = '';
  checkVisibility?: () => boolean;
  constructor(
    public offsetParent: unknown = {},
    visibility?: boolean,
    public rects = 1,
  ) {
    if (visibility !== undefined) this.checkVisibility = () => visibility;
  }
  getClientRects() {
    return { length: this.rects };
  }
  hasAttribute(name: string) {
    return this.attrs.has(name);
  }
  getAttribute(name: string) {
    return this.attrs.get(name) ?? null;
  }
  setAttribute(name: string, value: string) {
    this.attrs.set(name, value);
  }
  removeAttribute(name: string) {
    this.attrs.delete(name);
  }
  matches(selector: string) {
    return selector === ':hover' ? this.hovered : selector === ':focus-within' ? this.focusWithinNow : this.hovered || this.focusWithinNow;
  }
  contains(node: unknown) {
    return node === this || this.children.includes(node as FakeElement);
  }
  querySelector() {
    return this.children[0] ?? null;
  }
  querySelectorAll() {
    return this.children;
  }
  addEventListener(type: string, fn: (event: never) => void) {
    this.listeners.set(type, fn);
  }
  removeEventListener(type: string) {
    this.listeners.delete(type);
  }
  focus(options?: unknown) {
    this.focused = true;
    this.focusOptions = options;
    fakeDocument.activeElement = this;
  }
}

const fakeDocument = { activeElement: null as unknown, listeners: new Map<string, (event: never) => void>() } as {
  activeElement: unknown;
  listeners: Map<string, (event: never) => void>;
  addEventListener(type: string, fn: (event: never) => void): void;
  removeEventListener(type: string): void;
  contains(node: unknown): boolean;
};
fakeDocument.addEventListener = (type, fn) => fakeDocument.listeners.set(type, fn);
fakeDocument.removeEventListener = (type) => fakeDocument.listeners.delete(type);
fakeDocument.contains = () => true;

const realDocument = globalThis.document;
const realRaf = globalThis.requestAnimationFrame;
const realCancel = globalThis.cancelAnimationFrame;
afterEach(() => {
  globalThis.document = realDocument;
  globalThis.requestAnimationFrame = realRaf;
  globalThis.cancelAnimationFrame = realCancel;
  fakeDocument.activeElement = null;
  fakeDocument.listeners.clear();
});

const asEl = (el: FakeElement) => el as unknown as HTMLElement;

describe('trapFocus visibility', () => {
  test('a fixed-position control has no offsetParent yet stays in the trap', () => {
    expect(isRendered(asEl(new FakeElement(null, true)))).toBe(true);
  });

  test('checkVisibility false drops the control even with an offsetParent', () => {
    expect(isRendered(asEl(new FakeElement({}, false)))).toBe(false);
  });

  test('without checkVisibility, client rects decide', () => {
    expect(isRendered(asEl(new FakeElement(null, undefined, 1)))).toBe(true);
    expect(isRendered(asEl(new FakeElement({}, undefined, 0)))).toBe(false);
  });

  test('Tab from the last control wraps to a fixed-position first control', () => {
    globalThis.document = fakeDocument as unknown as Document;
    globalThis.requestAnimationFrame = (() => 0) as never;
    globalThis.cancelAnimationFrame = (() => {}) as never;
    const fixed = new FakeElement(null, true);
    const last = new FakeElement({}, true);
    const node = new FakeElement();
    node.children = [fixed, last];
    trapFocus(asEl(node));
    last.focus();
    let prevented = false;
    node.listeners.get('keydown')?.({ key: 'Tab', shiftKey: false, preventDefault: () => (prevented = true) } as never);
    expect(prevented).toBe(true);
    expect(fakeDocument.activeElement).toBe(fixed);
  });
});

describe('popup anchor wiring', () => {
  test('sets popup state and controls only while open, and cleans up what it set', () => {
    const anchor = new FakeElement();
    const off = wirePopupAnchor(asEl(anchor), true, 'panel-1');
    expect(anchor.attrs.get('aria-haspopup')).toBe('dialog');
    expect(anchor.attrs.get('aria-expanded')).toBe('true');
    expect(anchor.attrs.get('aria-controls')).toBe('panel-1');
    off();
    expect(anchor.attrs.size).toBe(0);
    wirePopupAnchor(asEl(anchor), false, 'panel-1');
    expect(anchor.attrs.get('aria-expanded')).toBe('false');
    expect(anchor.attrs.has('aria-controls')).toBe(false);
  });

  test('attributes the caller already owns are left alone', () => {
    const anchor = new FakeElement();
    anchor.attrs.set('aria-controls', 'list');
    anchor.attrs.set('aria-haspopup', 'listbox');
    wirePopupAnchor(asEl(anchor), true, 'panel-1')();
    expect(anchor.attrs.get('aria-controls')).toBe('list');
    expect(anchor.attrs.get('aria-haspopup')).toBe('listbox');
  });

  test('focusWithin focuses the first control without scrolling, else the container', () => {
    const container = new FakeElement();
    const control = new FakeElement();
    container.children = [control];
    focusWithin(asEl(container));
    expect(control.focused).toBe(true);
    expect(control.focusOptions).toEqual({ preventScroll: true });
    const bare = new FakeElement();
    focusWithin(asEl(bare));
    expect(bare.focused).toBe(true);
  });
});

describe('tooltip wiring', () => {
  function setup() {
    globalThis.document = fakeDocument as unknown as Document;
    const root = new FakeElement();
    const trigger = new FakeElement();
    root.children = [trigger];
    const bubble = new FakeElement();
    bubble.id = 'tip';
    const off = wireTooltip(asEl(root), asEl(bubble));
    const press = (key: string) => {
      let stopped = false;
      fakeDocument.listeners.get('keydown')?.({ key, stopImmediatePropagation: () => (stopped = true) } as never);
      return stopped;
    };
    return { root, trigger, off, press };
  }

  test('the trigger is described by the bubble and the link is removed on teardown', () => {
    const { trigger, off } = setup();
    expect(trigger.attrs.get('aria-describedby')).toBe('tip');
    off();
    expect(trigger.attrs.has('aria-describedby')).toBe(false);
  });

  test('an existing description is kept alongside the tooltip', () => {
    globalThis.document = fakeDocument as unknown as Document;
    const root = new FakeElement();
    const trigger = new FakeElement();
    trigger.attrs.set('aria-describedby', 'hint');
    root.children = [trigger];
    const bubble = new FakeElement();
    bubble.id = 'tip';
    const off = wireTooltip(asEl(root), asEl(bubble));
    expect(trigger.attrs.get('aria-describedby')).toBe('hint tip');
    off();
    expect(trigger.attrs.get('aria-describedby')).toBe('hint');
  });

  test('Escape dismisses a shown tooltip and is consumed once', () => {
    const { root, press } = setup();
    root.focusWithinNow = true;
    expect(press('Escape')).toBe(true);
    expect(root.attrs.has('data-dismissed')).toBe(true);
    expect(press('Escape')).toBe(false);
  });

  test('Escape passes through when the tooltip is not shown', () => {
    const { root, press } = setup();
    expect(press('Escape')).toBe(false);
    expect(root.attrs.has('data-dismissed')).toBe(false);
  });

  test('other keys never dismiss', () => {
    const { root, press } = setup();
    root.hovered = true;
    expect(press('Enter')).toBe(false);
  });

  test('leaving hover and focus makes it show again', () => {
    const { root } = setup();
    root.hovered = true;
    root.attrs.set('data-dismissed', '');
    root.hovered = false;
    root.listeners.get('pointerleave')?.({} as never);
    expect(root.attrs.has('data-dismissed')).toBe(false);
  });

  test('pointer leaving while focus is inside keeps it dismissed', () => {
    const { root } = setup();
    root.focusWithinNow = true;
    root.attrs.set('data-dismissed', '');
    root.listeners.get('pointerleave')?.({} as never);
    expect(root.attrs.has('data-dismissed')).toBe(true);
  });
});

describe('Modal names and descriptions', () => {
  test('a titled dialog is labelled by its heading at the requested level', () => {
    const out = html(SvelteModal as never, { open: true, title: 'Delete', headingLevel: 2 });
    expect(out).toMatch(/aria-labelledby="(bb-modal-title-[^"]+)"/);
    expect(out).toMatch(/<h2 class="bb-modal__title" id="bb-modal-title-[^"]+">Delete<\/h2>/);
  });

  test('the heading level defaults to the previous h3', () => {
    expect(html(SvelteModal as never, { open: true, title: 'Delete' })).toContain('<h3 class="bb-modal__title"');
  });

  test('a label-only dialog carries aria-label and no heading', () => {
    const out = html(SvelteModal as never, { open: true, label: 'Diagram' });
    expect(out).toContain('aria-label="Diagram"');
    expect(out).not.toContain('aria-labelledby');
    expect(out).not.toContain('<h3');
  });

  test('describedBy and role reach the dialog card', () => {
    const out = html(SvelteModal as never, { open: true, title: 'Delete', role: 'alertdialog', describedBy: 'why' });
    expect(out).toContain('role="alertdialog"');
    expect(out).toContain('aria-describedby="why"');
  });

  test('the astro twin matches the svelte name, description and level', async () => {
    const astro = await experimental_AstroContainer.create();
    const out = normalise(
      await astro.renderToString(AstroModal as never, {
        props: { title: 'Delete', headingLevel: 2, role: 'alertdialog', describedBy: 'why' },
      }),
    );
    expect(out).toContain('role="alertdialog"');
    expect(out).toContain('aria-describedby="why"');
    expect(out).toContain('<h2 class="bb-modal__title" id="bb-modal-title">Delete</h2>');
  });

  test('a nameless dialog and a toolbar without a label do not type-check', async () => {
    const source = await Bun.file(new URL('../svelte/Modal.svelte', import.meta.url)).text();
    expect(source).toContain('type Named = { title: string } | { title?: undefined; label: string };');
    expect(source).toContain('{ toolbar: Snippet; toolbarLabel: string }');
  });
});

describe('ConfirmDialog', () => {
  const base = { open: true, title: 'Delete', onConfirm: () => {} };

  test('the body is the dialog description', () => {
    const out = html(ConfirmDialog as never, { ...base, body: 'This cannot be undone.' });
    const id = /aria-describedby="([^"]+)"/.exec(out)?.[1];
    expect(id).toBeTruthy();
    expect(out).toContain(`<p class="bb-modal__body" id="${id}">This cannot be undone.</p>`);
    expect(out).toContain('role="dialog"');
  });

  test('a danger confirmation is an alertdialog', () => {
    expect(html(ConfirmDialog as never, { ...base, body: 'Gone.', tone: 'danger' })).toContain('role="alertdialog"');
  });

  test('no body means nothing to describe', () => {
    expect(html(ConfirmDialog as never, base)).not.toContain('aria-describedby');
  });
});

describe('Tooltip', () => {
  test('the bubble always has an id and role and the astro twin agrees when given one', async () => {
    const out = html(SvelteTooltip as never, { text: 'Copy', children: snippet('<button>x</button>') });
    expect(out).toMatch(/<span class="bb-tooltip__bubble" id="bb-tooltip-[^"]+" role="tooltip">Copy<\/span>/);
    expect(out).not.toContain('aria-hidden');
    const astro = await experimental_AstroContainer.create();
    const fromAstro = normalise(await astro.renderToString(AstroTooltip as never, { props: { text: 'Copy', id: 'tt' } }));
    expect(fromAstro).toContain('id="tt" role="tooltip"');
  });

  test('an explicit id is kept', () => {
    expect(html(SvelteTooltip as never, { text: 'Copy', id: 'tt', children: snippet('') })).toContain('id="tt" role="tooltip"');
  });

  test('the dismissed state hides the bubble and beats the hover rule', async () => {
    const css = await Bun.file(new URL('../styles/elements/tooltip.css', import.meta.url)).text();
    expect(css.indexOf('.bb-tooltip[data-dismissed] .bb-tooltip__bubble')).toBeGreaterThan(
      css.indexOf('.bb-tooltip:is(:hover, :focus-within) .bb-tooltip__bubble'),
    );
  });
});

describe('Popover', () => {
  const base = { label: 'Install', title: 'Add', pill: snippet('<span>Install</span>') };

  test('the heading level is configurable and defaults to h2', () => {
    expect(html(Popover as never, { ...base, open: true })).toContain('<h2 class="bb-h bb-h--l6"');
    expect(html(Popover as never, { ...base, open: true, headingLevel: 3 })).toContain('<h3 class="bb-h bb-h--l6"');
  });

  test('an empty dismiss label falls back to the catalog name', () => {
    const out = html(Popover as never, { ...base, dismissLabel: '', onDismiss: () => {} });
    expect(out).toContain('class="bb-popover__x" type="button" aria-label="Dismiss"');
  });

  test('the trigger controls the sheet only while open', () => {
    const closed = html(Popover as never, base);
    expect(closed).not.toContain('aria-controls');
    const open = html(Popover as never, { ...base, open: true });
    const id = /aria-controls="([^"]+)"/.exec(open)?.[1];
    expect(open).toContain(`id="${id}" role="dialog"`);
  });
});

describe('PickerPanel', () => {
  test('the dropdown is a focusable, identified dialog', () => {
    const out = html(PickerPanel as never, { open: true, label: 'Pick', children: snippet('<button>a</button>') });
    expect(out).toMatch(/role="dialog" aria-label="Pick" id="bb-picker-[^"]+" tabindex="-1"/);
  });

  test('a caller can name the panel id it wires to its own anchor', () => {
    expect(html(PickerPanel as never, { open: true, label: 'Pick', id: 'mine', children: snippet('') })).toContain('id="mine"');
  });

  test('the sheet scrim is neither a tab stop nor announced', async () => {
    const source = await Bun.file(new URL('../svelte/PickerPanel.svelte', import.meta.url)).text();
    expect(source).toContain('class="bb-picker-panel__scrim" type="button" tabindex="-1" aria-hidden="true"');
  });
});

describe('ProfileMenu', () => {
  const avatar = snippet('<i></i>');
  const base = { name: 'Mavey', caption: 'Owner', logoutLabel: 'Log out', avatar };

  test('the topbar trigger controls a named menu while open', () => {
    const out = html(ProfileMenu as never, { ...base, open: true, links: [{ href: '/a', label: 'A' }] });
    const id = /aria-controls="([^"]+)"/.exec(out)?.[1];
    expect(out).toContain(`id="${id}" role="menu" aria-label="Account menu"`);
    expect(html(ProfileMenu as never, base)).not.toContain('aria-controls');
  });

  test('only menuitems and groups sit inside the menu', () => {
    const out = html(ProfileMenu as never, {
      ...base,
      open: true,
      links: [{ href: '/a', label: 'A' }],
      help: [{ href: '/h', label: 'Help' }],
      helpTitle: 'Support',
    });
    expect(out).toContain('<div class="bb-profile__head" aria-hidden="true">');
    expect(out).toContain('<div class="bb-profile__section" aria-hidden="true">Support</div>');
    expect(out).toContain('role="group" aria-label="Support"');
    expect(out).toContain('<form method="POST" action="/auth/logout" role="none">');
  });

  test('the help group falls back to the help label when there is no title', () => {
    const out = html(ProfileMenu as never, { ...base, open: true, help: [{ href: '/h', label: 'Help' }] });
    expect(out).toContain('role="group" aria-label="Support"');
  });

  test('the rail menus are named and controlled', () => {
    const out = html(ProfileMenu as never, {
      ...base,
      variant: 'rail',
      open: true,
      links: [{ href: '/a', label: 'A' }],
      help: [{ href: '/h', label: 'Help' }],
    });
    expect(out).toMatch(/aria-controls="bb-profile-menu-[^"]+"/);
    expect(out).toContain('role="menu" aria-label="Account menu"');
    expect(html(ProfileMenu as never, { ...base, variant: 'rail', helpOpen: true, help: [{ href: '/h', label: 'Help' }] })).toMatch(
      /id="bb-profile-help-[^"]+" role="menu" aria-label="Support"/,
    );
  });

  test('the scrim carries no keyboard handler', async () => {
    const source = await Bun.file(new URL('../svelte/ProfileMenu.svelte', import.meta.url)).text();
    expect(source).not.toContain('closeOnEnter');
  });
});

describe('CopySurface', () => {
  test('the status region is a sibling of the button, never inside it', async () => {
    const svelte = html(SvelteCopySurface as never, { text: 'BAGEL' });
    expect(svelte).toContain('</button><span class="bb-copy__status" role="status"></span>');
    const astro = await experimental_AstroContainer.create();
    const fromAstro = normalise(await astro.renderToString(AstroCopySurface as never, { props: { text: 'BAGEL' } }));
    expect(fromAstro).toContain('</button><span class="bb-copy__status" role="status"></span>');
    expect(fromAstro).toBe(svelte);
  });

  test('caller attributes cannot replace the copy payload or the type', () => {
    const out = html(SvelteCopySurface as never, { text: 'BAGEL', 'data-copy': 'other', type: 'submit' });
    expect(out).toContain('data-copy="BAGEL"');
    expect(out).toContain('type="button"');
    expect(out).not.toContain('other');
    expect(out).not.toContain('submit');
  });

  test('a caller click handler runs beside the copy handler', async () => {
    const source = await Bun.file(new URL('../svelte/CopySurface.svelte', import.meta.url)).text();
    expect(source).toContain('userClick?.(event);');
    expect(source).toContain('void copy();');
  });
});
