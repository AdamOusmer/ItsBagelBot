// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// @ts-check
import { defineConfig } from 'astro/config';
import starlight from '@astrojs/starlight';
import mermaid from 'astro-mermaid';
import { readdirSync, readFileSync } from 'node:fs';

const localeFolder = new URL('./src/i18n/locales/', import.meta.url);
const docsCatalogs = Object.fromEntries(readdirSync(localeFolder).filter((name) => name.endsWith('.json')).map((name) => [name.slice(0, -5), JSON.parse(readFileSync(new URL(name, localeFolder), 'utf8'))]));
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

	primaryColor: '#e0c49a',
	primaryTextColor: '#14110c',
	primaryBorderColor: '#8a6a3e',

	secondaryColor: '#f5e6cf',
	secondaryTextColor: '#14110c',
	secondaryBorderColor: '#8a6a3e',

	tertiaryColor: '#15201b',
	tertiaryTextColor: '#e0c49a',
	tertiaryBorderColor: '#52b788',

	lineColor: '#e0c49a',
	textColor: '#f0ece4',

	mainBkg: '#e0c49a',
	secondBkg: '#f5e6cf',
	nodeBorder: '#8a6a3e',

	clusterBkg: 'rgba(82, 183, 136, 0.08)',
	clusterBorder: 'rgba(82, 183, 136, 0.45)',

	edgeLabelBackground: '#1d1a14',
	titleColor: '#f0ece4',

	actorBkg: '#e0c49a',
	actorBorder: '#8a6a3e',
	actorTextColor: '#14110c',
	actorLineColor: '#c9a87c',
	signalColor: '#f0ece4',
	signalTextColor: '#f0ece4',
	labelBoxBkgColor: '#15201b',
	labelBoxBorderColor: '#52b788',
	labelTextColor: '#e0c49a',
	loopTextColor: '#f0ece4',
	noteBkgColor: '#1d1a14',
	noteTextColor: '#e0c49a',
	noteBorderColor: '#c9a87c',
	activationBkgColor: 'rgba(82, 183, 136, 0.22)',
	activationBorderColor: '#52b788',
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
