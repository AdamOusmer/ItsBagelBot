// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export const css = `
.obs-canvas {
  position: absolute;
  inset: 0 auto auto 0;
  isolation: isolate;
  overflow: hidden;
  transform-origin: top left;
  font: 400 16px system-ui, sans-serif;
  line-height: 1.2;
  color: white;
}
.obs-canvas *, .obs-canvas *::before, .obs-canvas *::after { box-sizing: border-box; }
.obs-widget { position: absolute; }
.obs-widget-content { position: relative; width: 100%; height: 100%; overflow: hidden; }
.obs-media, .obs-custom { display: block; width: 100%; height: 100%; border: 0; }
.obs-text { display: grid; align-content: center; white-space: pre-wrap; overflow-wrap: anywhere; }
.obs-timer { display: grid; place-items: center; font-variant-numeric: tabular-nums; }
.obs-slide { position: absolute; inset: 0; opacity: 0; transition-property: opacity; }
.obs-slide-active { opacity: 1; }
.obs-alert {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 0.25em;
  width: 100%;
  height: 100%;
  position: relative;
  text-align: center;
}
.obs-alert .obs-media { flex: 1 1 50%; min-width: 0; min-height: 0; }
.obs-alert-above { flex-direction: column-reverse; }
.obs-alert-left { flex-direction: row-reverse; }
.obs-alert-right { flex-direction: row; }
.obs-alert-overlay .obs-media { position: absolute; inset: 0; }
.obs-alert-text { z-index: 1; flex: 0 1 auto; overflow-wrap: anywhere; }
.obs-alert-message { font-size: 0.55em; white-space: pre-wrap; }
.obs-event-line, .obs-chat-line { margin-block-end: 0.18em; overflow-wrap: anywhere; white-space: pre-wrap; }
.obs-chat-bottom { display: flex; flex-direction: column; justify-content: flex-end; }
.obs-chat-emote { display: inline-block; width: auto; height: 1.25em; vertical-align: middle; }
.obs-chat-badge { display: inline-block; margin-inline-end: 0.25em; padding: 0.1em 0.3em; border-radius: 0.25em; background: #243433; font-size: 0.67em; color: white; }
.obs-chat-badge-image { height: 1em; margin-inline-end: 0.25em; vertical-align: -0.1em; }
.obs-chat-bubbles { width: fit-content; padding: 0.24em 0.5em; border-radius: 0.65em; background: #111c1bef; }
.obs-chat-boxed { padding: 0.24em 0.5em; background: #111c1bef; }
.obs-flying-emote { position: absolute; object-fit: contain; animation-fill-mode: both; }
.obs-emote-wall { position: absolute; inset: 0; }
.obs-emote-combo { position: absolute; top: 10%; width: 100%; text-align: center; }
.obs-emote-fly { bottom: 0; animation-name: obs-emotes-fly; }
.obs-emote-rain { top: 0; animation-name: obs-emotes-rain; }
.obs-emote-bounce { bottom: 0; animation-name: obs-emotes-bounce; }
.obs-label { white-space: nowrap; }
.obs-marquee { width: max-content; animation: obs-marquee 10s linear infinite; }
.obs-credit-roll { text-align: center; }
.obs-credit-group { margin-block-end: 1em; }
.obs-credit-group strong { display: block; margin-block-end: 0.25em; }
.obs-goal { display: flex; flex-direction: column; width: 100%; height: 100%; align-items: center; justify-content: center; gap: 0.2em; }
.obs-goal-track { position: relative; overflow: hidden; width: 90%; min-height: 0.2em; background: var(--track); border-radius: 100vmax; }
.obs-goal-fill { width: var(--progress); height: 100%; position: absolute; inset: 0 auto 0 0; background: var(--fill); }
.obs-goal-vertical .obs-goal-track { width: 0.45em; flex: 1; }
.obs-goal-vertical .obs-goal-fill { width: 100%; height: var(--progress); inset: auto 0 0; }
.obs-goal-ring .obs-goal-track { width: auto; min-height: 0; flex: 1; aspect-ratio: 1; background: conic-gradient(var(--fill) var(--progress), var(--track) 0); mask: radial-gradient(transparent 54%, black 55%); }
.obs-goal-ring .obs-goal-fill, .obs-goal-text .obs-goal-track { display: none; }

@keyframes obs-marquee { from { transform: translateX(0); } to { transform: translateX(-100%); } }
@keyframes obs-fade { from { opacity: 0; } to { opacity: 1; } }
@keyframes obs-slide-up { from { transform: translateY(30%); opacity: 0; } to { transform: translateY(0); opacity: 1; } }
@keyframes obs-slide-down { from { transform: translateY(-30%); opacity: 0; } to { transform: translateY(0); opacity: 1; } }
@keyframes obs-slide-left { from { transform: translateX(30%); opacity: 0; } to { transform: translateX(0); opacity: 1; } }
@keyframes obs-slide-right { from { transform: translateX(-30%); opacity: 0; } to { transform: translateX(0); opacity: 1; } }
@keyframes obs-zoom { from { transform: scale(0.7); opacity: 0; } to { transform: scale(1); opacity: 1; } }
@keyframes obs-pop { 0% { transform: scale(0.65); opacity: 0; } 65% { transform: scale(1.09); } 100% { transform: scale(1); opacity: 1; } }
@keyframes obs-bounce { 0% { transform: translateY(-25%); opacity: 0; } 55% { transform: translateY(7%); } 78% { transform: translateY(-4%); } 100% { transform: translateY(0); opacity: 1; } }
@keyframes obs-flip { from { transform: perspective(700px) rotateY(90deg); opacity: 0; } to { transform: perspective(700px) rotateY(0); opacity: 1; } }
@keyframes obs-emotes-fly { from { transform: translateY(0) rotate(-20deg); } to { transform: translate(120%, calc(-1 * var(--obs-widget-height))) rotate(20deg); opacity: 0; } }
@keyframes obs-emotes-rain { from { transform: translateY(-100%) rotate(-20deg); } to { transform: translate(30%, var(--obs-widget-height)) rotate(20deg); opacity: 0; } }
@keyframes obs-emotes-bounce { 0% { transform: translateY(0); } 28% { transform: translate(50%, calc(-0.65 * var(--obs-widget-height))); } 55% { transform: translate(90%, 0); } 78% { transform: translate(135%, calc(-0.3 * var(--obs-widget-height))); } 100% { transform: translate(170%, 0); opacity: 0; } }
`;
