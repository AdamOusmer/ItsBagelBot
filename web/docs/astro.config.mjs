// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// @ts-check
import { defineConfig } from 'astro/config';
import starlight from '@astrojs/starlight';
import mermaid from 'astro-mermaid';
import { readFlatCatalogs } from '@bagel/kit/i18n/fs';

const docsCatalogs = readFlatCatalogs('docs');
const docsLocales = Object.fromEntries(Object.entries(docsCatalogs).map(([code, copy]) => [code === 'en' ? 'root' : code, { label: copy['lang.name'] ?? code, lang: code }]));
const sidebarGroup = (key, directory) => ({
  label: docsCatalogs.en[key],
  translations: Object.fromEntries(Object.entries(docsCatalogs).map(([code, copy]) => [code, copy[key] ?? docsCatalogs.en[key]])),
  items: [{ autogenerate: { directory } }],
});

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

export default defineConfig({
	site: 'https://docs.itsbagelbot.com',
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
		starlight({
			title: 'ItsBagelBot',
			description: 'The all-in-one Twitch companion baked for independence.',
			defaultLocale: 'root',
			locales: docsLocales,
			logo: {
				src: './src/assets/LogoBOT.png',
				replacesTitle: false,
			},
			customCss: ['./src/styles/theme.css'],
			components: {
				Head: './src/components/Head.astro',
				SkipLink: './src/components/SkipLink.astro',
				Header: './src/components/Header.astro',
				Footer: './src/components/Footer.astro',
			},
			social: [
				{
					icon: 'github',
					label: 'GitHub',
					href: 'https://github.com/ItsMavey/ItsBagelBot',
				},
				{
					icon: 'discord',
					label: 'Discord',
					href: 'https://discord.gg/SZ2remwSDv',
				},
			],
			sidebar: [
				sidebarGroup('guides', 'guides'),
				sidebarGroup('architecture', 'architecture'),
				sidebarGroup('infrastructure', 'infrastructure'),
				sidebarGroup('data', 'data-and-state'),
				sidebarGroup('services', 'microservices'),
				sidebarGroup('qa', 'qa'),
				sidebarGroup('adr', 'adr'),
				sidebarGroup('reference', 'reference'),
			],
		}),
	],
});
