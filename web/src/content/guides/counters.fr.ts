// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { GuideContent } from '../../lib/guides/types';

const guide: GuideContent = {
  slug: 'counters',
  meta: {
    title: 'Compteurs - Guides ItsBagelBot',
    description:
      "Comment fonctionnent les compteurs d'ItsBagelBot: le jeton {counter:name}, les quatre portées (toute la chaîne, par spectateur, par commande, par spectateur + commande), leur gestion avec !counter, et la liaison à une récompense de points de chaîne.",
    eyebrow: 'Guide',
    heading: 'Compteurs',
    lead: "Suivez tout ce qui arrive plus d'une fois: morts, câlins, échanges de récompenses. Choisissez comment ça se compte une seule fois, le bot se souvient pour toujours.",
    minutes: '8 min de lecture',
    card: {
      title: 'Compteurs',
      description:
        'Des nombres dont le bot se souvient pour toujours: le jeton {counter:name}, les quatre portées (chaîne, par spectateur, par commande, par spectateur + commande), et la liaison à une récompense de points de chaîne.',
      meta: '8 min · 4 étapes',
      chips: ['{counter:…}', 'portées', '!counter', 'points de chaîne'],
    },
  },
  sections: [
    {
      id: 'basics',
      heading: 'Les compteurs: des nombres qui se souviennent',
      note: 'Un seul jeton, {counter:name}, ajoute 1 et affiche le total. Le compte survit à chaque stream.',
      blocks: [
        {
          kind: 'prose',
          html: `
            <p>
                Un compteur est un nombre nommé dont le bot se souvient d'un stream à l'autre. Écrivez
                <code>&#123;counter:morts&#125;</code> dans la réponse d'une commande, et à chaque
                utilisation, le bot ajoute 1 à un compteur nommé «morts» et affiche le nouveau total
                directement dans sa réponse. Pas de tableur, pas de compte manuel, juste un jeton.
            </p>`,
        },
        {
          kind: 'chat',
          title: '#votre_chaine',
          caption: 'Deux utilisations de la même commande !mort, un seul compteur qui se souvient des deux.',
          lines: [
            { who: 'viewer', name: 'alex', text: '!mort' },
            { who: 'bot', text: 'votre_chaine est mort 47 fois.' },
            { who: 'viewer', name: 'maya_live', text: '!mort' },
            { who: 'bot', text: 'votre_chaine est mort 48 fois.' },
          ],
        },
        {
          kind: 'callout',
          tone: 'tip',
          html: `
                <b>Les compteurs demandent les Points de fidélité</b>
                Les compteurs sont stockés par le <a href="/fr/guides/modules">module Points de fidélité</a>.
                Activez-le d'abord, sinon <code>&#123;counter:…&#125;</code> reste du texte littéral dans
                le chat.`,
        },
      ],
    },
    {
      id: 'scopes',
      heading: 'Quatre façons de compter',
      note: 'Toute la chaîne, par spectateur, par commande, ou les deux. Choisissez-en une à la création; elle reste fixée.',
      blocks: [
        {
          kind: 'prose',
          html: `
            <p>
                Chaque compteur a exactement une <strong>portée</strong>, choisie au moment de sa
                création, et elle reste la même toute sa vie. La portée répond à une seule question:
                à qui appartient ce nombre?
            </p>`,
        },
        {
          kind: 'table',
          head: ['Portée', 'Mot-clé', 'Ce qui est compté'],
          rows: [
            ['Toute la chaîne', '<em>(par défaut)</em>', 'Un seul total partagé, tout le monde ajoute au même nombre.'],
            ['Par spectateur', '<code>user</code>', 'Chaque spectateur a son propre nombre.'],
            ['Par commande ou récompense', '<code>command</code>', 'Un total partagé pour une seule commande ou récompense, tous les spectateurs réunis.'],
            ['Par spectateur + commande ou récompense', '<code>user+command</code>', 'Chaque spectateur a un nombre séparé pour chaque commande ou récompense.'],
          ],
        },
        {
          kind: 'prose',
          html: `
            <h3>Toute la chaîne</h3>
            <p>
                Tout le monde fait avancer le même nombre partagé. Parfait pour un décompte qui
                appartient au stream lui-même, pas à un spectateur en particulier, comme un compteur
                de morts.
            </p>`,
        },
        {
          kind: 'chat',
          title: '#votre_chaine',
          caption: 'Peu importe qui tape !mort, le total de la chaîne continue de grimper.',
          lines: [
            { who: 'viewer', name: 'alex', text: '!mort' },
            { who: 'bot', text: 'votre_chaine est mort 47 fois.' },
            { who: 'viewer', name: 'sam', text: '!mort' },
            { who: 'bot', text: 'votre_chaine est mort 48 fois.' },
          ],
        },
        {
          kind: 'prose',
          html: `
            <h3>Par spectateur</h3>
            <p>
                Chaque spectateur a un nombre privé, que personne d'autre ne touche. Idéal pour les
                séries personnelles: combien de câlins quelqu'un a donnés, combien de fois il vous a
                battu à un jeu.
            </p>`,
        },
        {
          kind: 'chat',
          title: '#votre_chaine',
          caption: 'Même commande, deux spectateurs, deux comptes complètement séparés.',
          lines: [
            { who: 'viewer', name: 'maya_live', text: '!calin' },
            { who: 'bot', text: 'maya_live a donné 12 câlins.' },
            { who: 'viewer', name: 'alex', text: '!calin' },
            { who: 'bot', text: 'alex a donné 1 câlin.' },
          ],
        },
        {
          kind: 'prose',
          html: `
            <h3>Par commande ou récompense</h3>
            <p>
                La portée la plus récente: un seul total partagé pour une commande ou une récompense
                de points de chaîne, où chaque spectateur ajoute au même nombre. Idéal pour «combien
                de fois cette récompense précise a-t-elle été échangée, au total?»
            </p>`,
        },
        {
          kind: 'chat',
          title: '#votre_chaine',
          caption: 'Deux spectateurs différents, le total de la récompense continue quand même.',
          lines: [
            { who: 'system', text: 'points de chaîne · maya_live a échangé Hydrate!' },
            { who: 'bot', text: 'Hydrate! a été échangé 301 fois.' },
            { who: 'system', text: 'points de chaîne · alex a échangé Hydrate!' },
            { who: 'bot', text: 'Hydrate! a été échangé 302 fois.' },
          ],
        },
        {
          kind: 'prose',
          html: `
            <h3>Par spectateur + commande ou récompense</h3>
            <p>
                Combine les deux: chaque spectateur a un nombre séparé pour chaque commande ou
                récompense qui partage ce compteur. Liez un même compteur à deux commandes et chaque
                spectateur se retrouve avec un compte par commande, pas un seul compte partagé pour
                les deux.
            </p>`,
        },
        {
          kind: 'chat',
          title: '#votre_chaine',
          caption: "Le compte d'alex pour !calin et son compte pour !highfive ne se mélangent pas, même avec le même compteur.",
          lines: [
            { who: 'viewer', name: 'alex', text: '!calin' },
            { who: 'bot', text: 'alex a fait 3 câlins.' },
            { who: 'viewer', name: 'alex', text: '!highfive' },
            { who: 'bot', text: 'alex a fait 1 highfive.' },
          ],
        },
        {
          kind: 'callout',
          tone: 'warn',
          html: `
                <b>Fixée à la création</b>
                La portée d'un compteur ne peut pas changer après coup. Mauvais choix? Supprimez-le
                (<code>!counter delete nom</code>, ou le tableau de bord) et recréez-le; les anciennes
                valeurs ne sont pas conservées.`,
        },
      ],
    },
    {
      id: 'chat',
      heading: 'Gérer les compteurs depuis le chat',
      note: 'Créer, ajouter, fixer, réinitialiser, supprimer ou simplement demander, sans quitter le chat.',
      blocks: [
        {
          kind: 'prose',
          html: `
            <p>
                Vous et vos modérateurs pouvez créer et gérer des compteurs sans quitter le chat,
                tout sous <code>!counter</code>:
            </p>`,
        },
        {
          kind: 'table',
          head: ['Commande', "Ce qu'elle fait"],
          rows: [
            ['<code>!counter create &lt;nom&gt; [portée]</code>', 'Crée un compteur. Le mot de portée est <code>user</code>, <code>command</code> ou <code>user+command</code>; omettez-le pour toute la chaîne.'],
            ['<code>!counter add &lt;nom&gt; [montant]</code>', 'Ajoute 1, ou le montant donné. Les nombres négatifs soustraient.'],
            ['<code>!counter set &lt;nom&gt; &lt;valeur&gt;</code>', 'Fixe le total directement.'],
            ['<code>!counter reset &lt;nom&gt;</code>', 'Le remet à zéro. Pour un compteur par spectateur ou par commande, ceci efface tous les comptes stockés.'],
            ['<code>!counter delete &lt;nom&gt;</code>', 'Supprime le compteur complètement.'],
            ['<code>!counter list</code>', 'Liste tous les compteurs de la chaîne et leur portée.'],
            ['<code>!counter &lt;nom&gt;</code>', 'Montre la valeur actuelle, comme la lire dans le chat.'],
          ],
        },
        {
          kind: 'chat',
          title: '#votre_chaine',
          caption: 'Créer et faire avancer un compteur, du début à la fin, sans toucher au tableau de bord.',
          lines: [
            { who: 'mod', name: 'mod_sam', text: '!counter create morts' },
            { who: 'bot', text: 'Compteur morts créé (channel).' },
            { who: 'mod', name: 'mod_sam', text: '!counter add morts 5' },
            { who: 'bot', text: 'Le compteur morts est maintenant à 5.' },
          ],
        },
        {
          kind: 'callout',
          tone: 'tip',
          html: `
                <b>Portée «par commande», depuis le chat</b>
                <code>!counter create hydratations command</code> crée un compteur groupé par commande
                en une ligne, prêt à glisser dans la réponse d'une commande, ou à lier à une récompense
                de points de chaîne, comme <code>&#123;counter:hydratations&#125;</code>.`,
        },
        {
          kind: 'widget',
          name: 'CounterPlay',
          labels: {
            heading: "Essayez: avancez le compteur",
            windowTitle: '#votre_chaine',
            modName: 'mod_sam',
            addOne: '!counter add morts',
            subOne: '!counter add morts -1',
            setTen: '!counter set morts 10',
            viewerName: 'crust',
            viewerCommand: '!mort',
            viewerReplyTemplate: 'Oh non, {value} morts jusqu’ici.',
            modReplyTemplate: 'Le compteur morts est maintenant à {value}.',
          },
          props: { counterName: 'morts', start: 12 },
        },
      ],
    },
    {
      id: 'dashboard',
      heading: 'Le tableau de bord, et les récompenses de points de chaîne',
      note: 'La page Compteurs pour un contrôle direct, et comment une récompense s’y lie.',
      blocks: [
        {
          kind: 'prose',
          html: `
            <p>
                La page <a href="https://dashboard.itsbagelbot.com/counters?lang=fr" target="_blank" rel="noopener noreferrer">Compteurs</a>
                liste tous les compteurs de votre chaîne et permet de les créer, ajuster et supprimer
                en cliquant plutôt qu'en tapant.
            </p>`,
        },
        {
          kind: 'dash',
          screen: 'NewCounter',
          path: '/counters',
          caption: 'Panneau «nouveau compteur»: nommez-le, choisissez une portée, terminé. Les lignes existantes ont des boutons +/- pour les compteurs de toute la chaîne, ou un tableau de valeurs pour les autres.',
          notes: [
            { n: 1, text: 'Nom: la même chaîne que vous écririez dans {counter:…}.' },
            { n: 2, text: 'Portée: fixée dès la création.' },
            { n: 3, text: "Un rappel en langage clair d'où les compteurs peuvent être avancés." },
          ],
          labels: {
            panelHead: 'Nouveau compteur',
            fieldName: 'Nom',
            nameValue: 'morts',
            fieldCounts: 'Comptage',
            countsValue: 'Un total pour la chaîne',
            chip1: 'total chaîne',
            chip2: 'par utilisateur',
            chip3: 'par commande',
            chip4: 'par utilisateur + commande',
            hint: 'Incrémentez-le depuis une réponse de commande avec &#123;counter:morts&#125;, depuis une récompense sur la page Channel Points, ou avec !counter add.',
            cancel: 'Annuler',
            create: 'Créer le compteur',
          },
        },
        {
          kind: 'prose',
          html: `
            <h3>Lier un compteur à une récompense de points de chaîne</h3>
            <p>
                Chaque récompense <a href="/fr/guides/modules">Points de chaîne</a> peut garder son
                propre compteur, directement depuis l'éditeur de la récompense: tapez un nom de
                compteur (nouveau ou existant), choisissez sa portée, et chaque échange l'avance.
                Utilisez <code>&#123;counter&#125;</code> dans la réponse de chat de la récompense
                pour afficher le nouveau total.
            </p>`,
        },
        {
          kind: 'dash',
          screen: 'RewardCounter',
          path: '/channelpoints',
          caption: "Le bloc compteur de l'éditeur de récompense: un interrupteur, un nom, et les mêmes quatre portées.",
          notes: [
            { n: 1, text: "Désactivé par défaut, la plupart des récompenses n'en ont pas besoin." },
            { n: 2, text: "Par spectateur + récompense est souvent le bon choix: chaque spectateur bâtit son propre compte d'échanges pour cette récompense." },
          ],
          labels: {
            check: 'Tenir un compteur',
            fieldName: 'Nom du compteur',
            nameValue: 'hydratations',
            fieldScope: 'Ce que compte chaque échange',
            scopeValue: 'Par viewer, par récompense',
            chip1: 'toute la chaîne',
            chip2: 'par viewer',
            chip3: 'par récompense',
            chip4: 'par viewer + récompense',
            cancel: 'Annuler',
            save: 'Enregistrer',
          },
        },
        {
          kind: 'callout',
          tone: 'tip',
          html: `
                <b>Essayez-le dans le constructeur</b>
                Le <a href="/fr/command-builder">constructeur de commandes</a> peut vous guider pour
                nommer un compteur et choisir sa portée, puis vous remet le jeton
                <code>&#123;counter:…&#125;</code> terminé.
                <a href="/fr/command-builder">Ouvrir le constructeur de commandes</a>`,
        },
      ],
    },
  ],
};

export default guide;
