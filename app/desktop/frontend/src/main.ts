// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { defineTextFit } from '@bagel/ui/lib/text-fit';
import { mount } from 'svelte';
import './app.css';
import App from './App.svelte';

defineTextFit();

const target = document.getElementById('app');
if (!target) throw new Error('missing #app');

mount(App, { target });
