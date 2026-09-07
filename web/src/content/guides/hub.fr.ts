// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { HubContent } from '../../lib/guides/types';

const hub: HubContent = {
  meta: {
    title: 'Guides - ItsBagelBot',
    description:
      "Guides visuels pour ItsBagelBot: installez le bot, apprenez le tableau de bord et écrivez des commandes personnalisées avec variables et données en direct.",
    eyebrow: 'Guides',
    heading: 'Apprivoisez le bot.',
    lead: 'Chaque guide fonctionne avec le forfait gratuit, et chacun se lit en dix minutes ou moins.',
  },
  tool: {
    href: '/fr/command-builder',
    eyebrow: 'Outil interactif',
    name: 'Constructeur de commandes',
    description:
      'Écrivez le message comme une phrase, cliquez pour insérer les parties intelligentes, regardez la répétition en direct, puis envoyez la commande terminée dans votre tableau de bord.',
    cta: 'Ouvrir le constructeur',
    demoIn: 'Bienvenue, <i>&#123;user&#125;</i>! Vous êtes le visiteur <i>&#123;counter:visites&#125;</i> 🥯',
    demoOut: '<b>ItsBagelBot:</b> Bienvenue, maya_live! Vous êtes le visiteur 128 🥯',
  },
  help: {
    prompt: 'Un guide ne couvre pas votre question?',
    links: [
      { href: 'https://discord.gg/SZ2remwSDv', label: 'Demandez sur Discord', external: true },
      { href: '/fr/contact', label: 'Contactez le support' },
      { href: 'https://dashboard.itsbagelbot.com?lang=fr', label: 'Ouvrir le tableau de bord', external: true },
    ],
  },
};

export default hub;
