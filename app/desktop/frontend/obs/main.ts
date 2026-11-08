// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { mount } from 'svelte';
import './style.css';
import Renderer from './Renderer.svelte';
mount(Renderer, { target: document.getElementById('app')! });
