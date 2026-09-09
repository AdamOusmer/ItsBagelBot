// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// The "commands" guide in fr. Copy only: the structure it fills is the
// English guide, commands.en.ts, and every id below names one string in it
// (lib/guides/translate.ts derives the ids from where the strings sit).
// Adding a language is this file translated, with no structure to get wrong.
import type { GuideStrings } from '../../lib/guides/translate';

const strings: GuideStrings = {
    'anatomy.b0.html': `
            <p>
                Une commande personnalisée, c'est une question que vos spectateurs peuvent poser
                (<code>!calin</code>) et la réponse que votre bot donne. L'éditeur compte sept champs;
                seuls le nom et la réponse sont obligatoires.
            </p>`,
    'anatomy.b1.caption': "L'éditeur de commande tel qu'il s'amarre à côté de votre liste, chaque champ annoté.",
    'anatomy.b1.labels.accessValue': 'Tout le monde',
    'anatomy.b1.labels.altChipLabel': 'calins',
    'anatomy.b1.labels.botHtml': '<mark>sesame_sam</mark> donne à <mark>ferret_king</mark> un gros câlin bagel 🥯',
    'anatomy.b1.labels.cancel': 'Annuler',
    'anatomy.b1.labels.chatTag': 'Répétition du chat',
    'anatomy.b1.labels.checkActive': 'Active',
    'anatomy.b1.labels.checkLive': 'Uniquement pendant le direct',
    'anatomy.b1.labels.chip4': 'Source de données',
    'anatomy.b1.labels.chip4Tooltip': 'Insérer une valeur récupérée d’une définition API enregistrée',
    'anatomy.b1.labels.close': 'Fermer',
    'anatomy.b1.labels.fieldAccess': 'Accès',
    'anatomy.b1.labels.fieldAlts': 'Noms alternatifs',
    'anatomy.b1.labels.fieldCooldown': 'Délai (s)',
    'anatomy.b1.labels.fieldName': 'Nom',
    'anatomy.b1.labels.fieldResponse': 'Réponse',
    'anatomy.b1.labels.fieldRestrict': 'Restreindre à un ID utilisateur',
    'anatomy.b1.labels.nameValue': 'calin',
    'anatomy.b1.labels.optional': '(optionnel)',
    'anatomy.b1.labels.panelHead': 'Modifier la commande',
    'anatomy.b1.labels.remove': 'Retirer',
    'anatomy.b1.labels.responseHtml': '<span class="df-var">&#123;user&#125;</span> donne à <span class="df-var">&#123;touser&#125;</span> un gros câlin bagel 🥯',
    'anatomy.b1.labels.restrictPlaceholder': "ID utilisateur Twitch. Lui seul peut l'exécuter",
    'anatomy.b1.labels.save': 'Enregistrer',
    'anatomy.b1.labels.viewerText': '!calin ferret_king',
    'anatomy.b1.notes.0.text': 'Nom: ce que les spectateurs tapent après le «!». Minuscules, un seul mot.',
    'anatomy.b1.notes.1.text': 'Noms alternatifs: des déclencheurs supplémentaires pour la même commande (!calin et !calins peuvent ne faire qu’un).',
    'anatomy.b1.notes.2.text': 'Réponse: ce que le bot dit. Jusqu’à 5 lignes; chaque ligne est son propre message de chat.',
    'anatomy.b1.notes.3.text': 'La répétition du chat joue votre réponse avec des valeurs d’exemple avant d’enregistrer. Le constructeur de commandes a la même.',
    'anatomy.b1.notes.4.text': 'Accès et délai: qui peut l’utiliser, et combien de secondes de silence suivent chaque utilisation.',
    'anatomy.b1.notes.5.text': 'Uniquement pendant le direct met la commande en pause hors ligne; Active est l’interrupteur général.',
    'anatomy.b1.notes.6.text': 'Source de données: insère une valeur récupérée d’une définition API enregistrée, au lieu d’une variable.',
    'anatomy.b2.html': `
                <b>Astuce</b>
                La puce Source de données insère <code>&#123;urlfetch:name&#125;</code>, une valeur
                tirée d’une API que vous avez vous-même enregistrée. Le
                <a href="/fr/guides/data-sources">guide des sources de données</a> explique comment en
                enregistrer une première.`,
    'anatomy.heading': "L'anatomie d'une commande",
    'anatomy.note': 'Deux champs obligatoires, cinq réglages optionnels. Apprenez-les une fois, réutilisez-les toujours.',
    'builder.b0.html': `
            <p>
                Tout ce guide est intégré au
                <a href="/fr/command-builder">constructeur de commandes</a>: choisissez les variables
                dans une liste expliquée au lieu de les mémoriser, regardez une répétition de chat en
                direct pendant que vous tapez, et quand tout sonne juste, envoyez la commande à votre
                tableau de bord, où une confirmation la crée en un clic. C'est le chemin le plus court entre
                l'idée et la commande qui fonctionne.
            </p>`,
    'builder.b1.html': `
                <b>Essayez maintenant</b>
                <a href="/fr/command-builder">Ouvrir le constructeur de commandes</a>`,
    'builder.heading': 'Évitez la saisie: le constructeur',
    'builder.note': 'Composez une commande en cliquant, prévisualisez-la en direct, envoyez-la dans votre tableau de bord.',
    'create.b0.html': `
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
    'create.b1.caption': '!cmd add, edit et remove sont réservés au diffuseur et aux modérateurs.',
    'create.b1.lines.0.name': 'mod_sam',
    'create.b1.lines.0.text': '!cmd add hype ON LÂCHE PAS 🎉',
    'create.b1.lines.1.text': '@mod_sam la commande hype a été ajoutée',
    'create.b1.lines.2.name': 'maya_live',
    'create.b1.lines.2.text': '!hype',
    'create.b1.lines.3.text': 'ON LÂCHE PAS 🎉',
    'create.b1.title': '#votre_chaine',
    'create.b2.html': `
            <ul>
                <li><code>!cmd add &lt;nom&gt; &lt;réponse&gt;</code> crée une commande.</li>
                <li><code>!cmd edit &lt;nom&gt; &lt;réponse&gt;</code> remplace sa réponse.</li>
                <li><code>!cmd remove &lt;nom&gt;</code> la supprime.</li>
            </ul>`,
    'create.b3.html': `
                <b>Astuce</b>
                Le chat est rapide pour les petites phrases; le tableau de bord montre les réglages
                supplémentaires (accès, délai, autres noms) et un aperçu en direct. Utilisez ce qui est
                le plus près de vos mains.`,
    'create.heading': 'Deux façons d’en créer une',
    'create.note': "L'éditeur du tableau de bord, ou une seule ligne dans le chat. Les deux arrivent au même endroit.",
    'dynamic.b0.html': `
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
    'dynamic.b1.caption': 'Les trois en pleine action.',
    'dynamic.b1.lines.0.name': 'maya_live',
    'dynamic.b1.lines.0.text': '!roll',
    'dynamic.b1.lines.1.text': 'maya_live lance un 73 sur 100 🎲',
    'dynamic.b1.lines.2.name': 'alex',
    'dynamic.b1.lines.2.text': '!chute',
    'dynamic.b1.lines.3.text': 'votre_chaine est tombé 128 fois. Un nouveau record de grâce.',
    'dynamic.b1.title': '#votre_chaine',
    'dynamic.b2.html': `
                <b>Les compteurs demandent les Points de fidélité</b>
                Les compteurs sont stockés par le <a href="/fr/guides/modules">module Points de fidélité</a>.
                Activez-le d'abord, sinon le <code>&#123;counter:…&#125;</code> reste du texte littéral.
                Vos modérateurs gèrent les comptes dans le chat avec <code>!counter set</code>,
                <code>!counter reset</code> et compagnie.`,
    'dynamic.b3.labels.argsLabel': 'Args (tapé après la commande)',
    'dynamic.b3.labels.builderHref': '/fr/command-builder',
    'dynamic.b3.labels.builderLinkText': 'Ouvrir le constructeur de commandes',
    'dynamic.b3.labels.builderNote': 'Vous voulez la même répétition, avec création en un clic en plus ?',
    'dynamic.b3.labels.heading': 'Répétez une réponse',
    'dynamic.b3.labels.outputDefault': 'sesame_sam donne à ferret_king un gros câlin bagel 🥯',
    'dynamic.b3.labels.outputLabel': 'Ce que le chat voit',
    'dynamic.b3.labels.pillLabel': 'récupérée à l’envoi',
    'dynamic.b3.labels.responseDefault': '{user} donne à {touser} un gros câlin bagel 🥯',
    'dynamic.b3.labels.responseLabel': 'Réponse',
    'dynamic.b3.labels.whoLabel': 'Qui tape',
    'dynamic.heading': 'Dés, choix et compteurs',
    'dynamic.note': 'Trois variables qui changent à chaque fois: nombres aléatoires, choix aléatoires et compteurs qui se souviennent.',
    'meta.card.chips.0': '{user}',
    'meta.card.chips.1': '{random}',
    'meta.card.chips.2': '{counter:…}',
    'meta.card.chips.3': '!cmd',
    'meta.card.description': 'Créez des commandes qui saluent les gens par leur nom, lancent des dés et comptent vos victoires. Toutes les variables du bot, expliquées avec des exemples de chat.',
    'meta.card.meta': '12 min · 10 étapes',
    'meta.card.title': 'Commandes et variables',
    'meta.description': "Maîtrisez les commandes personnalisées d'ItsBagelBot: toutes les variables supportées ({user}, {random}, {counter} et plus), réponses multilignes, actions de chat, délais et niveaux d'accès.",
    'meta.eyebrow': 'Guide',
    'meta.heading': 'Commandes et variables',
    'meta.lead': 'Des commandes qui saluent les gens par leur nom, lancent des dés et comptent vos victoires. Pas de code: juste des accolades.',
    'meta.minutes': '12 min de lecture',
    'meta.title': 'Commandes et variables - Guides ItsBagelBot',
    'fallbacks.b0.html': `
            <p>
                <code>Va voir &#123;1&#125;!</code> se lit mal quand personne n’a tapé de nom: le chat
                voit <em>Va voir !</em>. Ajoutez une barre verticale et du texte dans les accolades, et
                ce texte prend le relais dès que la variable revient vide:
                <code>Va voir &#123;1|tout le monde&#125;!</code>.
            </p>
            <p>
                Ça marche avec n’importe quelle variable, y compris une qui <em>semble</em> remplie sans
                l’être: une <a href="/fr/guides/data-sources">source de données</a> qui a répondu vide
                (<code>&#123;urlfetch:temp|hors ligne&#125;</code>), ou un spectateur mentionné que
                personne n’a nommé (<code>&#123;touser|le chat&#125;</code>). Le texte après la barre est
                du texte simple, pas une autre variable.
            </p>`,
    'fallbacks.b1.caption': 'La même commande, avec et sans nom après elle.',
    'fallbacks.b1.lines.0.name': 'maya_live',
    'fallbacks.b1.lines.0.text': '!shoutout alex',
    'fallbacks.b1.lines.1.text': 'Allez donner de l’amour à alex 💛',
    'fallbacks.b1.lines.2.name': 'maya_live',
    'fallbacks.b1.lines.2.text': '!shoutout',
    'fallbacks.b1.lines.3.text': 'Allez donner de l’amour à tout le monde 💛',
    'fallbacks.b1.title': '#votre_chaine',
    'fallbacks.b2.html': `
                <b>Une valeur par défaut ne rattrape pas une faute de frappe</b>
                La barre verticale ne couvre qu’une valeur <em>vide</em>. Une variable que le bot ne
                reconnaît pas reste telle quelle, barre comprise: écrivez
                <code>&#123;touser|le chat&#125;</code> et le chat voit un nom, écrivez
                <code>&#123;tousr|le chat&#125;</code> et le chat voit
                <code>&#123;tousr|le chat&#125;</code>. C’est voulu: une faute reste visible au lieu de
                se cacher derrière sa propre valeur par défaut pour toujours.`,
    'fallbacks.heading': 'Valeurs par défaut, quand un mot manque',
    'fallbacks.note': 'Une barre verticale dans une variable lui donne quelque chose à dire quand elle revient vide.',
    'multiline.b0.html': `
            <p>
                Une réponse peut contenir jusqu'à <strong>5 lignes</strong>, et chaque ligne est envoyée
                comme son propre message, de haut en bas. Et une ligne qui <em>commence</em> par un de
                ces verbes devient une action Twitch native au lieu d'un message ordinaire:
            </p>`,
    'multiline.b1.head.0': 'La ligne commence par',
    'multiline.b1.head.1': 'Ce qui se passe',
    'multiline.b1.rows.0.0': '<code>/me</code>',
    'multiline.b1.rows.0.1': 'Message «action» en italique, style IRC classique.',
    'multiline.b1.rows.1.0': '<code>/announce</code>',
    'multiline.b1.rows.1.1': 'Une annonce Twitch mise en évidence. Variantes de couleur: <code>/announceblue</code>, <code>/announcegreen</code>, <code>/announceorange</code>, <code>/announcepurple</code>.',
    'multiline.b1.rows.2.0': '<code>/shoutout</code>',
    'multiline.b1.rows.2.1': 'Un shoutout Twitch natif vers le premier nom de la ligne.',
    'multiline.b1.rows.3.0': '<code>/pin</code>',
    'multiline.b1.rows.3.1': "Envoie le message et l'épingle jusqu'à la fin du stream.",
    'multiline.b2.caption': 'Une commande à deux lignes: une annonce, puis un message normal.',
    'multiline.b2.lines.0.name': 'maya_live',
    'multiline.b2.lines.0.text': '!concours',
    'multiline.b2.lines.1.text': 'annonce · Le concours est LANCÉ! Tapez !enter pour participer.',
    'multiline.b2.lines.2.text': "Gagnant tiré à l'heure pile. Bonne chance! 🍀",
    'multiline.b2.title': '#votre_chaine',
    'multiline.b3.html': `
                <b>Sécurité intégrée</b>
                Le texte fourni par les spectateurs (<code>&#123;args&#125;</code>,
                <code>&#123;touser&#125;</code>) est nettoyé avant d'atterrir dans le message: personne
                ne peut glisser un <code>/ban</code> ou un <code>/timeout</code> dans la sortie de votre
                commande.`,
    'multiline.heading': 'Plusieurs lignes et actions de chat',
    'multiline.note': 'Chaque ligne devient son propre message. Une ligne peut aussi annoncer, faire un shoutout ou épingler.',
    'rules.b0.html': "<p>L'éditeur vérifie tout cela pour vous et dit ce qui cloche en mots simples. Pour référence:</p>",
    'rules.b1.head.0': 'Champ',
    'rules.b1.head.1': 'La règle',
    'rules.b1.rows.0.0': 'Nom',
    'rules.b1.rows.0.1': "1 à 64 caractères, sans espaces, et sans le «!» (le chat l'ajoute). Stocké en minuscules: <code>!Calin</code> et <code>!calin</code> sont la même commande.",
    'rules.b1.rows.1.0': 'Noms alternatifs',
    'rules.b1.rows.1.1': "Jusqu'à 25, chacun suivant les mêmes règles que le nom.",
    'rules.b1.rows.2.0': 'Réponse',
    'rules.b1.rows.2.1': "Jusqu'à 5 lignes de 500 caractères chacune (un message de chat par ligne).",
    'rules.b1.rows.3.0': 'Délai',
    'rules.b1.rows.3.1': '0 à 86400 secondes. Il est partagé par tout le chat: après une utilisation, tout le monde attend.',
    'rules.b1.rows.4.0': 'Accès',
    'rules.b1.rows.4.1': "Rang minimal, dans l'ordre: tout le monde, abonnés, VIP, modérateurs, modérateurs principaux, diffuseur. Chaque niveau inclut ceux au-dessus.",
    'rules.b1.rows.5.0': 'Restreindre à une personne',
    'rules.b1.rows.5.1': "Verrouillez une commande sur un seul compte Twitch; cela remplace entièrement le niveau d'accès. Parfait pour la commande personnelle d'un ami.",
    'rules.heading': 'Les règles du jeu',
    'rules.note': "Noms, limites, délais et niveaux d'accès. Tout ce que l'éditeur accepte et refuse.",
    'utilities.b0.html': `
            <p>
                Quelques variables travaillent sur ce que vous écrivez à l'intérieur, plutôt que
                sur la personne qui a lancé la commande. Elles ne demandent ni module ni réglage:
                écrivez-en une, et le bot la calcule au moment de répondre.
            </p>`,
    'utilities.b1.head.0': 'Variable',
    'utilities.b1.head.1': 'Devient',
    'utilities.b1.head.2': 'Exemple',
    'utilities.b1.rows.0.0': '<code>&#123;math:1+2*3&#125;</code>',
    'utilities.b1.rows.0.1': "Le résultat d'une petite opération. Des nombres entiers avec <code>+</code>, <code>-</code>, <code>*</code>, <code>/</code> et des parenthèses; la multiplication et la division passent d'abord, comme à l'école. La division arrondit vers zéro, et une division par zéro revient vide.",
    'utilities.b1.rows.0.2': '7',
    'utilities.b1.rows.1.0': '<code>&#123;countdown:2026-12-25&#125;</code>',
    'utilities.b1.rows.1.1': "Le temps restant avant cette date, dans les mêmes mots que <code>!uptime</code>. Écrivez la date AAAA-MM-JJ, ou en horodatage complet avec une heure et un fuseau. Une fois la date passée, le compte s'arrête au lieu de repartir à l'envers.",
    'utilities.b1.rows.1.2': '3 jours, 4 heures',
    'utilities.b1.rows.2.0': '<code>&#123;countup:2020-01-01&#125;</code>',
    'utilities.b1.rows.2.1': "Le temps écoulé depuis cette date. La même horloge, lue dans l'autre sens.",
    'utilities.b1.rows.2.2': '2 ans, 3 mois',
    'utilities.b1.rows.3.0': '<code>&#123;repeat:3:bagel&#125;</code>',
    'utilities.b1.rows.3.1': "Votre phrase, autant de fois, séparée par des espaces. 20 fois au maximum, et l'ensemble doit tenir sur une seule ligne de chat.",
    'utilities.b1.rows.3.2': 'bagel bagel bagel',
    'utilities.b1.rows.4.0': '<code>&#123;querystring&#125;</code>',
    'utilities.b1.rows.4.1': "Tout le texte tapé après la commande, encodé pour tenir dans une adresse web. C'est celle à mettre dans l'URL d'une <a href=\"/fr/guides/data-sources\">source de données</a>.",
    'utilities.b1.rows.4.2': 'alex+good+luck',
    'utilities.b1.rows.5.0': '<code>&#123;queryescape:hello world&#125;</code>',
    'utilities.b1.rows.5.1': "Le même encodage, appliqué à un texte que vous écrivez vous-même. <code>&#123;pathescape:…&#125;</code> en est la jumelle pour la partie chemin d'une URL, où l'espace devient <code>%20</code> au lieu de <code>+</code>.",
    'utilities.b1.rows.5.2': 'hello+world',
    'utilities.b2.caption': 'Un compte à rebours et un calcul, dans deux commandes ordinaires.',
    'utilities.b2.lines.0.name': 'maya_live',
    'utilities.b2.lines.0.text': '!sortie',
    'utilities.b2.lines.1.text': 'La nouvelle saison arrive dans 3 jours, 4 heures 🥯',
    'utilities.b2.lines.2.name': 'alex',
    'utilities.b2.lines.2.text': '!morts',
    'utilities.b2.lines.3.text': 'Ça fait 128 morts, soit 8 par heure. Sans faute.',
    'utilities.b2.title': '#votre_chaine',
    'utilities.b3.html': `
                <b>Elles lisent ce que vous avez tapé, pas ce qu'une variable contient</b>
                <code>&#123;math:&#123;counter:morts&#125;+1&#125;</code> ne marche pas: une variable
                dans une autre variable ne fait pas encore partie du langage, donc le calcul voit les
                accolades au lieu du nombre et revient vide. Tout ce qu'elles ne savent pas calculer
                revient vide aussi, et c'est exactement là qu'une valeur par défaut sert:
                <code>&#123;math:1/0|aucune idée&#125;</code>.`,
    'utilities.heading': 'Les petits calculs que la réponse fait elle-même',
    'utilities.note': "Calculs, comptes à rebours, répétitions, et l'encodage d'un texte pour qu'il survive à une URL.",
    'variables.b0.html': `
            <p>
                Écrivez <code>&#123;user&#125;</code> dans une réponse et le bot le remplace par le nom
                de la personne qui a lancé la commande. Cette seule idée alimente tout ce qui suit.
                Voici les variables de personnes et de lieux que toute commande personnalisée comprend:
            </p>`,
    'variables.b1.head.0': 'Variable',
    'variables.b1.head.1': 'Devient',
    'variables.b1.head.2': 'Exemple',
    'variables.b1.rows.0.0': '<code>&#123;user&#125;</code>',
    'variables.b1.rows.0.1': 'La personne qui a utilisé la commande. <code>&#123;sender&#125;</code> est un ancien alias, même valeur.',
    'variables.b1.rows.0.2': 'maya_live',
    'variables.b1.rows.1.0': '<code>&#123;touser&#125;</code>',
    'variables.b1.rows.1.1': "Le premier mot tapé après la commande, sans le «@». Si rien n'est tapé, le nom du spectateur lui-même. <code>&#123;target&#125;</code> est identique.",
    'variables.b1.rows.1.2': 'alex',
    'variables.b1.rows.2.0': '<code>&#123;args&#125;</code>',
    'variables.b1.rows.2.1': "Tout le texte tapé après la commande, en une seule chaîne. Vide si rien n'a été tapé.",
    'variables.b1.rows.2.2': 'alex bonne chance pour l’examen',
    'variables.b1.rows.3.0': '<code>&#123;1&#125;</code>, <code>&#123;2&#125;</code>, …',
    'variables.b1.rows.3.1': 'Un mot à la fois: <code>&#123;1&#125;</code> est le premier mot tapé après la commande, <code>&#123;2&#125;</code> le deuxième, jusqu’à <code>&#123;30&#125;</code>. Un mot que personne n’a tapé revient vide.',
    'variables.b1.rows.3.2': 'alex',
    'variables.b1.rows.4.0': '<code>&#123;2:&#125;</code>',
    'variables.b1.rows.4.1': 'Ce mot jusqu’à la fin, en une seule chaîne. Changez le numéro pour commencer ailleurs; <code>&#123;1:&#125;</code> reprend tout.',
    'variables.b1.rows.4.2': 'bonne chance pour l’examen',
    'variables.b1.rows.5.0': '<code>&#123;userid&#125;</code>',
    'variables.b1.rows.5.1': 'L’ID utilisateur Twitch du spectateur. Il ne change jamais, même s’il change de nom.',
    'variables.b1.rows.5.2': '48291057',
    'variables.b1.rows.6.0': '<code>&#123;user.login&#125;</code>',
    'variables.b1.rows.6.1': 'Son identifiant en minuscules, qui peut différer du nom d’affichage montré par <code>&#123;user&#125;</code>.',
    'variables.b1.rows.6.2': 'maya_live',
    'variables.b1.rows.7.0': '<code>&#123;command&#125;</code>',
    'variables.b1.rows.7.1': 'Le nom de la commande qui a répondu, sans le «!». Les autres noms rapportent tous le nom principal.',
    'variables.b1.rows.7.2': 'calin',
    'variables.b1.rows.8.0': '<code>&#123;channel&#125;</code>',
    'variables.b1.rows.8.1': "Le nom d'affichage de votre chaîne.",
    'variables.b1.rows.8.2': 'votre_chaine',
    'variables.b1.rows.9.0': '<code>&#123;urlfetch:name&#125;</code>',
    'variables.b1.rows.9.1': 'Une valeur récupérée d’une API web enregistrée comme source de données. Expliqué dans le <a href="/fr/guides/data-sources">guide des sources de données</a>.',
    'variables.b1.rows.9.2': '22',
    'variables.b2.caption': 'Une commande, deux phrases très différentes: !calin seul vs !calin alex.',
    'variables.b2.lines.0.name': 'maya_live',
    'variables.b2.lines.0.text': '!calin',
    'variables.b2.lines.1.text': 'maya_live donne à maya_live un gros câlin bagel 🥯',
    'variables.b2.lines.2.name': 'maya_live',
    'variables.b2.lines.2.text': '!calin alex',
    'variables.b2.lines.3.text': 'maya_live donne à alex un gros câlin bagel 🥯',
    'variables.b2.title': '#votre_chaine',
    'variables.b3.html': `
                <b>Attention</b>
                Une variable que le bot ne reconnaît pas reste telle quelle, accolades comprises. Si le
                chat affiche un <code>&#123;quelquechose&#125;</code> littéral, vérifiez l'orthographe
                dans le tableau ci-dessus (ou composez la commande dans le
                <a href="/fr/command-builder">constructeur</a>, qui ne propose que de vraies variables).`,
    'variables.heading': 'Les variables: les parties intelligentes',
    'variables.note': 'Les accolades sont des espaces réservés. Le bot les remplit au moment de répondre.',
    'viewer.b0.html': `
            <p>
                Le bot répond déjà à <code>!followage</code>, <code>!accountage</code> et
                <code>!points</code>. Ces variables vous donnent les mêmes réponses sous forme de
                texte, pour que vous puissiez les glisser dans votre propre phrase au lieu d'envoyer
                celle du bot. Chacune interroge la personne qui a lancé la commande; ajoutez un nom
                après deux-points pour interroger quelqu'un d'autre.
            </p>`,
    'viewer.b1.head.0': 'Variable',
    'viewer.b1.head.1': 'Devient',
    'viewer.b1.head.2': 'Exemple',
    'viewer.b1.rows.0.0': '<code>&#123;followage&#125;</code>',
    'viewer.b1.rows.0.1': "Depuis combien de temps la personne vous suit, formulé exactement comme <code>!followage</code> le dit. <code>&#123;followage:alex&#125;</code> interroge quelqu'un d'autre. Une personne qui ne suit pas ne renvoie rien, et le streamer non plus, puisqu'il ne peut pas suivre sa propre chaîne.",
    'viewer.b1.rows.0.2': '3 mois',
    'viewer.b1.rows.1.0': '<code>&#123;accountage&#125;</code>',
    'viewer.b1.rows.1.1': "L'âge de son compte Twitch. <code>&#123;accountage:alex&#125;</code> interroge quelqu'un d'autre.",
    'viewer.b1.rows.1.2': '4 ans, 2 mois',
    'viewer.b1.rows.2.0': '<code>&#123;points&#125;</code>',
    'viewer.b1.rows.2.1': "Son solde de points. <code>&#123;points:alex&#125;</code> affiche celui de quelqu'un d'autre, à condition que votre chaîne l'ait vu parler. La variable ne fait que lire: aucune commande ne distribue de points ainsi.",
    'viewer.b1.rows.2.2': '1280',
    'viewer.b1.rows.3.0': '<code>&#123;pointsname&#125;</code>',
    'viewer.b1.rows.3.1': 'Le nom que vous avez donné à vos points, pour qu’une même réponse sonne juste que les vôtres s’appellent points ou bagels.',
    'viewer.b1.rows.3.2': 'bagels',
    'viewer.b1.rows.4.0': '<code>&#123;watchtime&#125;</code>',
    'viewer.b1.rows.4.1': "Le temps passé à regarder pendant que l'horloge de fidélité tournait, formulé comme <code>!uptime</code>. <code>&#123;watchtime:alex&#125;</code> interroge quelqu'un d'autre.",
    'viewer.b1.rows.4.2': '2 heures, 30 minutes',
    'viewer.b2.caption': 'Une seule commande qui dit trois choses que le bot savait déjà.',
    'viewer.b2.lines.0.name': 'maya_live',
    'viewer.b2.lines.0.text': '!moi',
    'viewer.b2.lines.1.text': 'maya_live: follow depuis 3 mois, 1280 bagels, 2 heures, 30 minutes de visionnage 🥯',
    'viewer.b2.lines.2.name': 'maya_live',
    'viewer.b2.lines.2.text': '!moi alex',
    'viewer.b2.lines.3.text': 'alex: follow depuis 1 an, 2 mois, 340 bagels, 12 heures de visionnage 🥯',
    'viewer.b2.title': '#votre_chaine',
    'viewer.b3.html': `
                <b>Chacune a besoin de son module</b>
                <code>&#123;followage&#125;</code> nécessite le module Followage,
                <code>&#123;accountage&#125;</code> le module Âge du compte, et les trois variables de
                points le module Points de fidélité. Module éteint, le bot laisse la variable dans le
                message telle que vous l'avez tapée, accolades comprises: si le chat vous renvoie
                <code>&#123;points&#125;</code>, c'est l'interrupteur à vérifier. Module allumé mais
                rien à dire (une personne qui ne suit pas, quelqu'un que votre chaîne n'a jamais vu
                parler), la variable revient vide, et c'est exactement à ça que sert une valeur par
                défaut: <code>&#123;followage|pas encore&#125;</code>.`,
    'viewer.heading': 'Ce que le bot sait déjà d’un spectateur',
    'viewer.note': 'Ancienneté du follow, âge du compte et points de fidélité, glissés dans une phrase à vous.',
};

export default strings;
