// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export function connectAnalyser(element: HTMLAudioElement): AnalyserNode {
  const context = new AudioContext();
  const node = context.createAnalyser();
  node.fftSize = 2048;
  node.smoothingTimeConstant = 0.75;
  context.createMediaElementSource(element).connect(node);
  node.connect(context.destination);
  return node;
}
