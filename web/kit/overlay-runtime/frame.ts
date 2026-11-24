// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { compatEvent, compatLoad, compatSession, type CompatEvent } from './compat';
import { isParentMessage, type FrameBundle, type ParentMessage } from './frame-protocol';

let loaded = false;

function emit(name: string, detail: unknown): void {
  dispatchEvent(new CustomEvent(name, { detail }));
}

function emitCompat(event?: CompatEvent): void {
  if (event) emit(event.name, event.detail);
}

function run(source: string): void {
  const script = document.createElement('script');
  script.textContent = source;
  document.body.append(script);
}

function mount(bundle: FrameBundle): void {
  const style = document.createElement('style');
  style.textContent = bundle.css;
  document.head.append(style);
  document.body.innerHTML = bundle.html;
  run(bundle.js);
}

function handle(message: ParentMessage): void {
  if (message.type === 'load') {
    if (loaded) return;
    loaded = true;
    mount(message.bundle);
    emit('bagel:load', { fields: message.fields, session: message.session });
    emitCompat(compatLoad(message.fields, message.session));
  } else if (message.type === 'event') {
    emit('bagel:event', { event: message.event });
    emitCompat(compatEvent(message.event));
  } else {
    emit('bagel:session', { session: message.session });
    emitCompat(compatSession(message.session));
  }
}

addEventListener('message', ({ source, data }) => {
  if (source === parent && isParentMessage(data)) handle(data);
});
parent.postMessage({ bagel: 1, type: 'ready' }, '*');
