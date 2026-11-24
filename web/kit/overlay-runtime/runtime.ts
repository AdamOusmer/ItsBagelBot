// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { Assets } from './assets';
import { installAlert } from './alert';
import { installCredits, installImage, installSlideshow, installText, installTimer, installVideo } from './basic-widgets';
import { installChat } from './chat-widgets';
import { installCustom } from './custom-widget';
import { installEventList, installGoal, installLabel } from './data-widgets';
import { installEmoteWall } from './emote-wall';
import type { FeedEvent, FeedSnapshot } from './feed';
import { css } from './renderer-css';
import { parseWidget } from './settings';
import { node, type Install, type RenderContext, type View } from './view';
import type { OverlayDocument, OverlayWidget } from '../lib/overlay-document';
import type { Widget } from './widget-types';

const empty: FeedSnapshot = { stats: {}, subathon: {}, control: { muted: false, paused: false, skip: 0 }, chat: [], events: [] };
type Layer = { host: HTMLElement; content: HTMLElement; signature?: string; view?: View };
type Canvas = Pick<OverlayDocument, 'width' | 'height' | 'widgets' | 'assets'>;
export type RuntimeOptions = Pick<RenderContext, 'sandboxOrigin' | 'preview'>;

const installers: { [K in Widget['kind']]: Install<K> } = {
  alertbox: installAlert,
  eventlist: installEventList,
  goal: installGoal,
  label: installLabel,
  chatbox: installChat,
  emotewall: installEmoteWall,
  timer: installTimer,
  credits: installCredits,
  text: installText,
  image: installImage,
  video: installVideo,
  slideshow: installSlideshow,
  custom: installCustom
};

function install(widget: Widget, content: HTMLElement, context: RenderContext): View | undefined {
  const mount = installers[widget.kind] as Install<Widget['kind']>;
  return mount(content, widget.settings, { ...context, id: widget.id }) ?? undefined;
}

function position(host: HTMLElement, widget: Widget): void {
  Object.assign(host.style, {
    left: `${widget.x}px`,
    top: `${widget.y}px`,
    width: `${widget.w}px`,
    height: `${widget.h}px`,
    opacity: String(widget.opacity),
    transform: `rotate(${widget.rotation}deg)`
  });
  host.style.setProperty('--obs-widget-height', `${widget.h}px`);
  host.hidden = widget.hidden;
}

let sheet: CSSStyleSheet | undefined;

function loadStyles(): void {
  if (!sheet) {
    sheet = new CSSStyleSheet();
    sheet.replaceSync(css);
  }
  if (!document.adoptedStyleSheets.includes(sheet)) document.adoptedStyleSheets = [...document.adoptedStyleSheets, sheet];
}

function parse(input: OverlayWidget): Widget[] {
  try {
    return [parseWidget(input)];
  } catch (error) {
    console.error('Widget skipped', input.id, error);
    return [];
  }
}

export class OverlayRuntime {
  #canvas = node('div', 'obs-canvas');
  #layers = new Map<string, Layer>();
  #snapshot = empty;
  #assets = new Assets();
  #timer: ReturnType<typeof setInterval>;

  constructor(
    host: HTMLElement,
    private options: RuntimeOptions = {}
  ) {
    loadStyles();
    host.append(this.#canvas);
    this.#timer = setInterval(() => this.#draw('tick'), 50);
  }

  setDocument(doc: Canvas): void {
    this.#assets = new Assets(doc.assets);
    this.#canvas.style.width = `${doc.width}px`;
    this.#canvas.style.height = `${doc.height}px`;
    const widgets = doc.widgets.flatMap(parse);
    const ids = new Set(widgets.map((widget) => widget.id));
    for (const [id, layer] of this.#layers) {
      if (!ids.has(id)) this.#remove(id, layer);
    }
    widgets.forEach((widget, order) => this.#layout(widget, order));
    this.#draw();
  }

  setSnapshot(snapshot: FeedSnapshot): void {
    this.#snapshot = snapshot;
    this.#draw();
  }

  event(event: FeedEvent): void {
    const frame = { snapshot: this.#snapshot, now: Date.now() };
    for (const layer of this.#layers.values()) {
      if (!layer.host.hidden) layer.view?.event?.(event, frame);
    }
    this.#draw();
  }

  scale(value: number): void {
    this.#canvas.style.transform = `scale(${value})`;
  }

  dispose(): void {
    clearInterval(this.#timer);
    for (const [id, layer] of this.#layers) this.#remove(id, layer);
    this.#canvas.remove();
  }

  #layout(widget: Widget, order: number): void {
    const layer = this.#layers.get(widget.id) ?? this.#newLayer(widget.id);
    layer.host.style.zIndex = String(order);
    position(layer.host, widget);
    const signature = JSON.stringify([widget.kind, widget.settings, widget.hidden]);
    if (layer.signature === signature) return;
    layer.signature = signature;
    layer.view?.dispose?.();
    layer.content.replaceChildren();
    layer.view = widget.hidden ? undefined : install(widget, layer.content, { ...this.options, assets: this.#assets });
  }

  #newLayer(id: string): Layer {
    const host = node('div', 'obs-widget');
    const content = node('div', 'obs-widget-content');
    host.append(content);
    this.#canvas.append(host);
    const layer = { host, content };
    this.#layers.set(id, layer);
    return layer;
  }

  #remove(id: string, layer: Layer): void {
    layer.view?.dispose?.();
    layer.host.remove();
    this.#layers.delete(id);
  }

  #draw(method: 'update' | 'tick' = 'update'): void {
    const frame = { snapshot: this.#snapshot, now: Date.now() };
    for (const layer of this.#layers.values()) layer.view?.[method]?.(frame);
  }
}
