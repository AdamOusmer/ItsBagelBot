// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// The structure of the "commands" guide: section order and ids, block kinds,
// which mock screen or widget each block shows, and the shape of the data
// those widgets take. Every k('...') names one line of copy in
// src/content/guides/commands.<lang>.ts.
import { k, type GuideSkeleton } from '../skeleton';

const skeleton: GuideSkeleton = {
    slug: 'commands',
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
            id: 'anatomy',
            heading: k('anatomy.heading'),
            note: k('anatomy.note'),
            blocks: [
                { kind: 'prose', html: k('anatomy.b0.html') },
                {
                    kind: 'dash',
                    screen: 'CommandEditor',
                    path: '/commands',
                    caption: k('anatomy.b1.caption'),
                    notes: [
                        { n: 1, text: k('anatomy.b1.notes.0.text') },
                        { n: 2, text: k('anatomy.b1.notes.1.text') },
                        { n: 3, text: k('anatomy.b1.notes.2.text') },
                        { n: 4, text: k('anatomy.b1.notes.3.text') },
                        { n: 5, text: k('anatomy.b1.notes.4.text') },
                        { n: 6, text: k('anatomy.b1.notes.5.text') },
                        { n: 7, text: k('anatomy.b1.notes.6.text') },
                    ],
                    labels: {
                        panelHead: k('anatomy.b1.labels.panelHead'),
                        close: k('anatomy.b1.labels.close'),
                        fieldName: k('anatomy.b1.labels.fieldName'),
                        nameValue: k('anatomy.b1.labels.nameValue'),
                        fieldAlts: k('anatomy.b1.labels.fieldAlts'),
                        optional: k('anatomy.b1.labels.optional'),
                        altChipLabel: k('anatomy.b1.labels.altChipLabel'),
                        remove: k('anatomy.b1.labels.remove'),
                        fieldResponse: k('anatomy.b1.labels.fieldResponse'),
                        responseHtml: k('anatomy.b1.labels.responseHtml'),
                        chip4: k('anatomy.b1.labels.chip4'),
                        chip4Tooltip: k('anatomy.b1.labels.chip4Tooltip'),
                        chatTag: k('anatomy.b1.labels.chatTag'),
                        viewerText: k('anatomy.b1.labels.viewerText'),
                        botHtml: k('anatomy.b1.labels.botHtml'),
                        fieldAccess: k('anatomy.b1.labels.fieldAccess'),
                        accessValue: k('anatomy.b1.labels.accessValue'),
                        fieldCooldown: k('anatomy.b1.labels.fieldCooldown'),
                        fieldRestrict: k('anatomy.b1.labels.fieldRestrict'),
                        restrictPlaceholder: k('anatomy.b1.labels.restrictPlaceholder'),
                        checkActive: k('anatomy.b1.labels.checkActive'),
                        checkLive: k('anatomy.b1.labels.checkLive'),
                        cancel: k('anatomy.b1.labels.cancel'),
                        save: k('anatomy.b1.labels.save'),
                    },
                },
                { kind: 'callout', tone: 'tip', html: k('anatomy.b2.html') },
            ],
        },
        {
            id: 'create',
            heading: k('create.heading'),
            note: k('create.note'),
            blocks: [
                { kind: 'prose', html: k('create.b0.html') },
                {
                    kind: 'chat',
                    title: k('create.b1.title'),
                    caption: k('create.b1.caption'),
                    lines: [
                        { who: 'mod', name: k('create.b1.lines.0.name'), text: k('create.b1.lines.0.text') },
                        { who: 'bot', text: k('create.b1.lines.1.text') },
                        {
                            who: 'viewer',
                            name: k('create.b1.lines.2.name'),
                            text: k('create.b1.lines.2.text'),
                        },
                        { who: 'bot', text: k('create.b1.lines.3.text') },
                    ],
                },
                { kind: 'prose', html: k('create.b2.html') },
                { kind: 'callout', tone: 'tip', html: k('create.b3.html') },
            ],
        },
        {
            id: 'variables',
            heading: k('variables.heading'),
            note: k('variables.note'),
            blocks: [
                { kind: 'prose', html: k('variables.b0.html') },
                {
                    kind: 'table',
                    head: [k('variables.b1.head.0'), k('variables.b1.head.1'), k('variables.b1.head.2')],
                    rows: [
                        [k('variables.b1.rows.0.0'), k('variables.b1.rows.0.1'), k('variables.b1.rows.0.2')],
                        [k('variables.b1.rows.1.0'), k('variables.b1.rows.1.1'), k('variables.b1.rows.1.2')],
                        [k('variables.b1.rows.2.0'), k('variables.b1.rows.2.1'), k('variables.b1.rows.2.2')],
                        [k('variables.b1.rows.3.0'), k('variables.b1.rows.3.1'), k('variables.b1.rows.3.2')],
                        [k('variables.b1.rows.4.0'), k('variables.b1.rows.4.1'), k('variables.b1.rows.4.2')],
                    ],
                },
                {
                    kind: 'chat',
                    title: k('variables.b2.title'),
                    caption: k('variables.b2.caption'),
                    lines: [
                        {
                            who: 'viewer',
                            name: k('variables.b2.lines.0.name'),
                            text: k('variables.b2.lines.0.text'),
                        },
                        { who: 'bot', text: k('variables.b2.lines.1.text') },
                        {
                            who: 'viewer',
                            name: k('variables.b2.lines.2.name'),
                            text: k('variables.b2.lines.2.text'),
                        },
                        { who: 'bot', text: k('variables.b2.lines.3.text') },
                    ],
                },
                { kind: 'callout', tone: 'warn', html: k('variables.b3.html') },
            ],
        },
        {
            id: 'dynamic',
            heading: k('dynamic.heading'),
            note: k('dynamic.note'),
            blocks: [
                { kind: 'prose', html: k('dynamic.b0.html') },
                {
                    kind: 'chat',
                    title: k('dynamic.b1.title'),
                    caption: k('dynamic.b1.caption'),
                    lines: [
                        {
                            who: 'viewer',
                            name: k('dynamic.b1.lines.0.name'),
                            text: k('dynamic.b1.lines.0.text'),
                        },
                        { who: 'bot', text: k('dynamic.b1.lines.1.text') },
                        {
                            who: 'viewer',
                            name: k('dynamic.b1.lines.2.name'),
                            text: k('dynamic.b1.lines.2.text'),
                        },
                        { who: 'bot', text: k('dynamic.b1.lines.3.text') },
                    ],
                },
                { kind: 'callout', tone: 'tip', html: k('dynamic.b2.html') },
                {
                    kind: 'widget',
                    name: 'Rehearsal',
                    labels: {
                        heading: k('dynamic.b3.labels.heading'),
                        responseLabel: k('dynamic.b3.labels.responseLabel'),
                        whoLabel: k('dynamic.b3.labels.whoLabel'),
                        argsLabel: k('dynamic.b3.labels.argsLabel'),
                        outputLabel: k('dynamic.b3.labels.outputLabel'),
                        pillLabel: k('dynamic.b3.labels.pillLabel'),
                        builderNote: k('dynamic.b3.labels.builderNote'),
                        builderLinkText: k('dynamic.b3.labels.builderLinkText'),
                        builderHref: k('dynamic.b3.labels.builderHref'),
                        responseDefault: k('dynamic.b3.labels.responseDefault'),
                        outputDefault: k('dynamic.b3.labels.outputDefault'),
                    },
                },
            ],
        },
        {
            id: 'multiline',
            heading: k('multiline.heading'),
            note: k('multiline.note'),
            blocks: [
                { kind: 'prose', html: k('multiline.b0.html') },
                {
                    kind: 'table',
                    head: [k('multiline.b1.head.0'), k('multiline.b1.head.1')],
                    rows: [
                        [k('multiline.b1.rows.0.0'), k('multiline.b1.rows.0.1')],
                        [k('multiline.b1.rows.1.0'), k('multiline.b1.rows.1.1')],
                        [k('multiline.b1.rows.2.0'), k('multiline.b1.rows.2.1')],
                        [k('multiline.b1.rows.3.0'), k('multiline.b1.rows.3.1')],
                    ],
                },
                {
                    kind: 'chat',
                    title: k('multiline.b2.title'),
                    caption: k('multiline.b2.caption'),
                    lines: [
                        {
                            who: 'viewer',
                            name: k('multiline.b2.lines.0.name'),
                            text: k('multiline.b2.lines.0.text'),
                        },
                        { who: 'system', text: k('multiline.b2.lines.1.text') },
                        { who: 'bot', text: k('multiline.b2.lines.2.text') },
                    ],
                },
                { kind: 'callout', tone: 'tip', html: k('multiline.b3.html') },
            ],
        },
        {
            id: 'rules',
            heading: k('rules.heading'),
            note: k('rules.note'),
            blocks: [
                { kind: 'prose', html: k('rules.b0.html') },
                {
                    kind: 'table',
                    head: [k('rules.b1.head.0'), k('rules.b1.head.1')],
                    rows: [
                        [k('rules.b1.rows.0.0'), k('rules.b1.rows.0.1')],
                        [k('rules.b1.rows.1.0'), k('rules.b1.rows.1.1')],
                        [k('rules.b1.rows.2.0'), k('rules.b1.rows.2.1')],
                        [k('rules.b1.rows.3.0'), k('rules.b1.rows.3.1')],
                        [k('rules.b1.rows.4.0'), k('rules.b1.rows.4.1')],
                        [k('rules.b1.rows.5.0'), k('rules.b1.rows.5.1')],
                    ],
                },
            ],
        },
        {
            id: 'builder',
            heading: k('builder.heading'),
            note: k('builder.note'),
            blocks: [
                { kind: 'prose', html: k('builder.b0.html') },
                { kind: 'callout', tone: 'tip', html: k('builder.b1.html') },
            ],
        },
    ],
};

export default skeleton;
