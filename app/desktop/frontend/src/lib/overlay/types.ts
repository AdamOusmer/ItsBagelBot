// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export type LayerKind = 'text' | 'image' | 'alert' | 'chat';

export type LayerTone = 'free' | 'paid' | 'vip';

interface LayerBase {
  id: string;
  kind: LayerKind;
  name: string;
  /** Stage position and size, as a percentage of the 16:9 canvas. Resolution-
      independent on purpose: switching canvas resolution never moves or
      rescales a layer, since the percentage means the same thing at any size. */
  x: number;
  y: number;
  w: number;
  h: number;
  opacity: number;
}

export interface TextLayer extends LayerBase {
  kind: 'text';
  content: string;
  /** Percent of canvas height, same reasoning as x/y/w/h. */
  fontSize: number;
  color: string;
  align: 'start' | 'center' | 'end';
}

export interface ImageLayer extends LayerBase {
  kind: 'image';
  caption: string;
  fit: 'cover' | 'contain';
}

export interface AlertLayer extends LayerBase {
  kind: 'alert';
  title: string;
  subtitle: string;
  tone: LayerTone;
}

export interface ChatMessage {
  user: string;
  tone: LayerTone | 'none';
  message: string;
}

export interface ChatLayer extends LayerBase {
  kind: 'chat';
  rows: number;
  messages: ChatMessage[];
}

export type Layer = TextLayer | ImageLayer | AlertLayer | ChatLayer;
