// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// The "getting-started" guide in fr. Copy only: the structure it fills lives in
// src/lib/guides/skeletons/getting-started.ts, and every key below is one k('...')
// there. Adding a language is this file translated, with no structure to get
// wrong; a key this locale omits falls back to English.
import type { GuideStrings } from '../../lib/guides/skeleton';

const strings: GuideStrings = {
    'connect.b0.html': `
            <p>
                Rendez-vous sur <a href="https://dashboard.itsbagelbot.com/auth/login?lang=fr" target="_blank" rel="noopener noreferrer">dashboard.itsbagelbot.com</a>
                et connectez-vous avec votre compte Twitch. Twitch affiche exactement ce que le bot a le
                droit de faire avant que vous n'approuviez quoi que ce soit, et c'est toute
                l'installation. Dès que vous êtes connecté, ItsBagelBot rejoint votre chat et reste
                silencieux tant qu'on ne lui parle pas.
            </p>`,
    'connect.b1.html': `
            <p>
                À la première connexion, une courte visite guidée vous accueille: accepter les
                conditions, choisir la langue de votre console, puis rendre le bot modérateur en
                tapant <code>/mod ItsBagelBot</code> dans votre propre chat. Ce dernier point compte
                plus qu'il n'y paraît: sans le statut de modérateur, Twitch fait taire le bot dès que
                votre chat passe en mode abonnés ou followers seulement, et ça ressemble à une panne.
            </p>`,
    'connect.b2.caption': 'Ce que votre chat voit, environ deux secondes après votre connexion.',
    'connect.b2.lines.0.text': 'itsbagelbot a rejoint #votre_chaine',
    'connect.b2.lines.1.name': 'maya_live',
    'connect.b2.lines.1.text': 'oh, un nouveau bot, salut!',
    'connect.b2.title': '#votre_chaine',
    'connect.b3.html': `
                <b>Bon à savoir</b>
                Le bot ne parle jamais sans y être invité. Tant que vous n'avez pas créé de commandes ni
                activé de modules, rejoindre le chat est la seule chose qu'il fait: votre chat reste
                exactement comme avant.`,
    'connect.heading': 'Connectez votre chaîne',
    'connect.note': 'Une seule connexion Twitch. Aucune clé API, aucun fichier de config, rien à installer.',
    'first-command.b0.html': `
            <p>
                Ouvrez <strong>Commandes</strong> et appuyez sur <strong>Nouvelle commande</strong>.
                Une commande n'exige que deux choses: un <strong>nom</strong> (ce que les spectateurs
                tapent après le <code>!</code>) et une <strong>réponse</strong> (ce que le bot répond).
                Faisons le grand classique:
            </p>`,
    'first-command.b1.caption': "La page Commandes: votre liste à gauche, l'éditeur amarré à droite.",
    'first-command.b1.labels.account': 'streamer · Diffuseur',
    'first-command.b1.labels.builtIn': 'intégrée',
    'first-command.b1.labels.cancel': 'Annuler',
    'first-command.b1.labels.chatTag': 'Répétition du chat',
    'first-command.b1.labels.create': 'Créer',
    'first-command.b1.labels.crumbPage': 'Commandes',
    'first-command.b1.labels.dockBilling': 'Facturation',
    'first-command.b1.labels.dockCommands': 'Commandes',
    'first-command.b1.labels.dockDiscord': 'Discord',
    'first-command.b1.labels.dockModules': 'Modules',
    'first-command.b1.labels.dockOverview': 'Aperçu',
    'first-command.b1.labels.dockSettings': 'Paramètres',
    'first-command.b1.labels.eyebrow': 'Gérer',
    'first-command.b1.labels.fieldName': 'Nom',
    'first-command.b1.labels.fieldResponse': 'Réponse',
    'first-command.b1.labels.filterActive': 'Actives',
    'first-command.b1.labels.filterAll': 'Toutes',
    'first-command.b1.labels.filterCustom': 'Personnalisées',
    'first-command.b1.labels.newCommand': 'Nouvelle commande',
    'first-command.b1.labels.panelHead': 'Nouvelle commande',
    'first-command.b1.labels.responseValue': 'Viens jaser entre les streams: discord.gg/votre-invitation',
    'first-command.b1.labels.row1Meta': 'Tous · 15s',
    'first-command.b1.labels.row2Meta': 'Tous · 0s',
    'first-command.b1.labels.row2Text': 'Viens jaser entre les streams…',
    'first-command.b1.labels.searchPlaceholder': 'Nom, alias, réponse…',
    'first-command.b1.labels.titleHtml': 'Commandes de <i>chat</i>',
    'first-command.b1.notes.0.text': "Nouvelle commande ouvre l'éditeur, amarré à côté de la liste sur ordinateur (une feuille en bas sur téléphone).",
    'first-command.b1.notes.1.text': 'Le nom, sans le «!» (le tableau de bord l’ajoute dans le chat). Minuscules, sans espaces.',
    'first-command.b1.notes.2.text': 'La réponse. Du texte simple suffit; le prochain guide montre les variables qui la rendent intelligente.',
    'first-command.b1.notes.3.text': 'Créer enregistre immédiatement, et la commande arrive dans le chat juste après.',
    'first-command.b2.caption': 'Trente secondes plus tard, dans le chat.',
    'first-command.b2.lines.0.name': 'maya_live',
    'first-command.b2.lines.0.text': '!discord',
    'first-command.b2.lines.1.text': 'Viens jaser entre les streams: discord.gg/votre-invitation',
    'first-command.b2.title': '#votre_chaine',
    'first-command.b3.html': `
            <p>
                L'éditeur propose aussi des niveaux d'accès (de tout le monde jusqu'au diffuseur), un délai entre
                les utilisations et un interrupteur «seulement en direct». Tout est optionnel et tout
                est expliqué dans le <a href="/fr/guides/commands">guide des commandes</a>.
            </p>`,
    'first-command.heading': 'Créez votre première commande',
    'first-command.note': 'Un nom et une réponse. Tout le reste est optionnel.',
    'first-module.b0.html': `
            <p>
                Direction <strong>Modules</strong>. Chaque tuile est une fonction avec son propre
                interrupteur, et cliquer la tuile ouvre ses réglages. Deux travaillent déjà pour vous:
                les <strong>Alertes de chat</strong> (follows, subs, cheers, raids) et
                <strong>AutoMod</strong> (la modération en couches présentée sur la page d'accueil; il travaille discrètement sans tuile sur cette grille).
                Deux autres n'affichent jamais d'interrupteur: <strong>Counters</strong> et
                <strong>Stream Management</strong> (les commandes derrière <code>!title</code>,
                <code>!game</code> et <code>!marker</code>) sont toujours actives.
            </p>`,
    'first-module.b1.caption': 'La page Modules: un rail de catégories à gauche, des tuiles avec Configurer et un interrupteur.',
    'first-module.b1.labels.account': 'streamer · Diffuseur',
    'first-module.b1.labels.configure': 'Configurer',
    'first-module.b1.labels.dockBilling': 'Facturation',
    'first-module.b1.labels.dockCommands': 'Commandes',
    'first-module.b1.labels.dockDiscord': 'Discord',
    'first-module.b1.labels.dockModules': 'Modules',
    'first-module.b1.labels.dockOverview': 'Aperçu',
    'first-module.b1.labels.dockSettings': 'Paramètres',
    'first-module.b1.labels.eyebrow': 'Gérer',
    'first-module.b1.labels.searchPlaceholder': 'Rechercher un module…',
    'first-module.b1.labels.sub': 'Fonctions optionnelles pour votre chaîne. 1 sur 20 activée.',
    'first-module.b1.labels.titleHtml': 'Modules de <i>chaîne</i>',
    'first-module.b1.notes.0.text': "Le rail des catégories. Cliquez un groupe et la grille défile jusqu'à lui.",
    'first-module.b1.notes.1.text': "Une tuile est un module: son nom, sa catégorie, une ligne qui le décrit, Configurer et l'interrupteur. Les alertes de chat démarrent actives; la plupart des autres attendent votre feu vert.",
    'first-module.b2.html': `
            <p>
                Rien ici n'est risqué à explorer: chaque module se désactive aussi vite qu'il s'active,
                et ses réglages sont conservés pour la prochaine fois. La visite complète de chaque
                module se trouve dans le <a href="/fr/guides/modules">manuel des modules</a>.
            </p>`,
    'first-module.heading': 'Activez votre premier module',
    'first-module.note': 'Les modules sont de grandes fonctions avec un interrupteur. Deux sont déjà actifs pour vous.',
    'go-live.b0.html': `
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
    'go-live.b1.html': `
                <b>La suite</b>
                Rendez vos commandes intelligentes: <a href="/fr/guides/commands">Commandes et variables</a>
                montre comment une ligne comme <code>Bienvenue, &#123;user&#125;!</code> salue chaque
                spectateur par son nom.`,
    'go-live.b2.labels.heading': 'Votre liste de la première heure',
    'go-live.b2.labels.reset': 'Effacer',
    'go-live.b2.props.items.0': 'Connectez-vous sur <a href="https://dashboard.itsbagelbot.com/auth/login?lang=fr" target="_blank" rel="noopener noreferrer">dashboard.itsbagelbot.com</a> et rendez le bot modérateur avec <code>/mod ItsBagelBot</code>.',
    'go-live.b2.props.items.1': 'Créez une commande, comme <code>!discord</code> ou <code>!socials</code>.',
    'go-live.b2.props.items.2': 'Ouvrez <strong>Modules</strong> et réécrivez les messages des <strong>Alertes de chat</strong> à votre façon.',
    'go-live.b2.props.items.3': 'Activez <strong>Local Time</strong> et réglez votre fuseau horaire, pour que <code>!time</code> réponde correctement.',
    'go-live.b2.props.items.4': "Ajoutez un modérateur dans <strong>Paramètres</strong>, pour que quelqu'un d'autre puisse vous aider.",
    'go-live.b2.props.items.5': 'Tapez votre nouvelle commande dans le chat vous-même et vérifiez la réponse.',
    'go-live.b2.props.items.6': 'Essayez <code>!uptime</code> une fois en direct.',
    'go-live.heading': 'Passez en direct sereinement',
    'go-live.note': 'Une liste de vérification de deux minutes, puis le bot prend le quart de nuit.',
    'meta.card.chips.0': 'connexion',
    'meta.card.chips.1': 'visite guidée',
    'meta.card.chips.2': 'premier module',
    'meta.card.description': 'De «Ajouter à Twitch» à votre premier stream avec le bot: connectez la chaîne, repérez-vous dans le tableau de bord et activez vos premiers outils.',
    'meta.card.meta': '7 min · 5 étapes',
    'meta.card.title': 'Bien démarrer',
    'meta.description': 'Installez ItsBagelBot en quelques minutes: connectez votre chaîne Twitch, visitez le tableau de bord, créez votre première commande et activez votre premier module.',
    'meta.eyebrow': 'Guide',
    'meta.heading': 'Bien démarrer',
    'meta.lead': 'De «Ajouter à Twitch» à votre premier stream avec le bot dans le chat. Sept minutes en tout, surtout de la lecture.',
    'meta.minutes': '7 min de lecture',
    'meta.title': 'Bien démarrer - Guides ItsBagelBot',
    'tour.b0.html': `
            <p>
                Le tableau de bord affiche une page à la fois, et toute sa navigation tient dans le
                dock flottant en bas de l'écran, identique sur ordinateur et téléphone.
                <strong>Aperçu</strong> est votre page d'accueil, <strong>Commandes</strong>
                héberge les commandes personnalisées, et <strong>Modules</strong> regroupe toutes les
                grandes fonctions. <strong>Discord</strong> a sa propre page pour connecter un serveur
                Discord (bêta payante). Facturation et Paramètres font ce que leur nom dit.
            </p>`,
    'tour.b1.caption': 'La page Aperçu. La navigation vit dans le dock flottant, en bas.',
    'tour.b1.labels.account': 'streamer · Diffuseur',
    'tour.b1.labels.cmd1Count': '1,2k',
    'tour.b1.labels.cmd1Response': '&#123;user&#125; lance un bagel tout chaud à &#123;target&#125;. Croustillant.',
    'tour.b1.labels.cmd2Response': "&#123;user&#125; se fond dans l'ombre. Merci pour le lurk.",
    'tour.b1.labels.crumbPage': 'Aperçu',
    'tour.b1.labels.disconnect': 'Déconnecter',
    'tour.b1.labels.dockBilling': 'Facturation',
    'tour.b1.labels.dockCommands': 'Commandes',
    'tour.b1.labels.dockDiscord': 'Discord',
    'tour.b1.labels.dockModules': 'Modules',
    'tour.b1.labels.dockOverview': 'Aperçu',
    'tour.b1.labels.dockSettings': 'Paramètres',
    'tour.b1.labels.eyebrow': 'État',
    'tour.b1.labels.manageModules': 'Gérer les modules',
    'tour.b1.labels.newCommand': 'Nouvelle commande',
    'tour.b1.labels.quickActions': 'Actions rapides',
    'tour.b1.labels.restart': 'Redémarrer',
    'tour.b1.labels.statusNote': "· rien ne vous attend pour l'instant",
    'tour.b1.labels.statusState': 'En ligne · dans le chat',
    'tour.b1.labels.titleHtml': 'Bonsoir, <i>streamer</i>',
    'tour.b1.labels.topCommands': 'Vos commandes favorites',
    'tour.b1.labels.uses': 'utilisations',
    'tour.b1.notes.0.text': 'Le dock est toute la navigation, sur tous les écrans: Aperçu, Commandes, Modules, Discord, Facturation, Paramètres.',
    'tour.b1.notes.1.text': 'État du bot: ItsBagelBot est-il dans votre chat en ce moment, avec le bouton pour corriger si besoin.',
    'tour.b1.notes.2.text': 'Actions rapides: les deux gestes les plus fréquents, à un clic.',
    'tour.b1.notes.3.text': 'Vos commandes les plus utilisées vivent ici aussi, à un clic de la modification.',
    'tour.b2.html': `
            <p>
                Tout ce que vous faites dans le tableau de bord est enregistré dès que vous confirmez:
                aucune étape de «déploiement», et les changements atteignent votre chat généralement en
                quelques secondes.
            </p>`,
    'tour.heading': 'Repérez-vous',
    'tour.note': 'Six arrêts dans le dock flottant. Vous passerez presque tout votre temps dans deux d’entre eux.',
};

export default strings;
