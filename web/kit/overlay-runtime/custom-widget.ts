// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { parseBundle } from './bundle';
import { FrameChannel } from './frame-channel';
import { changed, node, type Install } from './view';

async function loadBundle(url: string, signal: AbortSignal) {
  const response = await fetch(url, { signal, referrerPolicy: 'no-referrer', credentials: 'omit' });
  if (!response.ok) throw new Error('Widget bundle could not be loaded.');
  return parseBundle(await response.text());
}

export const installCustom: Install<'custom'> = (host, settings, { assets, id, sandboxOrigin }) => {
  const bundle = assets.resolve(settings.bundle);
  if (!bundle || !sandboxOrigin) return;
  const frame = node('iframe', 'obs-custom');
  frame.setAttribute('sandbox', 'allow-scripts');
  frame.title = 'Custom widget';
  frame.referrerPolicy = 'no-referrer';
  frame.src = `${sandboxOrigin}/sandbox/${encodeURIComponent(id)}`;
  const channel = new FrameChannel(frame, settings.values);
  const loading = new AbortController();
  loadBundle(bundle.url, loading.signal)
    .then((loaded) => channel.attach(loaded))
    .catch((error) => console.error(error));
  host.append(frame);
  return {
    event: (event) => channel.event(event),
    update: changed(
      ({ snapshot }) => snapshot.stats,
      (stats) => channel.session(stats)
    ),
    dispose: () => {
      loading.abort();
      channel.dispose();
    }
  };
};
