// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// The structure of the "data-sources" guide: section order and ids, block kinds,
// which mock screen or widget each block shows, and the shape of the data
// those widgets take. Every k('...') names one line of copy in
// src/content/guides/data-sources.<lang>.ts.
import { k, type GuideSkeleton } from '../skeleton';

const skeleton: GuideSkeleton = {
    slug: 'data-sources',
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
            chips: [k('meta.card.chips.0'), k('meta.card.chips.1'), k('meta.card.chips.2')],
        },
    },
    sections: [
        {
            id: 'what',
            heading: k('what.heading'),
            note: k('what.note'),
            blocks: [
                { kind: 'prose', html: k('what.b0.html') },
                {
                    kind: 'chat',
                    title: k('what.b1.title'),
                    caption: k('what.b1.caption'),
                    lines: [
                        { who: 'viewer', name: k('what.b1.lines.0.name'), text: k('what.b1.lines.0.text') },
                        { who: 'bot', text: k('what.b1.lines.1.text') },
                    ],
                },
                { kind: 'prose', html: k('what.b2.html') },
                { kind: 'callout', tone: 'note', html: k('what.b3.html') },
            ],
        },
        {
            id: 'create',
            heading: k('create.heading'),
            note: k('create.note'),
            blocks: [
                { kind: 'prose', html: k('create.b0.html') },
                {
                    kind: 'steps',
                    items: [
                        { title: k('create.b1.items.0.title'), html: k('create.b1.items.0.html') },
                        { title: k('create.b1.items.1.title'), html: k('create.b1.items.1.html') },
                        { title: k('create.b1.items.2.title'), html: k('create.b1.items.2.html') },
                        { title: k('create.b1.items.3.title'), html: k('create.b1.items.3.html') },
                        { title: k('create.b1.items.4.title'), html: k('create.b1.items.4.html') },
                        { title: k('create.b1.items.5.title'), html: k('create.b1.items.5.html') },
                    ],
                },
                {
                    kind: 'dash',
                    screen: 'DataSourceModal',
                    path: '/commands',
                    caption: k('create.b2.caption'),
                    notes: [
                        { n: 1, text: k('create.b2.notes.0.text') },
                        { n: 2, text: k('create.b2.notes.1.text') },
                        { n: 3, text: k('create.b2.notes.2.text') },
                        { n: 4, text: k('create.b2.notes.3.text') },
                        { n: 5, text: k('create.b2.notes.4.text') },
                    ],
                    labels: {
                        panelHead: k('create.b2.labels.panelHead'),
                        close: k('create.b2.labels.close'),
                        intro: k('create.b2.labels.intro'),
                        fieldDisplayName: k('create.b2.labels.fieldDisplayName'),
                        displayNameValue: k('create.b2.labels.displayNameValue'),
                        fieldSlug: k('create.b2.labels.fieldSlug'),
                        slugHint: k('create.b2.labels.slugHint'),
                        fieldUrl: k('create.b2.labels.fieldUrl'),
                        fieldAuth: k('create.b2.labels.fieldAuth'),
                        authValue: k('create.b2.labels.authValue'),
                        fetchSample: k('create.b2.labels.fetchSample'),
                        pasteInstead: k('create.b2.labels.pasteInstead'),
                        pickPrompt: k('create.b2.labels.pickPrompt'),
                        treeRoot: k('create.b2.labels.treeRoot'),
                        picked: k('create.b2.labels.picked'),
                        wholeResponse: k('create.b2.labels.wholeResponse'),
                        cancel: k('create.b2.labels.cancel'),
                        create: k('create.b2.labels.create'),
                    },
                },
                { kind: 'prose', html: k('create.b3.html') },
                {
                    kind: 'dash',
                    screen: 'DataSourcePicker',
                    path: '/commands',
                    caption: k('create.b4.caption'),
                    notes: [
                        { n: 1, text: k('create.b4.notes.0.text') },
                        { n: 2, text: k('create.b4.notes.1.text') },
                        { n: 3, text: k('create.b4.notes.2.text') },
                    ],
                    labels: {
                        fieldResponse: k('create.b4.labels.fieldResponse'),
                        responseHtml: k('create.b4.labels.responseHtml'),
                        insertVariable: k('create.b4.labels.insertVariable'),
                        chipCounter: k('create.b4.labels.chipCounter'),
                        chipDataSource: k('create.b4.labels.chipDataSource'),
                        popoverTitle: k('create.b4.labels.popoverTitle'),
                        row2Path: k('create.b4.labels.row2Path'),
                        newSource: k('create.b4.labels.newSource'),
                    },
                },
                { kind: 'callout', tone: 'tip', html: k('create.b5.html') },
            ],
        },
        {
            id: 'paths',
            heading: k('paths.heading'),
            note: k('paths.note'),
            blocks: [
                { kind: 'prose', html: k('paths.b0.html') },
                {
                    kind: 'widget',
                    name: 'PathPicker',
                    props: {
                        sample: `{
  "latitude": 45.5,
  "longitude": -73.6,
  "current": {
    "time": "2026-09-07T14:00",
    "temperature_2m": 21.4,
    "wind_speed_10m": 9.2,
    "rain": null
  },
  "units": {
    "temperature_2m": "°C",
    "wind_speed_10m": "km/h"
  },
  "hourly": {
    "temperature_2m": [20.1, 21.4, 22.8, 23.2]
  }
}`,
                    },
                    labels: {
                        sampleLabel: k('paths.b1.labels.sampleLabel'),
                        treeLabel: k('paths.b1.labels.treeLabel'),
                        empty: k('paths.b1.labels.empty'),
                        badJson: k('paths.b1.labels.badJson'),
                        tooDeep: k('paths.b1.labels.tooDeep'),
                        badSegment: k('paths.b1.labels.badSegment'),
                        notUsable: k('paths.b1.labels.notUsable'),
                        savedPathLabel: k('paths.b1.labels.savedPathLabel'),
                        tokenLabel: k('paths.b1.labels.tokenLabel'),
                        overrideLabel: k('paths.b1.labels.overrideLabel'),
                        chatLabel: k('paths.b1.labels.chatLabel'),
                        cutHere: k('paths.b1.labels.cutHere'),
                        wholeButton: k('paths.b1.labels.wholeButton'),
                        wholeLabel: k('paths.b1.labels.wholeLabel'),
                        wholeNote: k('paths.b1.labels.wholeNote'),
                        defName: k('paths.b1.labels.defName'),
                    },
                },
                {
                    kind: 'table',
                    head: [k('paths.b2.head.0'), k('paths.b2.head.1')],
                    caption: k('paths.b2.caption'),
                    rows: [
                        [k('paths.b2.rows.0.0'), k('paths.b2.rows.0.1')],
                        [k('paths.b2.rows.1.0'), k('paths.b2.rows.1.1')],
                        [k('paths.b2.rows.2.0'), k('paths.b2.rows.2.1')],
                        [k('paths.b2.rows.3.0'), k('paths.b2.rows.3.1')],
                        [k('paths.b2.rows.4.0'), k('paths.b2.rows.4.1')],
                        [k('paths.b2.rows.5.0'), k('paths.b2.rows.5.1')],
                        [k('paths.b2.rows.6.0'), k('paths.b2.rows.6.1')],
                        [k('paths.b2.rows.7.0'), k('paths.b2.rows.7.1')],
                    ],
                },
                { kind: 'callout', tone: 'tip', html: k('paths.b3.html') },
            ],
        },
        {
            id: 'keys',
            heading: k('keys.heading'),
            note: k('keys.note'),
            blocks: [
                { kind: 'prose', html: k('keys.b0.html') },
                { kind: 'callout', tone: 'warn', html: k('keys.b1.html') },
            ],
        },
        {
            id: 'limits',
            heading: k('limits.heading'),
            note: k('limits.note'),
            blocks: [
                { kind: 'prose', html: k('limits.b0.html') },
                {
                    kind: 'table',
                    head: [k('limits.b1.head.0'), k('limits.b1.head.1')],
                    caption: k('limits.b1.caption'),
                    rows: [
                        [k('limits.b1.rows.0.0'), k('limits.b1.rows.0.1')],
                        [k('limits.b1.rows.1.0'), k('limits.b1.rows.1.1')],
                        [k('limits.b1.rows.2.0'), k('limits.b1.rows.2.1')],
                        [k('limits.b1.rows.3.0'), k('limits.b1.rows.3.1')],
                        [k('limits.b1.rows.4.0'), k('limits.b1.rows.4.1')],
                        [k('limits.b1.rows.5.0'), k('limits.b1.rows.5.1')],
                        [k('limits.b1.rows.6.0'), k('limits.b1.rows.6.1')],
                        [k('limits.b1.rows.7.0'), k('limits.b1.rows.7.1')],
                        [k('limits.b1.rows.8.0'), k('limits.b1.rows.8.1')],
                        [k('limits.b1.rows.9.0'), k('limits.b1.rows.9.1')],
                        [k('limits.b1.rows.10.0'), k('limits.b1.rows.10.1')],
                        [k('limits.b1.rows.11.0'), k('limits.b1.rows.11.1')],
                        [k('limits.b1.rows.12.0'), k('limits.b1.rows.12.1')],
                        [k('limits.b1.rows.13.0'), k('limits.b1.rows.13.1')],
                    ],
                },
                {
                    kind: 'widget',
                    name: 'FetchBudget',
                    props: { runs: 12 },
                    labels: {
                        runsLabel: k('limits.b2.labels.runsLabel'),
                        runsUnit: k('limits.b2.labels.runsUnit'),
                        sourcesLabel: k('limits.b2.labels.sourcesLabel'),
                        fetchesLabel: k('limits.b2.labels.fetchesLabel'),
                        fetchesUnit: k('limits.b2.labels.fetchesUnit'),
                        cachedLabel: k('limits.b2.labels.cachedLabel'),
                        blockedLabel: k('limits.b2.labels.blockedLabel'),
                        blockedLine: k('limits.b2.labels.blockedLine'),
                        why: k('limits.b2.labels.why'),
                    },
                },
                { kind: 'callout', tone: 'note', html: k('limits.b3.html') },
            ],
        },
        {
            id: 'errors',
            heading: k('errors.heading'),
            note: k('errors.note'),
            blocks: [
                { kind: 'prose', html: k('errors.b0.html') },
                {
                    kind: 'table',
                    head: [k('errors.b1.head.0'), k('errors.b1.head.1')],
                    rows: [
                        [k('errors.b1.rows.0.0'), k('errors.b1.rows.0.1')],
                        [k('errors.b1.rows.1.0'), k('errors.b1.rows.1.1')],
                        [k('errors.b1.rows.2.0'), k('errors.b1.rows.2.1')],
                        [k('errors.b1.rows.3.0'), k('errors.b1.rows.3.1')],
                    ],
                },
                {
                    kind: 'widget',
                    name: 'FetchOutcomes',
                    labels: {
                        legend: k('errors.b2.labels.legend'),
                        title: k('errors.b2.labels.title'),
                        viewer: k('errors.b2.labels.viewer'),
                        viewerText: k('errors.b2.labels.viewerText'),
                        botName: k('errors.b2.labels.botName'),
                    },
                    props: {
                        outcomes: [
                            {
                                id: 'denied',
                                label: k('errors.b2.props.outcomes.0.label'),
                                bot: k('errors.b2.props.outcomes.0.bot'),
                                why: k('errors.b2.props.outcomes.0.why'),
                            },
                            {
                                id: 'limited',
                                label: k('errors.b2.props.outcomes.1.label'),
                                bot: k('errors.b2.props.outcomes.1.bot'),
                                why: k('errors.b2.props.outcomes.1.why'),
                            },
                            {
                                id: 'timeout',
                                label: k('errors.b2.props.outcomes.2.label'),
                                bot: k('errors.b2.props.outcomes.2.bot'),
                                why: k('errors.b2.props.outcomes.2.why'),
                            },
                            {
                                id: 'nopath',
                                label: k('errors.b2.props.outcomes.3.label'),
                                bot: k('errors.b2.props.outcomes.3.bot'),
                                why: k('errors.b2.props.outcomes.3.why'),
                            },
                            {
                                id: 'paused',
                                label: k('errors.b2.props.outcomes.4.label'),
                                bot: k('errors.b2.props.outcomes.4.bot'),
                                why: k('errors.b2.props.outcomes.4.why'),
                            },
                            {
                                id: 'toomany',
                                label: k('errors.b2.props.outcomes.5.label'),
                                bot: k('errors.b2.props.outcomes.5.bot'),
                                why: k('errors.b2.props.outcomes.5.why'),
                            },
                        ],
                    },
                },
                { kind: 'callout', tone: 'note', html: k('errors.b3.html') },
            ],
        },
        {
            id: 'rules',
            heading: k('rules.heading'),
            note: k('rules.note'),
            blocks: [
                { kind: 'prose', html: k('rules.b0.html') },
                { kind: 'callout', tone: 'tip', html: k('rules.b1.html') },
            ],
        },
    ],
};

export default skeleton;
