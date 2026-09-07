// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { GuideContent } from '../../lib/guides/types';

// Screen labels shared by the three dashboard mocks on this page. The screens
// carry the English strings as defaults, so a locale only lists what differs.
const dock = {
  dockOverview: 'Aperçu',
  dockCommands: 'Commandes',
  dockModules: 'Modules',
  dockDiscord: 'Discord',
  dockBilling: 'Facturation',
  dockSettings: 'Paramètres',
};
const account = 'streamer · Diffuseur';

const guide: GuideContent = {
  slug: 'getting-started',
  meta: {
    title: 'Bien démarrer - Guides ItsBagelBot',
    description:
      'Installez ItsBagelBot en quelques minutes: connectez votre chaîne Twitch, visitez le tableau de bord, créez votre première commande et activez votre premier module.',
    eyebrow: 'Guide',
    heading: 'Bien démarrer',
    lead: 'De «Ajouter à Twitch» à votre premier stream avec le bot dans le chat. Sept minutes en tout, surtout de la lecture.',
    minutes: '7 min de lecture',
    card: {
      title: 'Bien démarrer',
      description:
        'De «Ajouter à Twitch» à votre premier stream avec le bot: connectez la chaîne, repérez-vous dans le tableau de bord et activez vos premiers outils.',
      meta: '7 min · 5 étapes',
      chips: ['connexion', 'visite guidée', 'premier module'],
    },
  },
  sections: [
    {
      id: 'connect',
      heading: 'Connectez votre chaîne',
      note: 'Une seule connexion Twitch. Aucune clé API, aucun fichier de config, rien à installer.',
      blocks: [
        {
          kind: 'prose',
          html: `
            <p>
                Rendez-vous sur <a href="https://dashboard.itsbagelbot.com/auth/login?lang=fr" target="_blank" rel="noopener noreferrer">dashboard.itsbagelbot.com</a>
                et connectez-vous avec votre compte Twitch. Twitch affiche exactement ce que le bot a le
                droit de faire avant que vous n'approuviez quoi que ce soit, et c'est toute
                l'installation. Dès que vous êtes connecté, ItsBagelBot rejoint votre chat et reste
                silencieux tant qu'on ne lui parle pas.
            </p>`,
        },
        {
          kind: 'prose',
          html: `
            <p>
                À la première connexion, une courte visite guidée vous accueille: accepter les
                conditions, choisir la langue de votre console, puis rendre le bot modérateur en
                tapant <code>/mod ItsBagelBot</code> dans votre propre chat. Ce dernier point compte
                plus qu'il n'y paraît: sans le statut de modérateur, Twitch fait taire le bot dès que
                votre chat passe en mode abonnés ou followers seulement, et ça ressemble à une panne.
            </p>`,
        },
        {
          kind: 'chat',
          title: '#votre_chaine',
          caption: 'Ce que votre chat voit, environ deux secondes après votre connexion.',
          lines: [
            { who: 'system', text: 'itsbagelbot a rejoint #votre_chaine' },
            { who: 'viewer', name: 'maya_live', text: 'oh, un nouveau bot, salut!' },
          ],
        },
        {
          kind: 'callout',
          tone: 'tip',
          html: `
                <b>Bon à savoir</b>
                Le bot ne parle jamais sans y être invité. Tant que vous n'avez pas créé de commandes ni
                activé de modules, rejoindre le chat est la seule chose qu'il fait: votre chat reste
                exactement comme avant.`,
        },
      ],
    },
    {
      id: 'tour',
      heading: 'Repérez-vous',
      note: 'Six arrêts dans le dock flottant. Vous passerez presque tout votre temps dans deux d’entre eux.',
      blocks: [
        {
          kind: 'prose',
          html: `
            <p>
                Le tableau de bord affiche une page à la fois, et toute sa navigation tient dans le
                dock flottant en bas de l'écran, identique sur ordinateur et téléphone.
                <strong>Aperçu</strong> est votre page d'accueil, <strong>Commandes</strong>
                héberge les commandes personnalisées, et <strong>Modules</strong> regroupe toutes les
                grandes fonctions. <strong>Discord</strong> a sa propre page pour connecter un serveur
                Discord (bêta payante). Facturation et Paramètres font ce que leur nom dit.
            </p>`,
        },
        {
          kind: 'dash',
          screen: 'DashboardHome',
          path: '/',
          caption: 'La page Aperçu. La navigation vit dans le dock flottant, en bas.',
          notes: [
            { n: 1, text: 'Le dock est toute la navigation, sur tous les écrans: Aperçu, Commandes, Modules, Discord, Facturation, Paramètres.' },
            { n: 2, text: "État du bot: ItsBagelBot est-il dans votre chat en ce moment, avec le bouton pour corriger si besoin." },
            { n: 3, text: 'Actions rapides: les deux gestes les plus fréquents, à un clic.' },
            { n: 4, text: 'Vos commandes les plus utilisées vivent ici aussi, à un clic de la modification.' },
          ],
          labels: {
            ...dock,
            account,
            crumbPage: 'Aperçu',
            eyebrow: 'État',
            titleHtml: 'Bonsoir, <i>streamer</i>',
            statusState: 'En ligne · dans le chat',
            statusNote: "· rien ne vous attend pour l'instant",
            restart: 'Redémarrer',
            disconnect: 'Déconnecter',
            quickActions: 'Actions rapides',
            newCommand: 'Nouvelle commande',
            manageModules: 'Gérer les modules',
            topCommands: 'Vos commandes favorites',
            cmd1Response: '&#123;user&#125; lance un bagel tout chaud à &#123;target&#125;. Croustillant.',
            cmd1Count: '1,2k',
            cmd2Response: "&#123;user&#125; se fond dans l'ombre. Merci pour le lurk.",
            uses: 'utilisations',
          },
        },
        {
          kind: 'prose',
          html: `
            <p>
                Tout ce que vous faites dans le tableau de bord est enregistré dès que vous confirmez:
                aucune étape de «déploiement», et les changements atteignent votre chat généralement en
                quelques secondes.
            </p>`,
        },
      ],
    },
    {
      id: 'first-command',
      heading: 'Créez votre première commande',
      note: 'Un nom et une réponse. Tout le reste est optionnel.',
      blocks: [
        {
          kind: 'prose',
          html: `
            <p>
                Ouvrez <strong>Commandes</strong> et appuyez sur <strong>Nouvelle commande</strong>.
                Une commande n'exige que deux choses: un <strong>nom</strong> (ce que les spectateurs
                tapent après le <code>!</code>) et une <strong>réponse</strong> (ce que le bot répond).
                Faisons le grand classique:
            </p>`,
        },
        {
          kind: 'dash',
          screen: 'CommandsList',
          path: '/commands',
          caption: "La page Commandes: votre liste à gauche, l'éditeur amarré à droite.",
          notes: [
            { n: 1, text: "Nouvelle commande ouvre l'éditeur, amarré à côté de la liste sur ordinateur (une feuille en bas sur téléphone)." },
            { n: 2, text: 'Le nom, sans le «!» (le tableau de bord l’ajoute dans le chat). Minuscules, sans espaces.' },
            { n: 3, text: 'La réponse. Du texte simple suffit; le prochain guide montre les variables qui la rendent intelligente.' },
            { n: 4, text: 'Créer enregistre immédiatement, et la commande arrive dans le chat juste après.' },
          ],
          labels: {
            ...dock,
            account,
            crumbPage: 'Commandes',
            eyebrow: 'Gérer',
            titleHtml: 'Commandes de <i>chat</i>',
            filterAll: 'Toutes',
            filterActive: 'Actives',
            filterCustom: 'Personnalisées',
            searchPlaceholder: 'Nom, alias, réponse…',
            newCommand: 'Nouvelle commande',
            builtIn: 'intégrée',
            row1Meta: 'Tous · 15s',
            row2Text: 'Viens jaser entre les streams…',
            row2Meta: 'Tous · 0s',
            panelHead: 'Nouvelle commande',
            fieldName: 'Nom',
            fieldResponse: 'Réponse',
            responseValue: 'Viens jaser entre les streams: discord.gg/votre-invitation',
            chatTag: 'Répétition du chat',
            cancel: 'Annuler',
            create: 'Créer',
          },
        },
        {
          kind: 'chat',
          title: '#votre_chaine',
          caption: 'Trente secondes plus tard, dans le chat.',
          lines: [
            { who: 'viewer', name: 'maya_live', text: '!discord' },
            { who: 'bot', text: 'Viens jaser entre les streams: discord.gg/votre-invitation' },
          ],
        },
        {
          kind: 'prose',
          html: `
            <p>
                L'éditeur propose aussi des niveaux d'accès (de tout le monde jusqu'au diffuseur), un délai entre
                les utilisations et un interrupteur «seulement en direct». Tout est optionnel et tout
                est expliqué dans le <a href="/fr/guides/commands">guide des commandes</a>.
            </p>`,
        },
      ],
    },
    {
      id: 'first-module',
      heading: 'Activez votre premier module',
      note: 'Les modules sont de grandes fonctions avec un interrupteur. Deux sont déjà actifs pour vous.',
      blocks: [
        {
          kind: 'prose',
          html: `
            <p>
                Direction <strong>Modules</strong>. Chaque tuile est une fonction avec son propre
                interrupteur, et cliquer la tuile ouvre ses réglages. Deux travaillent déjà pour vous:
                les <strong>Alertes de chat</strong> (follows, subs, cheers, raids) et
                <strong>AutoMod</strong> (la modération en couches présentée sur la page d'accueil; il travaille discrètement sans tuile sur cette grille).
                Deux autres n'affichent jamais d'interrupteur: <strong>Counters</strong> et
                <strong>Stream Management</strong> (les commandes derrière <code>!title</code>,
                <code>!game</code> et <code>!marker</code>) sont toujours actives.
            </p>`,
        },
        {
          kind: 'dash',
          screen: 'ModulesGrid',
          path: '/modules',
          caption: 'La page Modules: un rail de catégories à gauche, des tuiles avec Configurer et un interrupteur.',
          labels: { dot3: '', dot4: '', dot5: '', dot6: '' },
          notes: [
            { n: 1, text: 'Le rail des catégories. Cliquez un groupe et la grille défile jusqu\'à lui.' },
            { n: 2, text: 'Une tuile est un module: son nom, sa catégorie, une ligne qui le décrit, Configurer et l\'interrupteur. Les alertes de chat démarrent actives; la plupart des autres attendent votre feu vert.' },
          ],
          labels: {
            ...dock,
            account,
            eyebrow: 'Gérer',
            titleHtml: 'Modules de <i>chaîne</i>',
            sub: 'Fonctions optionnelles pour votre chaîne. 1 sur 20 activée.',
            searchPlaceholder: 'Rechercher un module…',
            configure: 'Configurer',
          },
        },
        {
          kind: 'prose',
          html: `
            <p>
                Rien ici n'est risqué à explorer: chaque module se désactive aussi vite qu'il s'active,
                et ses réglages sont conservés pour la prochaine fois. La visite complète de chaque
                module se trouve dans le <a href="/fr/guides/modules">manuel des modules</a>.
            </p>`,
        },
      ],
    },
    {
      id: 'go-live',
      heading: 'Passez en direct sereinement',
      note: 'Une liste de vérification de deux minutes, puis le bot prend le quart de nuit.',
      blocks: [
        {
          kind: 'prose',
          html: `
            <p>Avant votre prochain stream, une petite répétition:</p>
            <ol>
                <li>Tapez vous-même votre nouvelle commande dans le chat: le bot vous répond comme à n'importe quel spectateur.</li>
                <li>Relisez les messages des <strong>Alertes de chat</strong> et donnez-leur votre ton.</li>
                <li>Souvent raidé? Activez le <strong>Shoutout automatique</strong> pour que les raiders soient salués même quand vous êtes en pleine partie.</li>
            </ol>
            <p>
                Et voilà toute l'installation. À partir d'ici, le bot modère, accueille et répond tout
                seul, et tout ce que vous venez de faire se modifie en plein stream depuis votre
                téléphone.
            </p>`,
        },
        {
          kind: 'callout',
          tone: 'tip',
          html: `
                <b>La suite</b>
                Rendez vos commandes intelligentes: <a href="/fr/guides/commands">Commandes et variables</a>
                montre comment une ligne comme <code>Bienvenue, &#123;user&#125;!</code> salue chaque
                spectateur par son nom.`,
        },
        {
          kind: 'widget',
          name: 'Checklist',
          labels: {
            heading: 'Votre liste de la première heure',
            reset: 'Effacer',
          },
          props: {
            storageKey: 'guides.getting-started.checklist',
            items: [
              'Connectez-vous sur <a href="https://dashboard.itsbagelbot.com/auth/login?lang=fr" target="_blank" rel="noopener noreferrer">dashboard.itsbagelbot.com</a> et rendez le bot modérateur avec <code>/mod ItsBagelBot</code>.',
              'Créez une commande, comme <code>!discord</code> ou <code>!socials</code>.',
              'Ouvrez <strong>Modules</strong> et réécrivez les messages des <strong>Alertes de chat</strong> à votre façon.',
              'Activez <strong>Local Time</strong> et réglez votre fuseau horaire, pour que <code>!time</code> réponde correctement.',
              'Ajoutez un modérateur dans <strong>Paramètres</strong>, pour que quelqu\'un d\'autre puisse vous aider.',
              'Tapez votre nouvelle commande dans le chat vous-même et vérifiez la réponse.',
              'Essayez <code>!uptime</code> une fois en direct.',
            ],
          },
        },
      ],
    },
  ],
};

export default guide;
