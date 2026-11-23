// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export const LOGIN_RE = /^[a-z0-9_]{1,25}$/;

export const USERNAME_RE = new RegExp(LOGIN_RE.source, 'i');
