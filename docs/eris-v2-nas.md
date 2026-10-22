# Eris v2 sur le NAS — 23 octobre 2026

Déploiement demandé par Hugo dans l'application TrueNAS existante `media-hub`. Le bot est sain et sa connexion Discord a été confirmée après démarrage. Aucune commande ou publication Discord de test n'a été envoyée par l'agent.

## Version active

- API et bot : `eris-concierge:rogue-91aeb1f5f9d2`, code `91aeb1f5f9d23cfb2cd722fffdae1e9023d0ec7b`, image `sha256:4b74498a9b932cc29abe61be2b2a4ac58c8394f146ed1c3121cb38c931ec69aa`.
- Authentification : `eris-auth:siwc-0a36fefe`, image `sha256:2776132d9310f1ebbc06a2cab2c113f704303a5524ab5895a32c476777a7eace`, service issu du commit `696b3d2`.
- Paramètres : `ERIS_PROFILE=v2`, `ERIS_PROTOCOL=responses`, `ERIS_MODEL=gpt-6-luna`, `ERIS_API_URL=https://api.openai.com/v1`.
- Mémoire durable : `/mnt/Main/tank/concierge/eris-character/character.db`, montée sur `/app/eris-data/character.db`.
- Connexion privée : `/mnt/Main/tank/concierge-secrets/eris-siwc`, accessible uniquement à son propriétaire et aux administrateurs. Seul son sous-dossier `tokens` est monté en lecture seule dans le bot.

Cette image remplace les quatre exécutables Go de l'image publiée précédente, compilés depuis le commit indiqué pour Linux amd64 et copiés avec `--chown=65532:65532 --chmod=0555`. La branche reste une PR de travail : ce déploiement ne constitue pas une fusion dans `main`.

Les images et configurations du Hub, du moteur local et du service d'authentification sont conservées. TrueNAS recrée toutefois les conteneurs du groupe pendant une mise à jour. Le moteur local reste disponible pour permettre un retour arrière ; le bot v2 utilise Luna.

## Retours Discord pris en compte

Le profil insiste sur l'imprévisibilité, les emballements, le registre mordant/macabre, les emojis et l'investissement excessif dans les petites affaires du serveur, sans relation amoureuse. Les avis politiques donnent une pirouette nihiliste plutôt qu'une analyse partisane. Le choix personnel entre des films cités reste une discussion ; les recherches de catalogue passent par les outils.

Les demandes v2 se poursuivent en texte : titre ou numéro, rappel du titre/année/saisons, puis « oui, vas-y » ou « annule ». Le modèle ne reçoit pas les jetons des choix. L'API relit le message Discord, vérifie membre/salon/confidentialité/expiration et consomme le jeton une seule fois avant l'envoi. Un changement de sujet ou une réponse à un autre message ne valide pas l'ancienne demande. Les anciens boutons restent utilisables ; leurs réponses v2 permettent ensuite de poursuivre sans boutons.

Le délai de dix secondes ne bloque plus une relance après livraison réussie. La protection contre les générations simultanées reste active. L'outil `monitoring` expose les mesures agrégées CPU, température, RAM, GPU vidéo et stockage, sans identité des spectateurs ni détails d'administration. Une mesure périmée ou manquante est indiquée indisponible.

## Vérifications

La [recette Luna](../evals/eris/dialogues/luna-v2-rogue.json) contient 26 tours sur le NAS, avec mémoire temporaire, outils et livraison simulés. Médiane d'environ 3,13 secondes, hors Discord et outils réels. Les assertions vérifient les relances Seerr/réalisateur, l'outil de mesures et les avis cinéma. Les essais intermédiaires sont conservés ; la réussite mécanique ne valide pas l'humour.

Après déploiement, un essai isolé utilisant le même code et le jeton applicatif vérifie préparation, sélection et confirmation en texte, puis routage vers les mesures. Les résultats des outils restent simulés : aucune demande média ni publication Discord réelle n'est envoyée par ce test. Les sources CPU, température, RAM et stockage du NAS ont également été vérifiées fraîches et disponibles.

Les 2 083 tests locaux et `go vet ./...` passent. La [CI du code déployé](https://github.com/hloiseau/le-concierge/actions/runs/39865106122) est entièrement verte : détecteur de courses, collecteur Python, rotation OAuth, compilation, Compose, image et démarrage isolé API/bot/Hub. Les tests de consentement couvrent notamment choix ambigu, annulation, expiration, autre membre/salon, texte négatif ou conditionnel, réponse à un autre message, double confirmation et continuation depuis un ancien bouton. La rotation OAuth est couverte par les tests du service ; cette recette ne constitue pas une observation d'une rotation réelle à son échéance.

Tous les conteneurs de l'application sont sains, le bot est connecté à Discord, la base de personnage existe et la clé d'autorisation Plex est inchangée. Le serveur de rappel OAuth était temporaire et son port local a été fermé après connexion.

Lors du déploiement initial du 22 octobre, une tentative avait rencontré un binaire sans droit d'exécution ; le retour automatique avait réussi. Les images vérifient depuis explicitement ce droit avant bascule. La mise à jour du 23 octobre n'a pas nécessité de retour arrière. La base principale a passé `quick_check` et son schéma est inchangé.

## Retour arrière

Configuration précédente sauvegardée sur le NAS, avec mode 0600 dans un dossier privé :

`/mnt/Main/tank/concierge/ops-backups/before-eris-rogue-20261022T223044Z.json`

Une copie SQLite cohérente, également privée, accompagne cette configuration : `/mnt/Main/tank/concierge/ops-backups/before-eris-rogue-20261022T223044Z.db`. Le retour de configuration restaure le bot `7228468` et l'API `20f96b6`, sans restaurer automatiquement la base ni effacer les nouvelles données.

Pour la restaurer, transmettre son contenu à `app.update('media-hub', {'custom_compose_config': configuration})` avec le client TrueNAS, puis vérifier les contrôles de santé et la reconnexion Discord. La sauvegarde contient des secrets : ne pas l'afficher, la joindre à une issue ou la copier dans Git. La mémoire et la connexion ChatGPT restent sur disque lors du retour arrière ; aucune suppression automatique de ces données n'est prévue.

## Recette humaine restante

Mentionner Eris ou répondre à l'un de ses messages. Essayer une discussion ordinaire, une provocation, une demande d'arrêt, une lubie à rappeler, un avis cinéma et une demande à confirmer en texte. `/eris oublier:true` permet de tester l'oubli. Le ton reste variable et certaines piques demeurent sages : le retour humain sur le naturel, la variété et l'intensité reste nécessaire.
