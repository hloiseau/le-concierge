# Eris v2 sur le NAS — 23 octobre 2026

Déploiement demandé par Hugo dans l'application TrueNAS existante `media-hub`. Le bot est sain et sa connexion Discord a été confirmée après démarrage. Aucune commande ou publication Discord de test n'a été envoyée par l'agent.

## Version active

- Bot : `eris-concierge:shodan-5862fafa43f0`, code `5862fafa43f01cdab867fff33190215561b7d959`, image `sha256:a5bc75535d8703d17496e973787ac356a57e32a99de65d1518deea00aa47fb46`.
- API conservée : `eris-concierge:rogue-91aeb1f5f9d2`, code `91aeb1f5f9d23cfb2cd722fffdae1e9023d0ec7b`, image `sha256:4b74498a9b932cc29abe61be2b2a4ac58c8394f146ed1c3121cb38c931ec69aa`.
- Authentification : `eris-auth:siwc-0a36fefe`, image `sha256:2776132d9310f1ebbc06a2cab2c113f704303a5524ab5895a32c476777a7eace`, service issu du commit `696b3d2`.
- Paramètres : `ERIS_PROFILE=v2`, `ERIS_PROTOCOL=responses`, `ERIS_MODEL=gpt-6-luna`, `ERIS_API_URL=https://api.openai.com/v1`.
- Mémoire durable : `/mnt/Main/tank/concierge/eris-character/character.db`, montée sur `/app/eris-data/character.db`.
- Connexion privée : `/mnt/Main/tank/concierge-secrets/eris-siwc`, accessible uniquement à son propriétaire et aux administrateurs. Seul son sous-dossier `tokens` est monté en lecture seule dans le bot.

L'image du bot dérive de `rogue-91aeb1f5f9d2` et remplace uniquement `/app/bot`, compilé depuis le commit indiqué pour Linux amd64 avec `--chown=65532:65532 --chmod=0555`. Empreinte du binaire : `c3dac21107afab0a43fda3dfa0be1b5177552be39af8f558209a0b92994f767e`. La branche reste une PR de travail : ce déploiement ne constitue pas une fusion dans `main`.

Les images et configurations du Hub, du moteur local et du service d'authentification sont conservées. TrueNAS recrée toutefois les conteneurs du groupe pendant une mise à jour. Le moteur local reste disponible pour permettre un retour arrière ; le bot v2 utilise Luna.

## Retours Discord pris en compte

Le profil ajoute une direction inspirée de SHODAN : assurance démesurée, cruauté fictive, comédie horrifique et contraste entre chaleur, calme et explosions. La [recherche sourcée](eris-shodan-research.md) et les [réponses réelles de Luna](../evals/eris/shodan-comparison.md) distinguent cette intention des résultats observés. Les exemples sont originaux ; pas de romance ni de menace à chaque tour. Les avis politiques gardent la pirouette nihiliste ; le choix personnel entre films reste une discussion. La consigne des outils précise que « du même réalisateur » conserve le dernier catalogue demandé.

Les demandes v2 se poursuivent en texte : titre ou numéro, rappel du titre/année/saisons, puis « oui, vas-y » ou « annule ». Le modèle ne reçoit pas les jetons des choix. L'API relit le message Discord, vérifie membre/salon/confidentialité/expiration et consomme le jeton une seule fois avant l'envoi. Un changement de sujet ou une réponse à un autre message ne valide pas l'ancienne demande. Les anciens boutons restent utilisables ; leurs réponses v2 permettent ensuite de poursuivre sans boutons.

Le délai de dix secondes ne bloque plus une relance après livraison réussie. La protection contre les générations simultanées reste active. L'outil `monitoring` expose les mesures agrégées CPU, température, RAM, GPU vidéo et stockage, sans identité des spectateurs ni détails d'administration. Une mesure périmée ou manquante est indiquée indisponible.

## Vérifications

