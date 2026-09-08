// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// The "data-sources" guide in fr. Copy only: the structure it fills is the
// English guide, data-sources.en.ts, and every id below names one string in it
// (lib/guides/translate.ts derives the ids from where the strings sit).
// Adding a language is this file translated, with no structure to get wrong.
import type { GuideStrings } from '../../lib/guides/translate';

const strings: GuideStrings = {
    'create.b0.html': `
            <p>
                Tout se passe dans
                <a href="https://dashboard.itsbagelbot.com/commands" target="_blank" rel="noopener noreferrer">Commandes</a>,
                dans l'éditeur qui s'amarre à côté de votre liste de commandes.
            </p>`,
    'create.b1.items.0.html': "<p>Ouvrez une commande, cliquez dans <strong>Réponse</strong>, et laissez le curseur à l'endroit exact où la valeur doit apparaître dans la phrase.</p>",
    'create.b1.items.0.title': 'Placez le curseur là où va la valeur',
    'create.b1.items.1.html': "<p>Sous la réponse, à côté des pastilles <code>&#123;user&#125;</code> et <code>&#123;args&#125;</code>, se trouve une puce <strong>Source de données</strong>. Son infobulle indique «Insère une valeur récupérée depuis une définition d'API enregistrée».</p>",
    'create.b1.items.1.title': 'Ouvrez la palette',
    'create.b1.items.2.html': '<p><strong>+ Nouvelle source</strong> ouvre la fenêtre <strong>Ajouter une source de données</strong>: «Indiquez une API web, récupérez une vraie réponse, puis cliquez sur la valeur à afficher dans le chat.»</p>',
    'create.b1.items.2.title': 'Créez-en une',
    'create.b1.items.3.html': "<p><strong>Nom affiché</strong> est pour vous. Il devient automatiquement le <strong>Nom de la définition</strong>, le mot qui entre dans le jeton: «Lettres minuscules, chiffres et underscores. Utilisé dans &#123;urlfetch:name&#125;.» Puis <strong>Adresse web</strong>, en https, jusqu'à 512 caractères.</p>",
    'create.b1.items.3.title': "Nommez-la et collez l'adresse",
    'create.b1.items.4.html': '<p><strong>Récupérer un exemple</strong> appelle votre API pour de vrai, environ une fois toutes les 10 secondes. Si la réponse arrive dans une forme que la fenêtre ne sait pas lire, prenez le lien <strong>ou collez une réponse</strong> et collez-en une à la main.</p>',
    'create.b1.items.4.title': 'Récupérez une vraie réponse',
    'create.b1.items.5.html': '<p>La réponse devient une arborescence sous «Cliquez sur la valeur à afficher dans le chat.» Cliquez <code>temperature_2m</code> et la fenêtre affiche <strong>Affiche current.temperature_2m</strong>. <strong>Ajouter la source</strong> enregistre le tout, et la ligne de la palette insère <code>&#123;urlfetch:weather&#125;</code> à votre curseur.</p>',
    'create.b1.items.5.title': 'Cliquez la valeur, puis ajoutez la source',
    'create.b2.caption': 'La fenêtre «Ajouter une source de données» avec une valeur déjà choisie.',
    'create.b2.labels.authValue': 'Aucune clé requise',
    'create.b2.labels.cancel': 'Annuler',
    'create.b2.labels.close': 'Fermer',
    'create.b2.labels.create': 'Ajouter la source',
    'create.b2.labels.displayNameValue': 'Météo locale',
    'create.b2.labels.fetchSample': 'Récupérer un exemple',
    'create.b2.labels.fieldAuth': 'Clé API',
    'create.b2.labels.fieldDisplayName': 'Nom affiché',
    'create.b2.labels.fieldSlug': 'Nom de la définition',
    'create.b2.labels.fieldUrl': 'Adresse web',
    'create.b2.labels.intro': 'Indiquez une API web, récupérez une vraie réponse, puis cliquez sur la valeur à afficher dans le chat.',
    'create.b2.labels.panelHead': 'Ajouter une source de données',
    'create.b2.labels.pasteInstead': 'ou collez une réponse',
    'create.b2.labels.pickPrompt': 'Cliquez sur la valeur à afficher dans le chat.',
    'create.b2.labels.picked': 'Affiche current.temperature_2m',
    'create.b2.labels.slugHint': 'Lettres minuscules, chiffres et underscores. Utilisé dans &#123;urlfetch:name&#125;.',
    'create.b2.labels.treeRoot': '(réponse)',
    'create.b2.labels.wholeResponse': 'Utiliser toute la réponse',
    'create.b2.notes.0.text': 'Le nom affiché est celui que vous lisez dans les listes. Il devient automatiquement le nom de la définition en dessous.',
    'create.b2.notes.1.text': 'Le nom de la définition est le mot placé dans le jeton: lettres minuscules, chiffres et underscores, jusqu’à 32 caractères.',
    'create.b2.notes.2.text': 'Adresse web: https, absolue, jusqu’à 512 caractères. Le bot l’envoie telle quelle, à chaque fois.',
    'create.b2.notes.3.text': 'Récupérer un exemple lance une vraie requête vers votre API. Environ un test toutes les 10 secondes.',
    'create.b2.notes.4.text': 'Cliquer une valeur enregistre son chemin sur la source. «Utiliser toute la réponse» bascule en texte brut.',
    'create.b3.html': `
            <p>
                Une fois la source créée, la même puce la répertorie. Chaque source enregistrée affiche
                son chemin, ce qui permet de distinguer deux flux météo d'un coup d'œil, et cliquer une
                ligne dépose le jeton là où était votre curseur.
            </p>`,
    'create.b4.caption': 'La puce «Source de données» ouverte à côté des pastilles de jetons.',
    'create.b4.labels.chipCounter': 'Compteur',
    'create.b4.labels.chipDataSource': 'Source de données',
    'create.b4.labels.fieldResponse': 'Réponse',
    'create.b4.labels.insertVariable': 'Insérer une variable',
    'create.b4.labels.newSource': '+ Nouvelle source',
    'create.b4.labels.popoverTitle': 'Définitions enregistrées',
    'create.b4.labels.responseHtml': 'Il fait <span class="df-var">&#123;urlfetch:weather&#125;</span>°C à Montréal en ce moment. Demandez à <span class="df-var">&#123;user&#125;</span> s’il veut un manteau.',
    'create.b4.labels.row2Path': 'Texte brut',
    'create.b4.notes.0.text': 'La puce Source de données se trouve avec les pastilles de jetons sous Réponse, à côté de Compteur.',
    'create.b4.notes.1.text': 'Chaque ligne montre le chemin enregistré sur la source, ou «Texte brut» quand elle affiche toute la réponse.',
    'create.b4.notes.2.text': '+ Nouvelle source ouvre la fenêtre sans perdre la commande en cours d’écriture.',
    'create.b5.html': `
                <b>Astuce</b>
                Une chaîne peut garder 20 définitions. Au-delà, l'éditeur affiche
                «Vous avez atteint la limite de 20 définitions pour votre chaîne. Supprimez-en une pour
                faire de la place.» La suppression demande deux clics, et le serveur nomme les commandes
                qui citent la source avant de la lâcher: «Ces commandes la citent. Elles perdront ces
                données si vous supprimez :».`,
    'create.heading': "Ajouter une source depuis l'éditeur de commande",
    'create.note': 'Six clics, sans quitter la commande en cours.',
    'errors.b0.html': `
            <p>
                Une commande ne devient jamais muette à cause d'une source de données. Le reste de la
                phrase part quand même et la valeur est remplacée par l'une de ces quatre chaînes, ce
                qui vous permet de lire le chat et de savoir ce qui a cassé.
            </p>`,
    'errors.b1.head.0': "Ce qui s'est passé",
    'errors.b1.head.1': 'Ce que le chat affiche',
    'errors.b1.rows.0.0': "L'API a refusé, elle vous a limité, ou le message est la relecture d'un ancien",
    'errors.b1.rows.0.1': '<code>[source unavailable]</code>',
    'errors.b1.rows.1.0': "L'API a renvoyé une erreur, ou le chemin n'a rien trouvé d'utilisable",
    'errors.b1.rows.1.1': '<code>[source error]</code>',
    'errors.b1.rows.2.0': "L'API a mis plus de temps que le bot n'attend",
    'errors.b1.rows.2.1': '<code>[source timed out]</code>',
    'errors.b1.rows.3.0': 'La source manque, est en pause, ou le jeton en nomme une jamais enregistrée',
    'errors.b1.rows.3.1': 'Le jeton, affiché tel quel: <code>&#123;urlfetch:weather&#125;</code>',
    'errors.b2.labels.botName': 'ItsBagelBot',
    'errors.b2.labels.legend': 'Choisissez ce qui a mal tourné',
    'errors.b2.labels.title': '#your_channel',
    'errors.b2.labels.viewer': 'sesame_sam',
    'errors.b2.labels.viewerText': '!weather',
    'errors.b2.props.outcomes.0.bot': 'Il fait [source unavailable]°C à Montréal en ce moment.',
    'errors.b2.props.outcomes.0.label': "L'API a refusé",
    'errors.b2.props.outcomes.0.why': "Votre clé a été rejetée, ou le point d'accès a écarté la requête. Testez depuis le tableau de bord et vous obtenez le même verdict: «L'API a refusé la requête. Vérifiez la clé ou l'URL.»",
    'errors.b2.props.outcomes.1.bot': 'Il fait [source unavailable]°C à Montréal en ce moment.',
    'errors.b2.props.outcomes.1.label': 'Limite de débit',
    'errors.b2.props.outcomes.1.why': "Un plafond a été atteint: 6 récupérations par minute pour la chaîne, 30 pour cette définition, ou 120 par minute pour cette API toutes chaînes confondues. Rien n'est cassé et la minute suivante refonctionne.",
    'errors.b2.props.outcomes.2.bot': 'Il fait [source timed out]°C à Montréal en ce moment.',
    'errors.b2.props.outcomes.2.label': 'Délai dépassé',
    'errors.b2.props.outcomes.2.why': 'Le bot attend 3,5 secondes puis parle sans la valeur. Une API lente sous charge en est la raison habituelle.',
    'errors.b2.props.outcomes.3.bot': 'Il fait [source error]°C à Montréal en ce moment.',
    'errors.b2.props.outcomes.3.label': 'La valeur a bougé',
    'errors.b2.props.outcomes.3.why': "Le chemin était correct mais la réponse n'avait rien au bout, en général parce que l'API a renommé un champ. Ouvrez la source, récupérez un exemple, et recliquez la valeur.",
    'errors.b2.props.outcomes.4.bot': 'Il fait {urlfetch:weather}°C à Montréal en ce moment.',
    'errors.b2.props.outcomes.4.label': 'Source en pause',
    'errors.b2.props.outcomes.4.why': "Une source en pause ou supprimée n'a rien à développer, donc le jeton est affiché tel qu'il est tapé. Le test du tableau de bord le dit clairement: «Définition manquante ou en pause. Le chat affiche le jeton brut tant qu'elle n'est pas active.»",
    'errors.b2.props.outcomes.5.bot': 'Classement: {urlfetch:standings}',
    'errors.b2.props.outcomes.5.label': 'Trop de sources',
    'errors.b2.props.outcomes.5.why': "Trois sources de données par réponse est la limite de l'éditeur, et il refuse d'en enregistrer une quatrième. Le bot garde son propre plafond à huit et affiche tel quel chaque jeton au-delà, comme celui-ci.",
    'errors.b3.html': `
                <b>Note</b>
                Ces quatre chaînes ne sont pas traduites. Une chaîne francophone voit elle aussi
                <code>[source unavailable]</code>, ce qui les garde faciles à rechercher et garde cette
                page honnête sur ce que vos spectateurs liront.`,
    'errors.heading': 'Ce que le chat affiche en cas d’échec',
    'errors.note': 'Quatre replis, tous courts, tous en anglais où que soit votre chat.',
    'keys.b0.html': `
            <p>
                Certaines API exigent une clé avant de répondre. Ajoutez-la une fois dans
                <strong>Paramètres</strong>, sur le panneau <strong>Clés API</strong>: un
                <strong>libellé</strong> jusqu'à 32 caractères pour la reconnaître plus tard, et le
                <strong>secret</strong> lui-même, jusqu'à 512 caractères. L'indication du panneau le dit
                mieux que nous: «Secrets au niveau du compte pour les sources de données. Les délégués
                peuvent les utiliser, jamais les lire.»
            </p>
            <p>
                Le secret est scellé avant d'être écrit et il n'est plus jamais réaffiché. Il vous reste
                le libellé et les 4 derniers caractères, ce qui suffit à distinguer deux clés quand vous
                en changez une. Quand une source de données porte une clé, le bot l'envoie dans un
                en-tête <code>Authorization: Bearer</code> à chaque récupération, et l'adresse reste
                propre.
            </p>
            <p>
                Dans la fenêtre «Ajouter une source de données», le champ <strong>Clé API</strong>
                n'apparaît qu'une fois qu'au moins une clé est enregistrée. Avant cela, il indique
                «Aucune clé requise», ce qui est aussi la bonne réponse pour la plupart des API
                publiques.
            </p>`,
    'keys.b1.html': `
                <b>Attention</b>
                Gardez la clé hors de l'<strong>Adresse web</strong> et hors de la réponse de la
                commande. Une adresse est stockée en texte et toute personne ayant accès au tableau de
                bord peut la lire, et une réponse part dans le chat où tout le monde la voit. Si votre
                API n'accepte la clé qu'en paramètre d'URL, considérez cette clé comme publique et
                changez-la régulièrement.`,
    'keys.heading': 'Les API qui demandent une clé',
    'keys.note': 'Les clés vivent sur votre compte, scellées, et voyagent dans un en-tête Authorization.',
    'limits.b0.html': `
            <p>
                Les chiffres ci-dessous sont ceux qui tournent en production. Le cache est celui que
                vous rencontrerez en premier: une réponse acceptée est réutilisée pendant 30 secondes,
                donc une commande lancée 40 fois en une minute n'appelle votre API que deux fois.
            </p>`,
    'limits.b1.caption': 'Identique sur toutes les formules.',
    'limits.b1.head.0': 'Limite',
    'limits.b1.head.1': 'Le chiffre',
    'limits.b1.rows.0.0': 'Définitions par chaîne',
    'limits.b1.rows.0.1': '20.',
    'limits.b1.rows.1.0': 'Sources de données dans une réponse',
    'limits.b1.rows.1.1': "3. L'éditeur refuse d'en enregistrer une quatrième.",
    'limits.b1.rows.10.0': "Requêtes par hôte d'API",
    'limits.b1.rows.10.1': '120 par minute, comptées sur toutes les chaînes pointant vers cet hôte.',
    'limits.b1.rows.11.0': 'Redirections',
    'limits.b1.rows.11.1': 'Jusqu’à 3 sauts, chacun restant en https.',
    'limits.b1.rows.12.0': 'Un hôte qui échoue en série',
    'limits.b1.rows.12.1': 'Cinq échecs de transport d’affilée et cet hôte est mis au repos 60 secondes.',
    'limits.b1.rows.13.0': '«Récupérer un exemple»',
    'limits.b1.rows.13.1': 'Environ un test toutes les 10 secondes. Le refus reste en anglais: "Too many test runs. Each one calls the real API. Wait about 10 seconds and try again."',
    'limits.b1.rows.2.0': 'Adresse web',
    'limits.b1.rows.2.1': 'https uniquement, absolue, jusqu’à 512 caractères.',
    'limits.b1.rows.3.0': 'Adresses refusées',
    'limits.b1.rows.3.1': 'Les adresses IP littérales, <code>localhost</code>, et tout ce qui finit par <code>.local</code> ou <code>.internal</code>. Vérifié à l’enregistrement puis à chaque récupération.',
    'limits.b1.rows.4.0': 'Taille de la réponse',
    'limits.b1.rows.4.1': '1 Mio, mesuré après décompression.',
    'limits.b1.rows.5.0': 'Type de contenu',
    'limits.b1.rows.5.1': '<code>application/json</code> ou <code>text/*</code>.',
    'limits.b1.rows.6.0': 'Délais',
    'limits.b1.rows.6.1': "L'API a 2,5 secondes, le service de récupération 3 secondes, et le bot cesse d'attendre à 3,5 secondes.",
    'limits.b1.rows.7.0': 'Cache',
    'limits.b1.rows.7.1': "Une bonne réponse est gardée 30 secondes. Un refus, comme un 404 ou un chemin qui ne trouve rien, est gardé 15 secondes. Une panne n'est pas mise en cache.",
    'limits.b1.rows.8.0': 'Requêtes par chaîne',
    'limits.b1.rows.8.1': '6 par minute, toutes définitions confondues.',
    'limits.b1.rows.9.0': 'Requêtes par définition',
    'limits.b1.rows.9.1': '30 par minute.',
    'limits.b2.labels.blockedLabel': 'Refusées par le plafond de 6 par minute',
    'limits.b2.labels.blockedLine': 'Ces exécutions affichent [source unavailable] dans le chat.',
    'limits.b2.labels.cachedLabel': 'Servies par le cache de 30 secondes',
    'limits.b2.labels.fetchesLabel': 'Requêtes réellement reçues par votre API',
    'limits.b2.labels.fetchesUnit': 'par minute',
    'limits.b2.labels.runsLabel': 'Les spectateurs lancent la commande',
    'limits.b2.labels.runsUnit': 'fois par minute',
    'limits.b2.labels.sourcesLabel': 'Sources de données citées par vos commandes',
    'limits.b2.labels.why': "Le cache existe pour votre quota d'API. Sans lui, un seul raid dépenserait un mois d'appels en une soirée, et toutes les autres chaînes pointant vers le même hôte le sentiraient aussi.",
    'limits.b3.html': `
                <b>Note</b>
                Deux secondes de patience, c'est long dans un chat. Si votre API est lente, attendez-vous
                à <code>[source timed out]</code> pendant un direct chargé et écrivez la phrase pour
                qu'elle se lise encore sans la valeur.`,
    'limits.heading': 'Limites et délais',
    'limits.note': 'Tout est plafonné, et le cache fait le gros du travail.',
    'meta.card.chips.0': '{urlfetch}',
    'meta.card.chips.1': 'Chemin JSON',
    'meta.card.chips.2': 'Clés API',
    'meta.card.description': "Mettez des valeurs en direct dans une commande: la météo, une statistique de jeu, votre propre API JSON. Enregistrez une adresse web, cliquez sur la valeur, et le bot l'annonce.",
    'meta.card.meta': '8 min · 7 étapes',
    'meta.card.title': 'Sources de données',
    'meta.description': "Affichez une valeur en direct depuis n'importe quelle API web dans une commande avec {urlfetch}: ajouter une source de données, choisir la valeur avec un chemin JSON, joindre une clé API, et connaître le cache et les limites de débit avant que le chat ne les trouve.",
    'meta.eyebrow': 'Guide',
    'meta.heading': 'Sources de données',
    'meta.lead': 'Enregistrez une adresse web une fois, cliquez sur la valeur voulue, et une commande la lit en direct.',
    'meta.minutes': '8 min de lecture',
    'meta.title': 'Sources de données - Guides ItsBagelBot',
    'paths.b0.html': `
            <p>
                Voici la même arborescence que dans le tableau de bord, avec les mêmes règles. Seules
                les valeurs sont cliquables; une branche comme <code>current</code> contient d'autres
                choses, elle ne peut donc pas terminer un chemin. Modifiez la réponse à gauche et
                l'arborescence suit, ce qui reste la façon la plus rapide de répéter avec votre propre
                API avant d'enregistrer quoi que ce soit.
            </p>`,
    'paths.b1.labels.badJson': "Ce n'est pas du JSON valide. Corrigez-le et l'arborescence apparaîtra ici.",
    'paths.b1.labels.badSegment': 'Cette clé contient des caractères que la grammaire des chemins refuse. Lettres, chiffres, underscore et trait d’union seulement, jusqu’à 64 caractères.',
    'paths.b1.labels.chatLabel': 'Le chat afficherait',
    'paths.b1.labels.cutHere': 'coupé ici, 100 octets',
    'paths.b1.labels.defName': 'weather',
    'paths.b1.labels.empty': "L'arborescence apparaît une fois le JSON analysé.",
    'paths.b1.labels.notUsable': 'Inutilisable. Un chemin doit se terminer sur une valeur, et celle-ci est vide.',
    'paths.b1.labels.overrideLabel': 'Chemin écrit dans le jeton',
    'paths.b1.labels.sampleLabel': "Réponse d'exemple",
    'paths.b1.labels.savedPathLabel': 'Enregistré sur la source',
    'paths.b1.labels.tokenLabel': 'Jeton que vous tapez',
    'paths.b1.labels.tooDeep': 'Plus profond que 8 niveaux. Choisissez quelque chose de plus proche du sommet.',
    'paths.b1.labels.treeLabel': 'Cliquez une valeur',
    'paths.b1.labels.wholeButton': 'Utiliser toute la réponse',
    'paths.b1.labels.wholeLabel': 'Texte brut',
    'paths.b1.labels.wholeNote': "Le mode texte brut n'enregistre aucun chemin. Le bot nettoie la réponse et affiche ses 100 premiers octets.",
    'paths.b2.caption': "La grammaire des chemins, celle-là même que l'éditeur vérifie à l'enregistrement.",
    'paths.b2.head.0': 'Règle',
    'paths.b2.head.1': 'Ce que ça veut dire',
    'paths.b2.rows.0.0': 'Des points entre les étapes',
    'paths.b2.rows.0.1': '<code>current.temperature_2m</code> ouvre <code>current</code>, puis en sort <code>temperature_2m</code>.',
    'paths.b2.rows.1.0': 'Les listes utilisent des chiffres nus',
    'paths.b2.rows.1.1': '<code>items.0.name</code> est la première entrée. Les crochets ne font pas partie de la grammaire.',
    'paths.b2.rows.2.0': 'Profondeur',
    'paths.b2.rows.2.1': 'Jusqu’à 8 étapes. Au-delà, l’arborescence refuse la valeur: «Plus profond que 8 niveaux. Choisissez quelque chose de plus proche du sommet.»',
    'paths.b2.rows.3.0': 'Chaque étape',
    'paths.b2.rows.3.1': 'Lettres, chiffres, underscore et trait d’union, jusqu’à 64 caractères chacune.',
    'paths.b2.rows.4.0': 'La fin du chemin',
    'paths.b2.rows.4.1': "Se pose sur une valeur: du texte, un nombre, true ou false. S'arrêter sur un objet, une liste ou un champ vide compte comme une définition cassée, et le chat reçoit le jeton brut.",
    'paths.b2.rows.5.0': 'Un chemin dans le jeton',
    'paths.b2.rows.5.1': "L'emporte sur celui enregistré sur la source. <code>&#123;urlfetch:weather.current.wind_speed_10m&#125;</code> lit une autre valeur à la même adresse.",
    'paths.b2.rows.6.0': 'Le nom',
    'paths.b2.rows.6.1': 'Insensible à la casse. <code>&#123;URLFETCH:Weather&#125;</code> et <code>&#123;urlfetch:weather&#125;</code> désignent la même source.',
    'paths.b2.rows.7.0': 'La valeur',
    'paths.b2.rows.7.1': 'Nettoyée, rognée, puis coupée à 100 octets avant d’arriver dans le chat.',
    'paths.b3.html': `
                <b>Astuce</b>
                Une seule adresse enregistrée peut nourrir plusieurs commandes. Enregistrez
                <code>&#123;urlfetch:weather&#125;</code> sur <code>current.temperature_2m</code> pour
                <code>!weather</code>, puis écrivez
                <code>&#123;urlfetch:weather.current.wind_speed_10m&#125;</code> dans <code>!wind</code>.
                Même source, même budget de 20 définitions, deux réponses différentes.`,
    'paths.heading': 'Choisir la valeur',
    'paths.note': 'Un chemin, ce sont des points entre les étapes. Cliquez une valeur et regardez le jeton s’écrire.',
    'rules.b0.html': `
            <ul>
                <li>
                    L'adresse est figée à l'enregistrement. <code>&#123;args&#125;</code> et
                    <code>&#123;user&#125;</code> restent intacts dans une adresse web, donc un
                    spectateur ne peut pas diriger la requête envoyée à votre API.
                </li>
                <li>
                    Les requêtes sont en <code>GET</code>, et les en-têtes ne sont pas les vôtres à
                    régler. Le seul que le bot ajoute est <code>Authorization: Bearer</code>, et
                    seulement quand la source porte une clé.
                </li>
                <li>
                    Les jetons ne se développent que dans les réponses de commandes personnalisées,
                    après les vérifications de permission, de direct et de délai. Un minuteur publie son
                    texte brut, et les compteurs laissent le jeton tranquille.
                </li>
                <li>
                    Un message de chat rejoué ne récupère jamais deux fois. Si le bot relit un
                    évènement ancien, le chat reçoit <code>[source unavailable]</code> plutôt qu'un
                    second appel à votre API.
                </li>
                <li>
                    Les adresses privées et locales sont écartées aux deux bouts: à l'enregistrement,
                    puis de nouveau au moment de la récupération.
                </li>
            </ul>`,
    'rules.b1.html': `
                <b>Astuce</b>
                Citez deux fois la même source dans une réponse et le bot ne récupère qu'une fois.
                Écrivez <code>&#123;urlfetch:weather&#125;</code> dans la phrase et
                <code>&#123;urlfetch:weather.current.wind_speed_10m&#125;</code> juste après: deux
                valeurs, une requête, une ligne de votre quota.`,
    'rules.heading': 'Ce qu’une source ne fera pas',
    'rules.note': 'Les bords, en un écran, avant de concevoir une commande autour.',
    'what.b0.html': `
            <p>
                Une source de données est une adresse web enregistrée une seule fois. Quand un
                spectateur lance une commande qui la cite, le bot appelle cette adresse, extrait une
                valeur de la réponse et la dit dans le chat. La météo, une statistique de jeu, la
                longueur de la file sur votre propre serveur: si ça répond en https et renvoie du JSON
                ou du texte, une commande peut le lire.
            </p>`,
    'what.b1.caption': 'Une commande, une valeur en direct.',
    'what.b1.lines.0.name': 'sesame_sam',
    'what.b1.lines.0.text': '!weather',
    'what.b1.lines.1.text': 'Il fait 21°C à Montréal en ce moment.',
    'what.b1.title': '#your_channel',
    'what.b2.html': `
            <p>
                Derrière cette ligne se cache une commande personnalisée ordinaire. Sa réponse, telle
                qu'elle est tapée dans l'éditeur:
            </p>
            <p><code>Il fait &#123;urlfetch:weather&#125;°C à Montréal en ce moment.</code></p>
            <p>
                Le jeton nomme la source, pas la valeur. L'endroit où le bot va chercher cette valeur
                est enregistré sur la source elle-même, sous forme de <strong>chemin</strong>: une
                adresse à l'intérieur de la réponse. La réponse est l'immeuble, <code>current</code>
                est l'étage, <code>temperature_2m</code> est la porte. Écrivez le tout avec des points
                et vous obtenez <code>current.temperature_2m</code>. La section trois fait ces clics
                pour vous, et l'analogie peut rentrer chez elle.
            </p>`,
    'what.b3.html': `
                <b>Note</b>
                Les sources de données sont gratuites sur toutes les formules. Une chaîne premium passe
                par une voie interne différente, et chaque limite de cette page est la même pour les
                deux.`,
    'what.heading': "Ce qu'est une source de données",
    'what.note': 'Une commande, une adresse web, une valeur que le bot annonce.',
};

export default strings;
