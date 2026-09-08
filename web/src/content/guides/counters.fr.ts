// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// The "counters" guide in fr. Copy only: the structure it fills is the
// English guide, counters.en.ts, and every id below names one string in it
// (lib/guides/translate.ts derives the ids from where the strings sit).
// Adding a language is this file translated, with no structure to get wrong.
import type { GuideStrings } from '../../lib/guides/translate';

const strings: GuideStrings = {
    'basics.b0.html': `
            <p>
                Un compteur est un nombre nommé dont le bot se souvient d'un stream à l'autre. Écrivez
                <code>&#123;counter:morts&#125;</code> dans la réponse d'une commande, et à chaque
                utilisation, le bot ajoute 1 à un compteur nommé «morts» et affiche le nouveau total
                directement dans sa réponse. Pas de tableur, pas de compte manuel, juste un jeton.
            </p>`,
    'basics.b1.caption': 'Deux utilisations de la même commande !mort, un seul compteur qui se souvient des deux.',
    'basics.b1.lines.0.name': 'alex',
    'basics.b1.lines.0.text': '!mort',
    'basics.b1.lines.1.text': 'votre_chaine est mort 47 fois.',
    'basics.b1.lines.2.name': 'maya_live',
    'basics.b1.lines.2.text': '!mort',
    'basics.b1.lines.3.text': 'votre_chaine est mort 48 fois.',
    'basics.b1.title': '#votre_chaine',
    'basics.b2.html': `
                <b>Les compteurs demandent les Points de fidélité</b>
                Les compteurs sont stockés par le <a href="/fr/guides/modules">module Points de fidélité</a>.
                Activez-le d'abord, sinon <code>&#123;counter:…&#125;</code> reste du texte littéral dans
                le chat.`,
    'basics.heading': 'Les compteurs: des nombres qui se souviennent',
    'basics.note': 'Un seul jeton, {counter:name}, ajoute 1 et affiche le total. Le compte survit à chaque stream.',
    'chat.b0.html': `
            <p>
                Vous et vos modérateurs pouvez créer et gérer des compteurs sans quitter le chat,
                tout sous <code>!counter</code>:
            </p>`,
    'chat.b1.head.0': 'Commande',
    'chat.b1.head.1': "Ce qu'elle fait",
    'chat.b1.rows.0.0': '<code>!counter create &lt;nom&gt; [portée]</code>',
    'chat.b1.rows.0.1': 'Crée un compteur. Le mot de portée est <code>user</code>, <code>command</code> ou <code>user+command</code>; omettez-le pour toute la chaîne.',
    'chat.b1.rows.1.0': '<code>!counter add &lt;nom&gt; [montant]</code>',
    'chat.b1.rows.1.1': 'Ajoute 1, ou le montant donné. Les nombres négatifs soustraient.',
    'chat.b1.rows.2.0': '<code>!counter set &lt;nom&gt; &lt;valeur&gt;</code>',
    'chat.b1.rows.2.1': 'Fixe le total directement.',
    'chat.b1.rows.3.0': '<code>!counter reset &lt;nom&gt;</code>',
    'chat.b1.rows.3.1': 'Le remet à zéro. Pour un compteur par spectateur ou par commande, ceci efface tous les comptes stockés.',
    'chat.b1.rows.4.0': '<code>!counter delete &lt;nom&gt;</code>',
    'chat.b1.rows.4.1': 'Supprime le compteur complètement.',
    'chat.b1.rows.5.0': '<code>!counter list</code>',
    'chat.b1.rows.5.1': 'Liste tous les compteurs de la chaîne et leur portée.',
    'chat.b1.rows.6.0': '<code>!counter &lt;nom&gt;</code>',
    'chat.b1.rows.6.1': 'Montre la valeur actuelle, comme la lire dans le chat.',
    'chat.b2.caption': 'Créer et faire avancer un compteur, du début à la fin, sans toucher au tableau de bord.',
    'chat.b2.lines.0.name': 'mod_sam',
    'chat.b2.lines.0.text': '!counter create morts',
    'chat.b2.lines.1.text': 'Compteur morts créé (channel).',
    'chat.b2.lines.2.name': 'mod_sam',
    'chat.b2.lines.2.text': '!counter add morts 5',
    'chat.b2.lines.3.text': 'Le compteur morts est maintenant à 5.',
    'chat.b2.title': '#votre_chaine',
    'chat.b3.html': `
                <b>Portée «par commande», depuis le chat</b>
                <code>!counter create hydratations command</code> crée un compteur groupé par commande
                en une ligne, prêt à glisser dans la réponse d'une commande, ou à lier à une récompense
                de points de chaîne, comme <code>&#123;counter:hydratations&#125;</code>.`,
    'chat.b4.labels.addOne': '!counter add morts',
    'chat.b4.labels.heading': 'Essayez: avancez le compteur',
    'chat.b4.labels.modName': 'mod_sam',
    'chat.b4.labels.modReplyTemplate': 'Le compteur morts est maintenant à {value}.',
    'chat.b4.labels.setTen': '!counter set morts 10',
    'chat.b4.labels.subOne': '!counter add morts -1',
    'chat.b4.labels.viewerCommand': '!mort',
    'chat.b4.labels.viewerName': 'crust',
    'chat.b4.labels.viewerReplyTemplate': 'Oh non, {value} morts jusqu’ici.',
    'chat.b4.labels.windowTitle': '#votre_chaine',
    'chat.b4.props.counterName': 'morts',
    'chat.heading': 'Gérer les compteurs depuis le chat',
    'chat.note': 'Créer, ajouter, fixer, réinitialiser, supprimer ou simplement demander, sans quitter le chat.',
    'dashboard.b0.html': `
            <p>
                La page <a href="https://dashboard.itsbagelbot.com/counters?lang=fr" target="_blank" rel="noopener noreferrer">Compteurs</a>
                liste tous les compteurs de votre chaîne et permet de les créer, ajuster et supprimer
                en cliquant plutôt qu'en tapant.
            </p>`,
    'dashboard.b1.caption': 'Panneau «nouveau compteur»: nommez-le, choisissez une portée, terminé. Les lignes existantes ont des boutons +/- pour les compteurs de toute la chaîne, ou un tableau de valeurs pour les autres.',
    'dashboard.b1.labels.cancel': 'Annuler',
    'dashboard.b1.labels.chip1': 'total chaîne',
    'dashboard.b1.labels.chip2': 'par utilisateur',
    'dashboard.b1.labels.chip3': 'par commande',
    'dashboard.b1.labels.chip4': 'par utilisateur + commande',
    'dashboard.b1.labels.countsValue': 'Un total pour la chaîne',
    'dashboard.b1.labels.create': 'Créer le compteur',
    'dashboard.b1.labels.fieldCounts': 'Comptage',
    'dashboard.b1.labels.fieldName': 'Nom',
    'dashboard.b1.labels.hint': 'Incrémentez-le depuis une réponse de commande avec &#123;counter:morts&#125;, depuis une récompense sur la page Channel Points, ou avec !counter add.',
    'dashboard.b1.labels.nameValue': 'morts',
    'dashboard.b1.labels.panelHead': 'Nouveau compteur',
    'dashboard.b1.notes.0.text': 'Nom: la même chaîne que vous écririez dans {counter:…}.',
    'dashboard.b1.notes.1.text': 'Portée: fixée dès la création.',
    'dashboard.b1.notes.2.text': "Un rappel en langage clair d'où les compteurs peuvent être avancés.",
    'dashboard.b2.html': `
            <h3>Lier un compteur à une récompense de points de chaîne</h3>
            <p>
                Chaque récompense <a href="/fr/guides/modules">Points de chaîne</a> peut garder son
                propre compteur, directement depuis l'éditeur de la récompense: tapez un nom de
                compteur (nouveau ou existant), choisissez sa portée, et chaque échange l'avance.
                Utilisez <code>&#123;counter&#125;</code> dans la réponse de chat de la récompense
                pour afficher le nouveau total.
            </p>`,
    'dashboard.b3.caption': "Le bloc compteur de l'éditeur de récompense: un interrupteur, un nom, et les mêmes quatre portées.",
    'dashboard.b3.labels.cancel': 'Annuler',
    'dashboard.b3.labels.check': 'Tenir un compteur',
    'dashboard.b3.labels.chip1': 'toute la chaîne',
    'dashboard.b3.labels.chip2': 'par viewer',
    'dashboard.b3.labels.chip3': 'par récompense',
    'dashboard.b3.labels.chip4': 'par viewer + récompense',
    'dashboard.b3.labels.fieldName': 'Nom du compteur',
    'dashboard.b3.labels.fieldScope': 'Ce que compte chaque échange',
    'dashboard.b3.labels.nameValue': 'hydratations',
    'dashboard.b3.labels.save': 'Enregistrer',
    'dashboard.b3.labels.scopeValue': 'Par viewer, par récompense',
    'dashboard.b3.notes.0.text': "Désactivé par défaut, la plupart des récompenses n'en ont pas besoin.",
    'dashboard.b3.notes.1.text': "Par spectateur + récompense est souvent le bon choix: chaque spectateur bâtit son propre compte d'échanges pour cette récompense.",
    'dashboard.b4.html': `
                <b>Essayez-le dans le constructeur</b>
                Le <a href="/fr/command-builder">constructeur de commandes</a> peut vous guider pour
                nommer un compteur et choisir sa portée, puis vous remet le jeton
                <code>&#123;counter:…&#125;</code> terminé.
                <a href="/fr/command-builder">Ouvrir le constructeur de commandes</a>`,
    'dashboard.heading': 'Le tableau de bord, et les récompenses de points de chaîne',
    'dashboard.note': 'La page Compteurs pour un contrôle direct, et comment une récompense s’y lie.',
    'meta.card.chips.0': '{counter:…}',
    'meta.card.chips.1': 'portées',
    'meta.card.chips.2': '!counter',
    'meta.card.chips.3': 'points de chaîne',
    'meta.card.description': 'Des nombres dont le bot se souvient pour toujours: le jeton {counter:name}, les quatre portées (chaîne, par spectateur, par commande, par spectateur + commande), et la liaison à une récompense de points de chaîne.',
    'meta.card.meta': '8 min · 4 étapes',
    'meta.card.title': 'Compteurs',
    'meta.description': "Comment fonctionnent les compteurs d'ItsBagelBot: le jeton {counter:name}, les quatre portées (toute la chaîne, par spectateur, par commande, par spectateur + commande), leur gestion avec !counter, et la liaison à une récompense de points de chaîne.",
    'meta.eyebrow': 'Guide',
    'meta.heading': 'Compteurs',
    'meta.lead': "Suivez tout ce qui arrive plus d'une fois: morts, câlins, échanges de récompenses. Choisissez comment ça se compte une seule fois, le bot se souvient pour toujours.",
    'meta.minutes': '8 min de lecture',
    'meta.title': 'Compteurs - Guides ItsBagelBot',
    'scopes.b0.html': `
            <p>
                Chaque compteur a exactement une <strong>portée</strong>, choisie au moment de sa
                création, et elle reste la même toute sa vie. La portée répond à une seule question:
                à qui appartient ce nombre?
            </p>`,
    'scopes.b1.head.0': 'Portée',
    'scopes.b1.head.1': 'Mot-clé',
    'scopes.b1.head.2': 'Ce qui est compté',
    'scopes.b1.rows.0.0': 'Toute la chaîne',
    'scopes.b1.rows.0.1': '<em>(par défaut)</em>',
    'scopes.b1.rows.0.2': 'Un seul total partagé, tout le monde ajoute au même nombre.',
    'scopes.b1.rows.1.0': 'Par spectateur',
    'scopes.b1.rows.1.1': '<code>user</code>',
    'scopes.b1.rows.1.2': 'Chaque spectateur a son propre nombre.',
    'scopes.b1.rows.2.0': 'Par commande ou récompense',
    'scopes.b1.rows.2.1': '<code>command</code>',
    'scopes.b1.rows.2.2': 'Un total partagé pour une seule commande ou récompense, tous les spectateurs réunis.',
    'scopes.b1.rows.3.0': 'Par spectateur + commande ou récompense',
    'scopes.b1.rows.3.1': '<code>user+command</code>',
    'scopes.b1.rows.3.2': 'Chaque spectateur a un nombre séparé pour chaque commande ou récompense.',
    'scopes.b10.html': `
                <b>Fixée à la création</b>
                La portée d'un compteur ne peut pas changer après coup. Mauvais choix? Supprimez-le
                (<code>!counter delete nom</code>, ou le tableau de bord) et recréez-le; les anciennes
                valeurs ne sont pas conservées.`,
    'scopes.b2.html': `
            <h3>Toute la chaîne</h3>
            <p>
                Tout le monde fait avancer le même nombre partagé. Parfait pour un décompte qui
                appartient au stream lui-même, pas à un spectateur en particulier, comme un compteur
                de morts.
            </p>`,
    'scopes.b3.caption': 'Peu importe qui tape !mort, le total de la chaîne continue de grimper.',
    'scopes.b3.lines.0.name': 'alex',
    'scopes.b3.lines.0.text': '!mort',
    'scopes.b3.lines.1.text': 'votre_chaine est mort 47 fois.',
    'scopes.b3.lines.2.name': 'sam',
    'scopes.b3.lines.2.text': '!mort',
    'scopes.b3.lines.3.text': 'votre_chaine est mort 48 fois.',
    'scopes.b3.title': '#votre_chaine',
    'scopes.b4.html': `
            <h3>Par spectateur</h3>
            <p>
                Chaque spectateur a un nombre privé, que personne d'autre ne touche. Idéal pour les
                séries personnelles: combien de câlins quelqu'un a donnés, combien de fois il vous a
                battu à un jeu.
            </p>`,
    'scopes.b5.caption': 'Même commande, deux spectateurs, deux comptes complètement séparés.',
    'scopes.b5.lines.0.name': 'maya_live',
    'scopes.b5.lines.0.text': '!calin',
    'scopes.b5.lines.1.text': 'maya_live a donné 12 câlins.',
    'scopes.b5.lines.2.name': 'alex',
    'scopes.b5.lines.2.text': '!calin',
    'scopes.b5.lines.3.text': 'alex a donné 1 câlin.',
    'scopes.b5.title': '#votre_chaine',
    'scopes.b6.html': `
            <h3>Par commande ou récompense</h3>
            <p>
                La portée la plus récente: un seul total partagé pour une commande ou une récompense
                de points de chaîne, où chaque spectateur ajoute au même nombre. Idéal pour «combien
                de fois cette récompense précise a-t-elle été échangée, au total?»
            </p>`,
    'scopes.b7.caption': 'Deux spectateurs différents, le total de la récompense continue quand même.',
    'scopes.b7.lines.0.text': 'points de chaîne · maya_live a échangé Hydrate!',
    'scopes.b7.lines.1.text': 'Hydrate! a été échangé 301 fois.',
    'scopes.b7.lines.2.text': 'points de chaîne · alex a échangé Hydrate!',
    'scopes.b7.lines.3.text': 'Hydrate! a été échangé 302 fois.',
    'scopes.b7.title': '#votre_chaine',
    'scopes.b8.html': `
            <h3>Par spectateur + commande ou récompense</h3>
            <p>
                Combine les deux: chaque spectateur a un nombre séparé pour chaque commande ou
                récompense qui partage ce compteur. Liez un même compteur à deux commandes et chaque
                spectateur se retrouve avec un compte par commande, pas un seul compte partagé pour
                les deux.
            </p>`,
    'scopes.b9.caption': "Le compte d'alex pour !calin et son compte pour !highfive ne se mélangent pas, même avec le même compteur.",
    'scopes.b9.lines.0.name': 'alex',
    'scopes.b9.lines.0.text': '!calin',
    'scopes.b9.lines.1.text': 'alex a fait 3 câlins.',
    'scopes.b9.lines.2.name': 'alex',
    'scopes.b9.lines.2.text': '!highfive',
    'scopes.b9.lines.3.text': 'alex a fait 1 highfive.',
    'scopes.b9.title': '#votre_chaine',
    'scopes.heading': 'Quatre façons de compter',
    'scopes.note': 'Toute la chaîne, par spectateur, par commande, ou les deux. Choisissez-en une à la création; elle reste fixée.',
};

export default strings;
