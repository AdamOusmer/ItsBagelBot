// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// @ts-check
import { defineConfig } from 'astro/config';
import sitemap from '@astrojs/sitemap';
import mermaid from 'astro-mermaid';
import { readFlatCatalogs } from '@bagel/kit/i18n/fs';

const docsLocales = Object.keys(readFlatCatalogs('docs'));

const mermaidDarkVars = {
	fontFamily: '"DM Sans", system-ui, sans-serif; font-weight: 600',
	fontSize: '25px',

	actorFontSize: '25px',
	messageFontSize: '25px',
	noteFontSize: '25px',

	background: 'transparent',

	primaryColor: '#eadbbd',
	primaryTextColor: '#111113',
	primaryBorderColor: '#8f7a58',

	secondaryColor: '#f6efe1',
	secondaryTextColor: '#111113',
	secondaryBorderColor: '#8f7a58',

	tertiaryColor: '#15201b',
	tertiaryTextColor: '#eadbbd',
	tertiaryBorderColor: '#62a593',

	lineColor: '#eadbbd',
	textColor: '#f4f4f5',

	mainBkg: '#eadbbd',
	secondBkg: '#f6efe1',
	nodeBorder: '#8f7a58',

	clusterBkg: 'rgba(98, 165, 147, 0.08)',
	clusterBorder: 'rgba(98, 165, 147, 0.45)',

	edgeLabelBackground: '#1f1f23',
	titleColor: '#f4f4f5',

	actorBkg: '#eadbbd',
	actorBorder: '#8f7a58',
	actorTextColor: '#111113',
	actorLineColor: '#d8bf91',
	signalColor: '#f4f4f5',
	signalTextColor: '#f4f4f5',
	labelBoxBkgColor: '#15201b',
	labelBoxBorderColor: '#62a593',
	labelTextColor: '#eadbbd',
	loopTextColor: '#f4f4f5',
	noteBkgColor: '#1f1f23',
	noteTextColor: '#eadbbd',
	noteBorderColor: '#d8bf91',
	activationBkgColor: 'rgba(98, 165, 147, 0.22)',
	activationBorderColor: '#62a593',
};

const codeColor = (token) => `var(--doc-code-${token})`;

// Shiki validates theme colours as hex, so the theme names placeholders and swaps in the brand variables after highlighting.
const CODE_PLACEHOLDERS = {
	'#000001': codeColor('bg'),
	'#000002': codeColor('fg'),
	'#000003': codeColor('comment'),
	'#000004': codeColor('keyword'),
	'#000005': codeColor('string'),
	'#000006': codeColor('title'),
	'#000007': codeColor('constant'),
};

const codeTheme = {
	name: 'bagel-docs',
	type: 'dark',
	colorReplacements: CODE_PLACEHOLDERS,
	colors: { 'editor.background': '#000001', 'editor.foreground': '#000002' },
	tokenColors: [
		{ scope: ['comment', 'punctuation.definition.comment'], settings: { foreground: '#000003' } },
		{ scope: ['keyword', 'storage', 'keyword.operator.expression'], settings: { foreground: '#000004' } },
		{ scope: ['string', 'markup.inline.raw', 'string.quoted'], settings: { foreground: '#000005' } },
		{ scope: ['entity.name.function', 'entity.name.type', 'support.function', 'entity.name.tag'], settings: { foreground: '#000006' } },
		{ scope: ['constant', 'variable.other.constant', 'support.constant', 'variable.language'], settings: { foreground: '#000007' } },
	],
};

export default defineConfig({
	site: 'https://docs.itsbagelbot.com',
	trailingSlash: 'ignore',
	markdown: {
		shikiConfig: { theme: codeTheme },
	},
	integrations: [
		mermaid({
			theme: 'base',
			autoTheme: false,
			mermaidConfig: {
				securityLevel: 'strict',
				themeVariables: mermaidDarkVars,
				themeCSS: '.cluster-label span, .cluster-label text, .cluster text, .cluster span { font-size: 25px !important; font-weight: 600 !important; }',
				flowchart: { curve: 'basis', padding: 60, useMaxWidth: true, nodeSpacing: 30, rankSpacing: 50, subGraphTitleMargin: { top: 10, bottom: 14 } },
				sequence: { useMaxWidth: true, wrap: true, actorMargin: 30, messageMargin: 30, boxMargin: 20 },
				er: { useMaxWidth: true },
				gantt: { useMaxWidth: true },
			},
		}),
		sitemap({
			i18n: {
				defaultLocale: 'en',
				locales: Object.fromEntries(docsLocales.map((code) => [code, code])),
			},
		}),
	],
});
