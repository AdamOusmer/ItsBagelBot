// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { GuideContent } from '../../lib/guides/types';

const guide: GuideContent = {
  slug: 'commands',
  meta: {
    title: 'Commandes et variables - Guides ItsBagelBot',
    description:
      "Maîtrisez les commandes personnalisées d'ItsBagelBot: toutes les variables supportées ({user}, {random}, {counter} et plus), réponses multilignes, actions de chat, délais et niveaux d'accès.",
    eyebrow: 'Guide',
    heading: 'Commandes et variables',
    lead: 'Des commandes qui saluent les gens par leur nom, lancent des dés et comptent vos victoires. Pas de code: juste des accolades.',
    minutes: '9 min de lecture',
    card: {
      title: 'Commandes et variables',
      description:
        'Créez des commandes qui saluent les gens par leur nom, lancent des dés et comptent vos victoires. Toutes les variables du bot, expliquées avec des exemples de chat.',
      meta: '9 min · 7 étapes',
      chips: ['{user}', '{random}', '{counter:…}', '!cmd'],
    },
  },
  sections: [
    {
      id: 'anatomy',
      heading: "L'anatomie d'une commande",
      note: 'Deux champs obligatoires, cinq réglages optionnels. Apprenez-les une fois, réutilisez-les toujours.',
      blocks: [
        {
          kind: 'prose',
          html: `
            <p>
                Une commande personnalisée, c'est une question que vos spectateurs peuvent poser
                (<code>!calin</code>) et la réponse que votre bot donne. L'éditeur compte sept champs;
                seuls le nom et la réponse sont obligatoires.
            </p>`,
        },
        {
          kind: 'dash',
          screen: 'CommandEditor',
          path: '/commands',
          caption: "L'éditeur de commande tel qu'il s'amarre à côté de votre liste, chaque champ annoté.",
          notes: [
            { n: 1, text: 'Nom: ce que les spectateurs tapent après le «!». Minuscules, un seul mot.' },
            { n: 2, text: 'Noms alternatifs: des déclencheurs supplémentaires pour la même commande (!calin et !calins peuvent ne faire qu’un).' },
            { n: 3, text: 'Réponse: ce que le bot dit. Jusqu’à 5 lignes; chaque ligne est son propre message de chat.' },
            { n: 4, text: 'La répétition du chat joue votre réponse avec des valeurs d’exemple avant d’enregistrer. Le constructeur de commandes a la même.' },
            { n: 5, text: 'Accès et délai: qui peut l’utiliser, et combien de secondes de silence suivent chaque utilisation.' },
            { n: 6, text: 'Uniquement pendant le direct met la commande en pause hors ligne; Active est l’interrupteur général.' },
            { n: 7, text: 'Source de données: insère une valeur récupérée d’une définition API enregistrée, au lieu d’une variable.' },
          ],
          labels: {
            panelHead: 'Modifier la commande',
            close: 'Fermer',
            fieldName: 'Nom',
            nameValue: 'calin',
            fieldAlts: 'Noms alternatifs',
            optional: '(optionnel)',
            altChipLabel: 'calins',
            remove: 'Retirer',
            fieldResponse: 'Réponse',
            responseHtml:
              '<span class="df-var">&#123;user&#125;</span> donne à <span class="df-var">&#123;touser&#125;</span> un gros câlin bagel 🥯',
            chip4: 'Source de données',
            chip4Tooltip: 'Insérer une valeur récupérée d’une définition API enregistrée',
            chatTag: 'Répétition du chat',
            viewerText: '!calin ferret_king',
            botHtml: '<mark>sesame_sam</mark> donne à <mark>ferret_king</mark> un gros câlin bagel 🥯',
            fieldAccess: 'Accès',
            accessValue: 'Tout le monde',
            fieldCooldown: 'Délai (s)',
            fieldRestrict: 'Restreindre à un ID utilisateur',
            restrictPlaceholder: "ID utilisateur Twitch. Lui seul peut l'exécuter",
            checkActive: 'Active',
            checkLive: 'Uniquement pendant le direct',
            cancel: 'Annuler',
            save: 'Enregistrer',
          },
        },
        {
          kind: 'callout',
          tone: 'tip',
          html: `
                <b>Astuce</b>
                La puce Source de données insère <code>&#123;urlfetch:name&#125;</code>, une valeur
                tirée d’une API que vous avez vous-même enregistrée. Le
                <a href="/fr/guides/data-sources">guide des sources de données</a> explique comment en
                enregistrer une première.`,
        },
      ],
    },
    {
      id: 'create',
      heading: 'Deux façons d’en créer une',
      note: "L'éditeur du tableau de bord, ou une seule ligne dans le chat. Les deux arrivent au même endroit.",
      blocks: [
        {
          kind: 'prose',
          html: `
            <h3>Depuis le tableau de bord</h3>
            <p>
                <a href="https://dashboard.itsbagelbot.com/commands" target="_blank" rel="noopener noreferrer">Commandes</a>,
                cliquez <strong>Nouvelle commande</strong>, remplissez le nom et la réponse, puis
                <strong>Créer</strong>. Elle répond dans le chat généralement en quelques secondes. La modification
                fonctionne pareil: cliquez une commande, changez-la, enregistrez.
            </p>
            <h3>Depuis le chat, avec !cmd</h3>
            <p>
                Vous et vos modérateurs pouvez aussi gérer les commandes sans quitter le chat, en plein
                stream. N'importe quel modérateur peut l'utiliser: <code>!cmd</code> ne demande pas la
                promotion modérateur principal qu'exigent certains autres built-ins.
            </p>`,
        },
        {
          kind: 'chat',
          title: '#votre_chaine',
          caption: '!cmd add, edit et remove sont réservés au diffuseur et aux modérateurs.',
          lines: [
            { who: 'mod', name: 'mod_sam', text: '!cmd add hype ON LÂCHE PAS 🎉' },
            { who: 'bot', text: '@mod_sam la commande hype a été ajoutée' },
            { who: 'viewer', name: 'maya_live', text: '!hype' },
            { who: 'bot', text: 'ON LÂCHE PAS 🎉' },
          ],
        },
        {
          kind: 'prose',
          html: `
            <ul>
                <li><code>!cmd add &lt;nom&gt; &lt;réponse&gt;</code> crée une commande.</li>
                <li><code>!cmd edit &lt;nom&gt; &lt;réponse&gt;</code> remplace sa réponse.</li>
                <li><code>!cmd remove &lt;nom&gt;</code> la supprime.</li>
            </ul>`,
        },
        {
          kind: 'callout',
          tone: 'tip',
          html: `
                <b>Astuce</b>
                Le chat est rapide pour les petites phrases; le tableau de bord montre les réglages
                supplémentaires (accès, délai, autres noms) et un aperçu en direct. Utilisez ce qui est
                le plus près de vos mains.`,
        },
      ],
    },
    {
      id: 'variables',
      heading: 'Les variables: les parties intelligentes',
      note: 'Les accolades sont des espaces réservés. Le bot les remplit au moment de répondre.',
      blocks: [
        {
          kind: 'prose',
          html: `
            <p>
                Écrivez <code>&#123;user&#125;</code> dans une réponse et le bot le remplace par le nom
                de la personne qui a lancé la commande. Cette seule idée alimente tout ce qui suit.
                Voici les variables de personnes et de lieux que toute commande personnalisée comprend:
            </p>`,
        },
        {
          kind: 'table',
          head: ['Variable', 'Devient', 'Exemple'],
          rows: [
            ['<code>&#123;user&#125;</code>', 'La personne qui a utilisé la commande. <code>&#123;sender&#125;</code> est un ancien alias, même valeur.', 'maya_live'],
            ['<code>&#123;touser&#125;</code>', "Le premier mot tapé après la commande, sans le «@». Si rien n'est tapé, le nom du spectateur lui-même. <code>&#123;target&#125;</code> est identique.", 'alex'],
            ['<code>&#123;args&#125;</code>', "Tout le texte tapé après la commande, en une seule chaîne. Vide si rien n'a été tapé.", 'bonne chance!'],
            ['<code>&#123;channel&#125;</code>', "Le nom d'affichage de votre chaîne.", 'votre_chaine'],
            ['<code>&#123;urlfetch:name&#125;</code>', 'Une valeur récupérée d’une API web enregistrée comme source de données. Expliqué dans le <a href="/fr/guides/data-sources">guide des sources de données</a>.', '22'],
          ],
        },
        {
          kind: 'chat',
          title: '#votre_chaine',
          caption: 'Une commande, deux phrases très différentes: !calin seul vs !calin alex.',
          lines: [
            { who: 'viewer', name: 'maya_live', text: '!calin' },
            { who: 'bot', text: 'maya_live donne à maya_live un gros câlin bagel 🥯' },
            { who: 'viewer', name: 'maya_live', text: '!calin alex' },
            { who: 'bot', text: 'maya_live donne à alex un gros câlin bagel 🥯' },
          ],
        },
        {
          kind: 'callout',
          tone: 'warn',
          html: `
                <b>Attention</b>
                Une variable que le bot ne reconnaît pas reste telle quelle, accolades comprises. Si le
                chat affiche un <code>&#123;quelquechose&#125;</code> littéral, vérifiez l'orthographe
                dans le tableau ci-dessus (ou composez la commande dans le
                <a href="/fr/command-builder">constructeur</a>, qui ne propose que de vraies variables).`,
        },
      ],
    },
    {
      id: 'dynamic',
      heading: 'Dés, choix et compteurs',
      note: 'Trois variables qui changent à chaque fois: nombres aléatoires, choix aléatoires et compteurs qui se souviennent.',
      blocks: [
        {
          kind: 'prose',
          html: `
            <h3>&#123;random&#125;: les dés</h3>
            <p>
                <code>&#123;random&#125;</code> devient un nombre entier de 1 à 100. Choisissez votre
                propre plage avec <code>&#123;random:1-6&#125;</code> (les deux bornes comptent).
            </p>
            <h3>&#123;choice:…&#125;: une pièce à autant de faces que vous voulez</h3>
            <p>
                <code>&#123;choice:oui,non,redemande plus tard&#125;</code> choisit une option de votre
                liste séparée par des virgules, différente à chaque fois.
            </p>
            <h3>&#123;counter:…&#125;: un nombre qui se souvient</h3>
            <p>
                <code>&#123;counter:chutes&#125;</code> ajoute 1 au compteur nommé «chutes» et affiche
                le nouveau total. Le compte survit aux streams: <code>!chute</code> peut suivre vos
                dégringolades toute l'année. Chaque compteur a une portée, choisie à sa création, qui
                détermine à qui appartient ce nombre: un seul total pour la chaîne, un par spectateur,
                un groupé par commande ou récompense, ou un par spectateur et par commande. Le
                <a href="/fr/guides/counters">guide des compteurs</a> présente les quatre avec des
                exemples.
            </p>`,
        },
        {
          kind: 'chat',
          title: '#votre_chaine',
          caption: 'Les trois en pleine action.',
          lines: [
            { who: 'viewer', name: 'maya_live', text: '!roll' },
            { who: 'bot', text: 'maya_live lance un 73 sur 100 🎲' },
            { who: 'viewer', name: 'alex', text: '!chute' },
            { who: 'bot', text: 'votre_chaine est tombé 128 fois. Un nouveau record de grâce.' },
          ],
        },
        {
          kind: 'callout',
          tone: 'tip',
          html: `
                <b>Les compteurs demandent les Points de fidélité</b>
                Les compteurs sont stockés par le <a href="/fr/guides/modules">module Points de fidélité</a>.
                Activez-le d'abord, sinon le <code>&#123;counter:…&#125;</code> reste du texte littéral.
                Vos modérateurs gèrent les comptes dans le chat avec <code>!counter set</code>,
                <code>!counter reset</code> et compagnie.`,
        },
        {
          kind: 'widget',
          name: 'Rehearsal',
          labels: {
            heading: 'Répétez une réponse',
            responseLabel: 'Réponse',
            whoLabel: 'Qui tape',
            argsLabel: 'Args (tapé après la commande)',
            outputLabel: 'Ce que le chat voit',
            pillLabel: 'récupérée à l’envoi',
            builderNote: 'Vous voulez la même répétition, avec création en un clic en plus ?',
            builderLinkText: 'Ouvrir le constructeur de commandes',
            builderHref: '/fr/command-builder',
            responseDefault: '{user} donne à {touser} un gros câlin bagel 🥯',
            outputDefault: 'sesame_sam donne à ferret_king un gros câlin bagel 🥯',
          },
        },
      ],
    },
    {
      id: 'multiline',
      heading: 'Plusieurs lignes et actions de chat',
      note: 'Chaque ligne devient son propre message. Une ligne peut aussi annoncer, faire un shoutout ou épingler.',
      blocks: [
        {
          kind: 'prose',
          html: `
            <p>
                Une réponse peut contenir jusqu'à <strong>5 lignes</strong>, et chaque ligne est envoyée
                comme son propre message, de haut en bas. Et une ligne qui <em>commence</em> par un de
                ces verbes devient une action Twitch native au lieu d'un message ordinaire:
            </p>`,
        },
        {
          kind: 'table',
          head: ['La ligne commence par', 'Ce qui se passe'],
          rows: [
            ['<code>/me</code>', 'Message «action» en italique, style IRC classique.'],
            ['<code>/announce</code>', 'Une annonce Twitch mise en évidence. Variantes de couleur: <code>/announceblue</code>, <code>/announcegreen</code>, <code>/announceorange</code>, <code>/announcepurple</code>.'],
            ['<code>/shoutout</code>', 'Un shoutout Twitch natif vers le premier nom de la ligne.'],
            ['<code>/pin</code>', "Envoie le message et l'épingle jusqu'à la fin du stream."],
          ],
        },
        {
          kind: 'chat',
          title: '#votre_chaine',
          caption: 'Une commande à deux lignes: une annonce, puis un message normal.',
          lines: [
            { who: 'viewer', name: 'maya_live', text: '!concours' },
            { who: 'system', text: 'annonce · Le concours est LANCÉ! Tapez !enter pour participer.' },
            { who: 'bot', text: "Gagnant tiré à l'heure pile. Bonne chance! 🍀" },
          ],
        },
        {
          kind: 'callout',
          tone: 'tip',
          html: `
                <b>Sécurité intégrée</b>
                Le texte fourni par les spectateurs (<code>&#123;args&#125;</code>,
                <code>&#123;touser&#125;</code>) est nettoyé avant d'atterrir dans le message: personne
                ne peut glisser un <code>/ban</code> ou un <code>/timeout</code> dans la sortie de votre
                commande.`,
        },
      ],
    },
    {
      id: 'rules',
      heading: 'Les règles du jeu',
      note: "Noms, limites, délais et niveaux d'accès. Tout ce que l'éditeur accepte et refuse.",
      blocks: [
        {
          kind: 'prose',
          html: `<p>L'éditeur vérifie tout cela pour vous et dit ce qui cloche en mots simples. Pour référence:</p>`,
        },
        {
          kind: 'table',
          head: ['Champ', 'La règle'],
          rows: [
            ['Nom', "1 à 64 caractères, sans espaces, et sans le «!» (le chat l'ajoute). Stocké en minuscules: <code>!Calin</code> et <code>!calin</code> sont la même commande."],
            ['Noms alternatifs', "Jusqu'à 25, chacun suivant les mêmes règles que le nom."],
            ['Réponse', "Jusqu'à 5 lignes de 500 caractères chacune (un message de chat par ligne)."],
            ['Délai', '0 à 86400 secondes. Il est partagé par tout le chat: après une utilisation, tout le monde attend.'],
            ['Accès', "Rang minimal, dans l'ordre: tout le monde, abonnés, VIP, modérateurs, modérateurs principaux, diffuseur. Chaque niveau inclut ceux au-dessus."],
            ['Restreindre à une personne', "Verrouillez une commande sur un seul compte Twitch; cela remplace entièrement le niveau d'accès. Parfait pour la commande personnelle d'un ami."],
          ],
        },
      ],
    },
    {
      id: 'builder',
      heading: 'Évitez la saisie: le constructeur',
      note: 'Composez une commande en cliquant, prévisualisez-la en direct, envoyez-la dans votre tableau de bord.',
      blocks: [
        {
          kind: 'prose',
          html: `
            <p>
                Tout ce guide est intégré au
                <a href="/fr/command-builder">constructeur de commandes</a>: choisissez les variables
                dans une liste expliquée au lieu de les mémoriser, regardez une répétition de chat en
                direct pendant que vous tapez, et quand tout sonne juste, envoyez la commande à votre
                tableau de bord, où une confirmation la crée en un clic. C'est le chemin le plus court entre
                l'idée et la commande qui fonctionne.
            </p>`,
        },
        {
          kind: 'callout',
          tone: 'tip',
          html: `
                <b>Essayez maintenant</b>
                <a href="/fr/command-builder">Ouvrir le constructeur de commandes</a>`,
        },
      ],
    },
  ],
};

export default guide;
