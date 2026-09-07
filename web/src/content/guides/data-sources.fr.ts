// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { GuideContent } from '../../lib/guides/types';

// La réponse de départ du widget PathPicker. Identique en anglais: c'est une
// réponse d'API, pas du texte à traduire, et une clé traduite enseignerait un
// chemin qui n'existe pas.
const SAMPLE = `{
  "latitude": 45.5,
  "longitude": -73.6,
  "current": {
    "time": "2026-09-07T14:00",
    "temperature_2m": 21.4,
    "wind_speed_10m": 9.2,
    "rain": null
  },
  "units": {
    "temperature_2m": "°C",
    "wind_speed_10m": "km/h"
  },
  "hourly": {
    "temperature_2m": [20.1, 21.4, 22.8, 23.2]
  }
}`;

const guide: GuideContent = {
  slug: 'data-sources',
  meta: {
    title: 'Sources de données - Guides ItsBagelBot',
    description:
      "Affichez une valeur en direct depuis n'importe quelle API web dans une commande avec {urlfetch}: ajouter une source de données, choisir la valeur avec un chemin JSON, joindre une clé API, et connaître le cache et les limites de débit avant que le chat ne les trouve.",
    eyebrow: 'Guide',
    heading: 'Sources de données',
    lead: "Enregistrez une adresse web une fois, cliquez sur la valeur voulue, et une commande la lit en direct.",
    minutes: '8 min de lecture',
    card: {
      title: 'Sources de données',
      description:
        "Mettez des valeurs en direct dans une commande: la météo, une statistique de jeu, votre propre API JSON. Enregistrez une adresse web, cliquez sur la valeur, et le bot l'annonce.",
      meta: '8 min · 7 étapes',
      chips: ['{urlfetch}', 'Chemin JSON', 'Clés API'],
    },
  },
  sections: [
    {
      id: 'what',
      heading: "Ce qu'est une source de données",
      note: 'Une commande, une adresse web, une valeur que le bot annonce.',
      blocks: [
        {
          kind: 'prose',
          html: `
            <p>
                Une source de données est une adresse web enregistrée une seule fois. Quand un
                spectateur lance une commande qui la cite, le bot appelle cette adresse, extrait une
                valeur de la réponse et la dit dans le chat. La météo, une statistique de jeu, la
                longueur de la file sur votre propre serveur: si ça répond en https et renvoie du JSON
                ou du texte, une commande peut le lire.
            </p>`,
        },
        {
          kind: 'chat',
          title: '#your_channel',
          caption: 'Une commande, une valeur en direct.',
          lines: [
            { who: 'viewer', name: 'sesame_sam', text: '!weather' },
            { who: 'bot', text: 'Il fait 21°C à Montréal en ce moment.' },
          ],
        },
        {
          kind: 'prose',
          html: `
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
        },
        {
          kind: 'callout',
          tone: 'note',
          html: `
                <b>Note</b>
                Les sources de données sont gratuites sur toutes les formules. Une chaîne premium passe
                par une voie interne différente, et chaque limite de cette page est la même pour les
                deux.`,
        },
      ],
    },
    {
      id: 'create',
      heading: "Ajouter une source depuis l'éditeur de commande",
      note: 'Six clics, sans quitter la commande en cours.',
      blocks: [
        {
          kind: 'prose',
          html: `
            <p>
                Tout se passe dans
                <a href="https://dashboard.itsbagelbot.com/commands" target="_blank" rel="noopener noreferrer">Commandes</a>,
                dans l'éditeur qui s'amarre à côté de votre liste de commandes.
            </p>`,
        },
        {
          kind: 'steps',
          items: [
            {
              title: 'Placez le curseur là où va la valeur',
              html: `<p>Ouvrez une commande, cliquez dans <strong>Réponse</strong>, et laissez le curseur à l'endroit exact où la valeur doit apparaître dans la phrase.</p>`,
            },
            {
              title: 'Ouvrez la palette',
              html: `<p>Sous la réponse, à côté des pastilles <code>&#123;user&#125;</code> et <code>&#123;args&#125;</code>, se trouve une puce <strong>Source de données</strong>. Son infobulle indique «Insère une valeur récupérée depuis une définition d'API enregistrée».</p>`,
            },
            {
              title: 'Créez-en une',
              html: `<p><strong>+ Nouvelle source</strong> ouvre la fenêtre <strong>Ajouter une source de données</strong>: «Indiquez une API web, récupérez une vraie réponse, puis cliquez sur la valeur à afficher dans le chat.»</p>`,
            },
            {
              title: "Nommez-la et collez l'adresse",
              html: `<p><strong>Nom affiché</strong> est pour vous. Il devient automatiquement le <strong>Nom de la définition</strong>, le mot qui entre dans le jeton: «Lettres minuscules, chiffres et underscores. Utilisé dans &#123;urlfetch:name&#125;.» Puis <strong>Adresse web</strong>, en https, jusqu'à 512 caractères.</p>`,
            },
            {
              title: 'Récupérez une vraie réponse',
              html: `<p><strong>Récupérer un exemple</strong> appelle votre API pour de vrai, environ une fois toutes les 10 secondes. Si la réponse arrive dans une forme que la fenêtre ne sait pas lire, prenez le lien <strong>ou collez une réponse</strong> et collez-en une à la main.</p>`,
            },
            {
              title: 'Cliquez la valeur, puis ajoutez la source',
              html: `<p>La réponse devient une arborescence sous «Cliquez sur la valeur à afficher dans le chat.» Cliquez <code>temperature_2m</code> et la fenêtre affiche <strong>Affiche current.temperature_2m</strong>. <strong>Ajouter la source</strong> enregistre le tout, et la ligne de la palette insère <code>&#123;urlfetch:weather&#125;</code> à votre curseur.</p>`,
            },
          ],
        },
        {
          kind: 'dash',
          screen: 'DataSourceModal',
          path: '/commands',
          caption: "La fenêtre «Ajouter une source de données» avec une valeur déjà choisie.",
          notes: [
            { n: 1, text: 'Le nom affiché est celui que vous lisez dans les listes. Il devient automatiquement le nom de la définition en dessous.' },
            { n: 2, text: 'Le nom de la définition est le mot placé dans le jeton: lettres minuscules, chiffres et underscores, jusqu’à 32 caractères.' },
            { n: 3, text: 'Adresse web: https, absolue, jusqu’à 512 caractères. Le bot l’envoie telle quelle, à chaque fois.' },
            { n: 4, text: 'Récupérer un exemple lance une vraie requête vers votre API. Environ un test toutes les 10 secondes.' },
            { n: 5, text: 'Cliquer une valeur enregistre son chemin sur la source. «Utiliser toute la réponse» bascule en texte brut.' },
          ],
          labels: {
            panelHead: 'Ajouter une source de données',
            close: 'Fermer',
            intro: 'Indiquez une API web, récupérez une vraie réponse, puis cliquez sur la valeur à afficher dans le chat.',
            fieldDisplayName: 'Nom affiché',
            displayNameValue: 'Météo locale',
            fieldSlug: 'Nom de la définition',
            slugHint: 'Lettres minuscules, chiffres et underscores. Utilisé dans &#123;urlfetch:name&#125;.',
            fieldUrl: 'Adresse web',
            fieldAuth: 'Clé API',
            authValue: 'Aucune clé requise',
            fetchSample: 'Récupérer un exemple',
            pasteInstead: 'ou collez une réponse',
            pickPrompt: 'Cliquez sur la valeur à afficher dans le chat.',
            treeRoot: '(réponse)',
            picked: 'Affiche current.temperature_2m',
            wholeResponse: 'Utiliser toute la réponse',
            cancel: 'Annuler',
            create: 'Ajouter la source',
          },
        },
        {
          kind: 'prose',
          html: `
            <p>
                Une fois la source créée, la même puce la répertorie. Chaque source enregistrée affiche
                son chemin, ce qui permet de distinguer deux flux météo d'un coup d'œil, et cliquer une
                ligne dépose le jeton là où était votre curseur.
            </p>`,
        },
        {
          kind: 'dash',
          screen: 'DataSourcePicker',
          path: '/commands',
          caption: 'La puce «Source de données» ouverte à côté des pastilles de jetons.',
          notes: [
            { n: 1, text: 'La puce Source de données se trouve avec les pastilles de jetons sous Réponse, à côté de Compteur.' },
            { n: 2, text: 'Chaque ligne montre le chemin enregistré sur la source, ou «Texte brut» quand elle affiche toute la réponse.' },
            { n: 3, text: '+ Nouvelle source ouvre la fenêtre sans perdre la commande en cours d’écriture.' },
          ],
          labels: {
            fieldResponse: 'Réponse',
            responseHtml:
              'Il fait <span class="df-var">&#123;urlfetch:weather&#125;</span>°C à Montréal en ce moment. Demandez à <span class="df-var">&#123;user&#125;</span> s’il veut un manteau.',
            insertVariable: 'Insérer une variable',
            chipCounter: 'Compteur',
            chipDataSource: 'Source de données',
            popoverTitle: 'Définitions enregistrées',
            row2Path: 'Texte brut',
            newSource: '+ Nouvelle source',
          },
        },
        {
          kind: 'callout',
          tone: 'tip',
          html: `
                <b>Astuce</b>
                Une chaîne peut garder 20 définitions. Au-delà, l'éditeur affiche
                «Vous avez atteint la limite de 20 définitions pour votre chaîne. Supprimez-en une pour
                faire de la place.» La suppression demande deux clics, et le serveur nomme les commandes
                qui citent la source avant de la lâcher: «Ces commandes la citent. Elles perdront ces
                données si vous supprimez :».`,
        },
      ],
    },
    {
      id: 'paths',
      heading: 'Choisir la valeur',
      note: 'Un chemin, ce sont des points entre les étapes. Cliquez une valeur et regardez le jeton s’écrire.',
      blocks: [
        {
          kind: 'prose',
          html: `
            <p>
                Voici la même arborescence que dans le tableau de bord, avec les mêmes règles. Seules
                les valeurs sont cliquables; une branche comme <code>current</code> contient d'autres
                choses, elle ne peut donc pas terminer un chemin. Modifiez la réponse à gauche et
                l'arborescence suit, ce qui reste la façon la plus rapide de répéter avec votre propre
                API avant d'enregistrer quoi que ce soit.
            </p>`,
        },
        {
          kind: 'widget',
          name: 'PathPicker',
          props: { sample: SAMPLE },
          labels: {
            sampleLabel: "Réponse d'exemple",
            treeLabel: 'Cliquez une valeur',
            empty: "L'arborescence apparaît une fois le JSON analysé.",
            badJson: "Ce n'est pas du JSON valide. Corrigez-le et l'arborescence apparaîtra ici.",
            tooDeep: 'Plus profond que 8 niveaux. Choisissez quelque chose de plus proche du sommet.',
            badSegment: 'Cette clé contient des caractères que la grammaire des chemins refuse. Lettres, chiffres, underscore et trait d’union seulement, jusqu’à 64 caractères.',
            notUsable: 'Inutilisable. Un chemin doit se terminer sur une valeur, et celle-ci est vide.',
            savedPathLabel: 'Enregistré sur la source',
            tokenLabel: 'Jeton que vous tapez',
            overrideLabel: 'Chemin écrit dans le jeton',
            chatLabel: 'Le chat afficherait',
            cutHere: 'coupé ici, 100 octets',
            wholeButton: 'Utiliser toute la réponse',
            wholeLabel: 'Texte brut',
            wholeNote: "Le mode texte brut n'enregistre aucun chemin. Le bot nettoie la réponse et affiche ses 100 premiers octets.",
            defName: 'weather',
          },
        },
        {
          kind: 'table',
          head: ['Règle', 'Ce que ça veut dire'],
          caption: "La grammaire des chemins, celle-là même que l'éditeur vérifie à l'enregistrement.",
          rows: [
            ['Des points entre les étapes', '<code>current.temperature_2m</code> ouvre <code>current</code>, puis en sort <code>temperature_2m</code>.'],
            ['Les listes utilisent des chiffres nus', "<code>items.0.name</code> est la première entrée. Les crochets ne font pas partie de la grammaire."],
            ['Profondeur', 'Jusqu’à 8 étapes. Au-delà, l’arborescence refuse la valeur: «Plus profond que 8 niveaux. Choisissez quelque chose de plus proche du sommet.»'],
            ['Chaque étape', 'Lettres, chiffres, underscore et trait d’union, jusqu’à 64 caractères chacune.'],
            ['La fin du chemin', "Se pose sur une valeur: du texte, un nombre, true ou false. S'arrêter sur un objet, une liste ou un champ vide compte comme une définition cassée, et le chat reçoit le jeton brut."],
            ['Un chemin dans le jeton', "L'emporte sur celui enregistré sur la source. <code>&#123;urlfetch:weather.current.wind_speed_10m&#125;</code> lit une autre valeur à la même adresse."],
            ['Le nom', 'Insensible à la casse. <code>&#123;URLFETCH:Weather&#125;</code> et <code>&#123;urlfetch:weather&#125;</code> désignent la même source.'],
            ['La valeur', 'Nettoyée, rognée, puis coupée à 100 octets avant d’arriver dans le chat.'],
          ],
        },
        {
          kind: 'callout',
          tone: 'tip',
          html: `
                <b>Astuce</b>
                Une seule adresse enregistrée peut nourrir plusieurs commandes. Enregistrez
                <code>&#123;urlfetch:weather&#125;</code> sur <code>current.temperature_2m</code> pour
                <code>!weather</code>, puis écrivez
                <code>&#123;urlfetch:weather.current.wind_speed_10m&#125;</code> dans <code>!wind</code>.
                Même source, même budget de 20 définitions, deux réponses différentes.`,
        },
      ],
    },
    {
      id: 'keys',
      heading: 'Les API qui demandent une clé',
      note: 'Les clés vivent sur votre compte, scellées, et voyagent dans un en-tête Authorization.',
      blocks: [
        {
          kind: 'prose',
          html: `
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
        },
        {
          kind: 'callout',
          tone: 'warn',
          html: `
                <b>Attention</b>
                Gardez la clé hors de l'<strong>Adresse web</strong> et hors de la réponse de la
                commande. Une adresse est stockée en texte et toute personne ayant accès au tableau de
                bord peut la lire, et une réponse part dans le chat où tout le monde la voit. Si votre
                API n'accepte la clé qu'en paramètre d'URL, considérez cette clé comme publique et
                changez-la régulièrement.`,
        },
      ],
    },
    {
      id: 'limits',
      heading: 'Limites et délais',
      note: 'Tout est plafonné, et le cache fait le gros du travail.',
      blocks: [
        {
          kind: 'prose',
          html: `
            <p>
                Les chiffres ci-dessous sont ceux qui tournent en production. Le cache est celui que
                vous rencontrerez en premier: une réponse acceptée est réutilisée pendant 30 secondes,
                donc une commande lancée 40 fois en une minute n'appelle votre API que deux fois.
            </p>`,
        },
        {
          kind: 'table',
          head: ['Limite', 'Le chiffre'],
          caption: 'Identique sur toutes les formules.',
          rows: [
            ['Définitions par chaîne', '20.'],
            ['Sources de données dans une réponse', "3. L'éditeur refuse d'en enregistrer une quatrième."],
            ['Adresse web', 'https uniquement, absolue, jusqu’à 512 caractères.'],
            ['Adresses refusées', 'Les adresses IP littérales, <code>localhost</code>, et tout ce qui finit par <code>.local</code> ou <code>.internal</code>. Vérifié à l’enregistrement puis à chaque récupération.'],
            ['Taille de la réponse', '1 Mio, mesuré après décompression.'],
            ['Type de contenu', '<code>application/json</code> ou <code>text/*</code>.'],
            ['Délais', "L'API a 2,5 secondes, le service de récupération 3 secondes, et le bot cesse d'attendre à 3,5 secondes."],
            ['Cache', "Une bonne réponse est gardée 30 secondes. Un refus, comme un 404 ou un chemin qui ne trouve rien, est gardé 15 secondes. Une panne n'est pas mise en cache."],
            ['Requêtes par chaîne', '6 par minute, toutes définitions confondues.'],
            ['Requêtes par définition', '30 par minute.'],
            ["Requêtes par hôte d'API", '120 par minute, comptées sur toutes les chaînes pointant vers cet hôte.'],
            ['Redirections', 'Jusqu’à 3 sauts, chacun restant en https.'],
            ['Un hôte qui échoue en série', 'Cinq échecs de transport d’affilée et cet hôte est mis au repos 60 secondes.'],
            ['«Récupérer un exemple»', 'Environ un test toutes les 10 secondes. Le refus reste en anglais: "Too many test runs. Each one calls the real API. Wait about 10 seconds and try again."'],
          ],
        },
        {
          kind: 'widget',
          name: 'FetchBudget',
          props: { runs: 12 },
          labels: {
            runsLabel: 'Les spectateurs lancent la commande',
            runsUnit: 'fois par minute',
            sourcesLabel: 'Sources de données citées par vos commandes',
            fetchesLabel: 'Requêtes réellement reçues par votre API',
            fetchesUnit: 'par minute',
            cachedLabel: 'Servies par le cache de 30 secondes',
            blockedLabel: 'Refusées par le plafond de 6 par minute',
            blockedLine: 'Ces exécutions affichent [source unavailable] dans le chat.',
            why: "Le cache existe pour votre quota d'API. Sans lui, un seul raid dépenserait un mois d'appels en une soirée, et toutes les autres chaînes pointant vers le même hôte le sentiraient aussi.",
          },
        },
        {
          kind: 'callout',
          tone: 'note',
          html: `
                <b>Note</b>
                Deux secondes de patience, c'est long dans un chat. Si votre API est lente, attendez-vous
                à <code>[source timed out]</code> pendant un direct chargé et écrivez la phrase pour
                qu'elle se lise encore sans la valeur.`,
        },
      ],
    },
    {
      id: 'errors',
      heading: 'Ce que le chat affiche en cas d’échec',
      note: 'Quatre replis, tous courts, tous en anglais où que soit votre chat.',
      blocks: [
        {
          kind: 'prose',
          html: `
            <p>
                Une commande ne devient jamais muette à cause d'une source de données. Le reste de la
                phrase part quand même et la valeur est remplacée par l'une de ces quatre chaînes, ce
                qui vous permet de lire le chat et de savoir ce qui a cassé.
            </p>`,
        },
        {
          kind: 'table',
          head: ["Ce qui s'est passé", 'Ce que le chat affiche'],
          rows: [
            ["L'API a refusé, elle vous a limité, ou le message est la relecture d'un ancien", '<code>[source unavailable]</code>'],
            ["L'API a renvoyé une erreur, ou le chemin n'a rien trouvé d'utilisable", '<code>[source error]</code>'],
            ["L'API a mis plus de temps que le bot n'attend", '<code>[source timed out]</code>'],
            ["La source manque, est en pause, ou le jeton en nomme une jamais enregistrée", 'Le jeton, affiché tel quel: <code>&#123;urlfetch:weather&#125;</code>'],
          ],
        },
        {
          kind: 'widget',
          name: 'FetchOutcomes',
          labels: {
            legend: "Choisissez ce qui a mal tourné",
            title: '#your_channel',
            viewer: 'sesame_sam',
            viewerText: '!weather',
            botName: 'ItsBagelBot',
          },
          props: {
            outcomes: [
              {
                id: 'denied',
                label: "L'API a refusé",
                bot: 'Il fait [source unavailable]°C à Montréal en ce moment.',
                why: "Votre clé a été rejetée, ou le point d'accès a écarté la requête. Testez depuis le tableau de bord et vous obtenez le même verdict: «L'API a refusé la requête. Vérifiez la clé ou l'URL.»",
              },
              {
                id: 'limited',
                label: 'Limite de débit',
                bot: 'Il fait [source unavailable]°C à Montréal en ce moment.',
                why: "Un plafond a été atteint: 6 récupérations par minute pour la chaîne, 30 pour cette définition, ou 120 par minute pour cette API toutes chaînes confondues. Rien n'est cassé et la minute suivante refonctionne.",
              },
              {
                id: 'timeout',
                label: 'Délai dépassé',
                bot: 'Il fait [source timed out]°C à Montréal en ce moment.',
                why: "Le bot attend 3,5 secondes puis parle sans la valeur. Une API lente sous charge en est la raison habituelle.",
              },
              {
                id: 'nopath',
                label: 'La valeur a bougé',
                bot: 'Il fait [source error]°C à Montréal en ce moment.',
                why: "Le chemin était correct mais la réponse n'avait rien au bout, en général parce que l'API a renommé un champ. Ouvrez la source, récupérez un exemple, et recliquez la valeur.",
              },
              {
                id: 'paused',
                label: 'Source en pause',
                bot: 'Il fait {urlfetch:weather}°C à Montréal en ce moment.',
                why: "Une source en pause ou supprimée n'a rien à développer, donc le jeton est affiché tel qu'il est tapé. Le test du tableau de bord le dit clairement: «Définition manquante ou en pause. Le chat affiche le jeton brut tant qu'elle n'est pas active.»",
              },
              {
                id: 'toomany',
                label: 'Trop de sources',
                bot: 'Classement: {urlfetch:standings}',
                why: "Trois sources de données par réponse est la limite de l'éditeur, et il refuse d'en enregistrer une quatrième. Le bot garde son propre plafond à huit et affiche tel quel chaque jeton au-delà, comme celui-ci.",
              },
            ],
          },
        },
        {
          kind: 'callout',
          tone: 'note',
          html: `
                <b>Note</b>
                Ces quatre chaînes ne sont pas traduites. Une chaîne francophone voit elle aussi
                <code>[source unavailable]</code>, ce qui les garde faciles à rechercher et garde cette
                page honnête sur ce que vos spectateurs liront.`,
        },
      ],
    },
    {
      id: 'rules',
      heading: 'Ce qu’une source ne fera pas',
      note: 'Les bords, en un écran, avant de concevoir une commande autour.',
      blocks: [
        {
          kind: 'prose',
          html: `
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
        },
        {
          kind: 'callout',
          tone: 'tip',
          html: `
                <b>Astuce</b>
                Citez deux fois la même source dans une réponse et le bot ne récupère qu'une fois.
                Écrivez <code>&#123;urlfetch:weather&#125;</code> dans la phrase et
                <code>&#123;urlfetch:weather.current.wind_speed_10m&#125;</code> juste après: deux
                valeurs, une requête, une ligne de votre quota.`,
        },
      ],
    },
  ],
};

export default guide;
