// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export type Locale = string;

export type MessageTree = { [key: string]: string | string[] | MessageTree };
