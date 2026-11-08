// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// @ts-check
import { defineConfig } from 'astro/config';
import sitemap from '@astrojs/sitemap';
import mermaid from 'astro-mermaid';
import { readFlatCatalogs } from '@bagel/kit/i18n/fs';

const docsLocales = Object.keys(readFlatCatalogs('docs'));


// Preserve published URLs after the code-focused documentation reorganization.
const movedPages = {
  "infrastructure/hardware-and-cluster": "infrastructure/runtime-configuration",
  "infrastructure/networking": "infrastructure/runtime-configuration",
  "infrastructure/new-relic-bottleneck-plan": "infrastructure/observability",
  "infrastructure/performance-roadmap": "infrastructure/performance",
  "adr/0001-rewriting-to-microservices": "adr/0001-rewrite-as-services",
  "adr/0002-adoption-of-go-as-primary-service-language": "adr/0002-go-for-services",
  "adr/0003-adoption-of-nats-as-communication-bridge": "adr/0003-nats-service-communication",
  "adr/0004-adoption-of-oracle-cloud": "adr",
  "adr/0005-adoption-of-mysql-heatwave": "adr/0004-mysql-durable-state",
  "adr/0006-adoption-of-elixir-for-twitch-ingress": "adr/0005-elixir-twitch-ingress",
  "adr/0007-adoption-of-per-schema-data-microservices": "adr/0006-data-service-ownership",
  "adr/0008-caching-and-write-behind-strategy": "adr/0007-cache-and-write-behind",
  "adr/0009-adoption-of-valkey-for-the-settings-projection": "adr/0008-shared-read-projection",
  "adr/0010-adoption-of-new-relic-for-observability": "adr",
  "adr/0011-transactions-owns-giveaway-winner-records": "adr/0009-transactions-own-giveaway-awards",
  "adr/0012-kit-variables-manifest-is-the-public-inventory": "adr/0010-kit-variable-inventory"
};
const redirects = Object.fromEntries(docsLocales.flatMap((locale) => {
	const prefix = locale === "en" ? "" : `/${locale}`;
	return Object.entries(movedPages).map(([from, to]) => [`${prefix}/${from}`, `${prefix}/${to}/`]);
}));

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
	redirects,
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
