// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// The structure of the "counters" guide: section order and ids, block kinds,
// which mock screen or widget each block shows, and the shape of the data
// those widgets take. Every k('...') names one line of copy in
// src/content/guides/counters.<lang>.ts.
import { k, type GuideSkeleton } from '../skeleton';

const skeleton: GuideSkeleton = {
    slug: 'counters',
    meta: {
        title: k('meta.title'),
        description: k('meta.description'),
        eyebrow: k('meta.eyebrow'),
        heading: k('meta.heading'),
        lead: k('meta.lead'),
        minutes: k('meta.minutes'),
        card: {
            title: k('meta.card.title'),
            description: k('meta.card.description'),
            meta: k('meta.card.meta'),
            chips: [k('meta.card.chips.0'), k('meta.card.chips.1'), k('meta.card.chips.2'), k('meta.card.chips.3')],
        },
    },
    sections: [
        {
            id: 'basics',
            heading: k('basics.heading'),
            note: k('basics.note'),
            blocks: [
                { kind: 'prose', html: k('basics.b0.html') },
                {
                    kind: 'chat',
                    title: k('basics.b1.title'),
                    caption: k('basics.b1.caption'),
                    lines: [
                        {
                            who: 'viewer',
                            name: k('basics.b1.lines.0.name'),
                            text: k('basics.b1.lines.0.text'),
                        },
                        { who: 'bot', text: k('basics.b1.lines.1.text') },
                        {
                            who: 'viewer',
                            name: k('basics.b1.lines.2.name'),
                            text: k('basics.b1.lines.2.text'),
                        },
                        { who: 'bot', text: k('basics.b1.lines.3.text') },
                    ],
                },
                { kind: 'callout', tone: 'tip', html: k('basics.b2.html') },
            ],
        },
        {
            id: 'scopes',
            heading: k('scopes.heading'),
            note: k('scopes.note'),
            blocks: [
                { kind: 'prose', html: k('scopes.b0.html') },
                {
                    kind: 'table',
                    head: [k('scopes.b1.head.0'), k('scopes.b1.head.1'), k('scopes.b1.head.2')],
                    rows: [
                        [k('scopes.b1.rows.0.0'), k('scopes.b1.rows.0.1'), k('scopes.b1.rows.0.2')],
                        [k('scopes.b1.rows.1.0'), k('scopes.b1.rows.1.1'), k('scopes.b1.rows.1.2')],
                        [k('scopes.b1.rows.2.0'), k('scopes.b1.rows.2.1'), k('scopes.b1.rows.2.2')],
                        [k('scopes.b1.rows.3.0'), k('scopes.b1.rows.3.1'), k('scopes.b1.rows.3.2')],
                    ],
                },
                { kind: 'prose', html: k('scopes.b2.html') },
                {
                    kind: 'chat',
                    title: k('scopes.b3.title'),
                    caption: k('scopes.b3.caption'),
                    lines: [
                        {
                            who: 'viewer',
                            name: k('scopes.b3.lines.0.name'),
                            text: k('scopes.b3.lines.0.text'),
                        },
                        { who: 'bot', text: k('scopes.b3.lines.1.text') },
                        {
                            who: 'viewer',
                            name: k('scopes.b3.lines.2.name'),
                            text: k('scopes.b3.lines.2.text'),
                        },
                        { who: 'bot', text: k('scopes.b3.lines.3.text') },
                    ],
                },
                { kind: 'prose', html: k('scopes.b4.html') },
                {
                    kind: 'chat',
                    title: k('scopes.b5.title'),
                    caption: k('scopes.b5.caption'),
                    lines: [
                        {
                            who: 'viewer',
                            name: k('scopes.b5.lines.0.name'),
                            text: k('scopes.b5.lines.0.text'),
                        },
                        { who: 'bot', text: k('scopes.b5.lines.1.text') },
                        {
                            who: 'viewer',
                            name: k('scopes.b5.lines.2.name'),
                            text: k('scopes.b5.lines.2.text'),
                        },
                        { who: 'bot', text: k('scopes.b5.lines.3.text') },
                    ],
                },
                { kind: 'prose', html: k('scopes.b6.html') },
                {
                    kind: 'chat',
                    title: k('scopes.b7.title'),
                    caption: k('scopes.b7.caption'),
                    lines: [
                        { who: 'system', text: k('scopes.b7.lines.0.text') },
                        { who: 'bot', text: k('scopes.b7.lines.1.text') },
                        { who: 'system', text: k('scopes.b7.lines.2.text') },
                        { who: 'bot', text: k('scopes.b7.lines.3.text') },
                    ],
                },
                { kind: 'prose', html: k('scopes.b8.html') },
                {
                    kind: 'chat',
                    title: k('scopes.b9.title'),
                    caption: k('scopes.b9.caption'),
                    lines: [
                        {
                            who: 'viewer',
                            name: k('scopes.b9.lines.0.name'),
                            text: k('scopes.b9.lines.0.text'),
                        },
                        { who: 'bot', text: k('scopes.b9.lines.1.text') },
                        {
                            who: 'viewer',
                            name: k('scopes.b9.lines.2.name'),
                            text: k('scopes.b9.lines.2.text'),
                        },
                        { who: 'bot', text: k('scopes.b9.lines.3.text') },
                    ],
                },
                { kind: 'callout', tone: 'warn', html: k('scopes.b10.html') },
            ],
        },
        {
            id: 'chat',
            heading: k('chat.heading'),
            note: k('chat.note'),
            blocks: [
                { kind: 'prose', html: k('chat.b0.html') },
                {
                    kind: 'table',
                    head: [k('chat.b1.head.0'), k('chat.b1.head.1')],
                    rows: [
                        [k('chat.b1.rows.0.0'), k('chat.b1.rows.0.1')],
                        [k('chat.b1.rows.1.0'), k('chat.b1.rows.1.1')],
                        [k('chat.b1.rows.2.0'), k('chat.b1.rows.2.1')],
                        [k('chat.b1.rows.3.0'), k('chat.b1.rows.3.1')],
                        [k('chat.b1.rows.4.0'), k('chat.b1.rows.4.1')],
                        [k('chat.b1.rows.5.0'), k('chat.b1.rows.5.1')],
                        [k('chat.b1.rows.6.0'), k('chat.b1.rows.6.1')],
                    ],
                },
                {
                    kind: 'chat',
                    title: k('chat.b2.title'),
                    caption: k('chat.b2.caption'),
                    lines: [
                        { who: 'mod', name: k('chat.b2.lines.0.name'), text: k('chat.b2.lines.0.text') },
                        { who: 'bot', text: k('chat.b2.lines.1.text') },
                        { who: 'mod', name: k('chat.b2.lines.2.name'), text: k('chat.b2.lines.2.text') },
                        { who: 'bot', text: k('chat.b2.lines.3.text') },
                    ],
                },
                { kind: 'callout', tone: 'tip', html: k('chat.b3.html') },
                {
                    kind: 'widget',
                    name: 'CounterPlay',
                    labels: {
                        heading: k('chat.b4.labels.heading'),
                        windowTitle: k('chat.b4.labels.windowTitle'),
                        modName: k('chat.b4.labels.modName'),
                        addOne: k('chat.b4.labels.addOne'),
                        subOne: k('chat.b4.labels.subOne'),
                        setTen: k('chat.b4.labels.setTen'),
                        viewerName: k('chat.b4.labels.viewerName'),
                        viewerCommand: k('chat.b4.labels.viewerCommand'),
                        viewerReplyTemplate: k('chat.b4.labels.viewerReplyTemplate'),
                        modReplyTemplate: k('chat.b4.labels.modReplyTemplate'),
                    },
                    props: { counterName: k('chat.b4.props.counterName'), start: 12 },
                },
            ],
        },
        {
            id: 'dashboard',
            heading: k('dashboard.heading'),
            note: k('dashboard.note'),
            blocks: [
                { kind: 'prose', html: k('dashboard.b0.html') },
                {
                    kind: 'dash',
                    screen: 'NewCounter',
                    path: '/counters',
                    caption: k('dashboard.b1.caption'),
                    notes: [
                        { n: 1, text: k('dashboard.b1.notes.0.text') },
                        { n: 2, text: k('dashboard.b1.notes.1.text') },
                        { n: 3, text: k('dashboard.b1.notes.2.text') },
                    ],
                    labels: {
                        panelHead: k('dashboard.b1.labels.panelHead'),
                        fieldName: k('dashboard.b1.labels.fieldName'),
                        nameValue: k('dashboard.b1.labels.nameValue'),
                        fieldCounts: k('dashboard.b1.labels.fieldCounts'),
                        countsValue: k('dashboard.b1.labels.countsValue'),
                        chip1: k('dashboard.b1.labels.chip1'),
                        chip2: k('dashboard.b1.labels.chip2'),
                        chip3: k('dashboard.b1.labels.chip3'),
                        chip4: k('dashboard.b1.labels.chip4'),
                        hint: k('dashboard.b1.labels.hint'),
                        cancel: k('dashboard.b1.labels.cancel'),
                        create: k('dashboard.b1.labels.create'),
                    },
                },
                { kind: 'prose', html: k('dashboard.b2.html') },
                {
                    kind: 'dash',
                    screen: 'RewardCounter',
                    path: '/channelpoints',
                    caption: k('dashboard.b3.caption'),
                    notes: [
                        { n: 1, text: k('dashboard.b3.notes.0.text') },
                        { n: 2, text: k('dashboard.b3.notes.1.text') },
                    ],
                    labels: {
                        check: k('dashboard.b3.labels.check'),
                        fieldName: k('dashboard.b3.labels.fieldName'),
                        nameValue: k('dashboard.b3.labels.nameValue'),
                        fieldScope: k('dashboard.b3.labels.fieldScope'),
                        scopeValue: k('dashboard.b3.labels.scopeValue'),
                        chip1: k('dashboard.b3.labels.chip1'),
                        chip2: k('dashboard.b3.labels.chip2'),
                        chip3: k('dashboard.b3.labels.chip3'),
                        chip4: k('dashboard.b3.labels.chip4'),
                        cancel: k('dashboard.b3.labels.cancel'),
                        save: k('dashboard.b3.labels.save'),
                    },
                },
                { kind: 'callout', tone: 'tip', html: k('dashboard.b4.html') },
            ],
        },
    ],
};

export default skeleton;
