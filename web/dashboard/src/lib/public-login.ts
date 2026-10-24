// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export const LOGIN_RE = /^[a-z0-9_]{1,25}$/;

const LOGIN_SHAPE_RE = new RegExp(LOGIN_RE.source, 'i');

export const normalizeLogin = (raw: string): string => raw.replace(/^@+/, '').toLowerCase();

export const isLoginShape = (name: string): boolean => LOGIN_SHAPE_RE.test(name);