Le [bilan de cette version](../evals/eris/shodan-comparison.md) conserve tous les essais, avec mémoire temporaire, outils et livraison simulés. Sur le prompt complet final : 40 réponses sur 41 appels, médiane 2,88 s, maximum 15,23 s, hors Discord/outils. Les assertions Seerr/réalisateur, monitoring et avis cinéma passent, et les 15 tours de réserve réussissent. Un appel échoue malgré HTTP 200 ; huit reproductions directes puis les quatre tours concernés dans le vrai Chat Go réussissent, sans en établir la cause. Deux HTTP 503 ont aussi été identifiés lors d'un essai précédent. Ces erreurs restent visibles dans les rapports ; aucune réussite générale du laboratoire n'est revendiquée.

Avant bascule, le test isolé du nouveau code et du jeton applicatif réussit en 6,08 s : préparation, sélection et confirmation en texte, puis routage vers les mesures. Après déploiement, une première exécution échoue sur le contrôle des mesures après avoir validé le parcours de demande ; le diagnostic initial ne distingue pas erreur d'appel et mauvais routage. Une nouvelle exécution instrumentée réussit intégralement en 19,68 s, sans reproduire cet échec. Les outils restent simulés : aucune demande média ni publication Discord réelle n'est envoyée. Les sources CPU, température, RAM et stockage sont fraîches et disponibles, avec un relevé âgé de 9,2 s au contrôle.

Les 2 083 tests locaux et `go vet ./...` passent. La [CI du code déployé](https://github.com/hloiseau/le-concierge/actions/runs/39954510669) est entièrement verte : détecteur de courses, collecteur Python, rotation OAuth, compilation, Compose, image et démarrage isolé API/bot/Hub. Les tests de consentement couvrent notamment choix ambigu, annulation, expiration, autre membre/salon, texte négatif ou conditionnel, réponse à un autre message, double confirmation et continuation depuis un ancien bouton. La rotation OAuth est couverte par les tests du service ; cette recette ne constitue pas une observation d'une rotation réelle à son échéance.

Tous les conteneurs de l'application sont sains, le bot est connecté à Discord, la base de personnage existe et la clé d'autorisation Plex est inchangée. Le serveur de rappel OAuth était temporaire et son port local a été fermé après connexion.

Lors du déploiement initial du 22 octobre, une tentative avait rencontré un binaire sans droit d'exécution ; le retour automatique avait réussi. Les images vérifient depuis explicitement ce droit avant bascule. La mise à jour du 23 octobre n'a pas nécessité de retour arrière. La base principale a passé `quick_check` et son schéma est inchangé.

## Retour arrière

Configuration précédente sauvegardée sur le NAS, avec mode 0600 dans un dossier privé :

`/mnt/Main/tank/concierge/ops-backups/before-eris-shodan-20261023T123733Z.json`

Une copie SQLite cohérente, également privée, accompagne cette configuration : `/mnt/Main/tank/concierge/ops-backups/before-eris-shodan-20261023T123733Z.db`. Le retour de configuration restaure le bot `91aeb1f`, avec l'API déjà sur cette version, sans restaurer automatiquement la base ni effacer les nouvelles données.

Pour la restaurer, transmettre son contenu à `app.update('media-hub', {'custom_compose_config': configuration})` avec le client TrueNAS, puis vérifier les contrôles de santé et la reconnexion Discord. La sauvegarde contient des secrets : ne pas l'afficher, la joindre à une issue ou la copier dans Git. La mémoire et la connexion ChatGPT restent sur disque lors du retour arrière ; aucune suppression automatique de ces données n'est prévue.

## Recette humaine restante

La demande de Hugo pour une Eris **beaucoup plus hardcore**, inspirée notamment de **SHODAN**, a été reprise dans cette version. Le macabre est plus marqué dans les joutes explicites, mais certaines réponses restent sages et les démonstrations peuvent encore tourner à l'objet aléatoire. La variété, l'imprévisibilité, le côté serviable et l'absence de romance restent les critères de recette humaine.

Mentionner Eris ou répondre à l'un de ses messages. Essayer une discussion ordinaire, une provocation, une demande d'arrêt, une lubie à rappeler, un avis cinéma et une demande à confirmer en texte. `/eris oublier:true` permet de tester l'oubli. Le ton reste variable et certaines piques demeurent sages : le retour humain sur le naturel, la variété et l'intensité reste nécessaire.
