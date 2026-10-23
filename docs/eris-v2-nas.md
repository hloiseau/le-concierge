# Eris v2 sur le NAS — 23 octobre 2026

Déploiement demandé par Hugo dans l'application TrueNAS existante `media-hub`. Le bot est sain et sa connexion Discord a été confirmée après démarrage. Aucune commande ou publication Discord de test n'a été envoyée par l'agent.

## Version active

- API et bot : `eris-concierge:capabilities-4bf51ffe3281`, code `4bf51ffe32810fcb3e51b0c6d7f869f6de6452c5`, image `sha256:e6f7af9379da7ac997d63f57bf6047fcf03c9c46fff9f5363de467e24cea0c25`.
- Authentification : `eris-auth:siwc-0a36fefe`, image `sha256:2776132d9310f1ebbc06a2cab2c113f704303a5524ab5895a32c476777a7eace`, service issu du commit `696b3d2`.
- Paramètres : `ERIS_PROFILE=v2`, `ERIS_PROTOCOL=responses`, `ERIS_MODEL=gpt-6-luna`, `ERIS_API_URL=https://api.openai.com/v1`.
- Mémoire durable : `/mnt/Main/tank/concierge/eris-character/character.db`, montée sur `/app/eris-data/character.db`.
- Connexion privée : `/mnt/Main/tank/concierge-secrets/eris-siwc`, accessible uniquement à son propriétaire et aux administrateurs. Seul son sous-dossier `tokens` est monté en lecture seule dans le bot.

L'image dérive de `organic-c37c13553174` et remplace `/app/api` et `/app/bot`, compilés depuis le commit indiqué pour Linux amd64 avec `--chown=65532:65532 --chmod=0555`. Empreintes : API `1d7dba754eb120b27565bb6d17d3fbed1097c2d9ec7dfe86752752c6f23d8b5b`, bot `e893e5480a299eb803293a4b625012a1b8f31a3d855bdd5b00b5520cc0692fd6`. La branche reste une PR de travail : ce déploiement ne constitue pas une fusion dans `main`.

Les images et configurations du Hub, du moteur local et du service d'authentification sont conservées. TrueNAS recrée toutefois les conteneurs du groupe pendant une mise à jour. Le moteur local reste disponible pour permettre un retour arrière ; le bot v2 utilise Luna.

## Retours Discord pris en compte

Le profil ajoute une direction inspirée de SHODAN : assurance démesurée, cruauté fictive, comédie horrifique et contraste entre chaleur, calme et explosions. La [recherche sourcée](eris-shodan-research.md) et les [réponses réelles de Luna](../evals/eris/shodan-comparison.md) distinguent cette intention des résultats observés. Les exemples sont originaux ; pas de romance ni de menace à chaque tour. Les avis politiques gardent la pirouette nihiliste ; le choix personnel entre films reste une discussion. La consigne des outils précise que « du même réalisateur » conserve le dernier catalogue demandé.

Les [demandes v2](eris-natural-requests.md) se poursuivent sans double validation : une demande explicite avec résultat unique exact est envoyée directement ; sinon, sélectionner le titre, son numéro ou « celui de 2021 » suffit. Une saison manquante reste une question. Le modèle ne reçoit pas les jetons des choix. L'API relit le message Discord, vérifie membre/salon/confidentialité/expiration et consomme les choix de la proposition avant l'envoi. Un changement de sujet ou une réponse à un autre message ne valide pas l'ancienne demande. Les anciens boutons et confirmations restent compatibles.

La suppression reste indisponible : Eris explique immédiatement qu'elle ne peut ni annuler une demande déjà envoyée dans Seerr ni supprimer un média Plex. Ce parcours ne demande pas de titre, ne propose pas d'ajout et ne renvoie pas en privé. Un garde-fou dans le chat et l'API intercepte les formulations explicites ; la consigne conserve l'intention dans les relances. Le style demande des expressions naturelles et des images compréhensibles, avec une correction simple après une erreur.

Le délai de dix secondes ne bloque plus une relance après livraison réussie. La protection contre les générations simultanées reste active. L'outil `monitoring` expose les mesures agrégées CPU, température, RAM, GPU vidéo et stockage, sans identité des spectateurs ni détails d'administration. Une mesure périmée ou manquante est indiquée indisponible.

## Vérifications

Le [bilan du réglage SHODAN précédent](../evals/eris/shodan-comparison.md) conserve tous ses essais, avec mémoire temporaire, outils et livraison simulés. Sur son prompt complet : 40 réponses sur 41 appels, médiane 2,88 s, maximum 15,23 s, hors Discord/outils. Les assertions Seerr/réalisateur, monitoring et avis cinéma passent, et les 15 tours de réserve réussissent. Un appel échoue malgré HTTP 200 ; huit reproductions directes puis les quatre tours concernés dans le vrai Chat Go réussissent, sans en établir la cause. Deux HTTP 503 ont aussi été identifiés lors d'un essai précédent. Ces erreurs restent visibles dans les rapports ; aucune réussite générale du laboratoire n'est revendiquée.

Lors du déploiement précédent `c37c135`, les essais avant et après bascule validaient préparation puis sélection par année en deux messages, sans confirmation supplémentaire. Dans les deux essais, le contrôle annexe de monitoring échouait malgré HTTP 200 et sans erreur de génération : aucun plan monitoring n'était exécuté. Ce routage demeure non résolu et n'a pas été réévalué dans le contrôle ciblé de cette correction.

Pour `4bf51ff`, le [rejeu suppression et style](eris-natural-requests.md#régression-suppression-et-style--23-octobre-2026) conserve 14 tours : 13 réponses et un HTTP 503, suivi d'un contrôle ciblé réussi de deux tours. Aucun outil média n'est exécuté dans le scénario de suppression. Le contrôle de l'image avant puis après déploiement réussit : cinq tours sur la suppression et deux tours de préparation/sélection d'un ajout normal, en 5,51 s puis 5,13 s. Les outils sont simulés : aucune demande média, suppression ou publication Discord réelle n'est envoyée.

Les 2 111 tests locaux et `go vet ./...` passent. La [CI du code déployé](https://github.com/hloiseau/le-concierge/actions/runs/39984270892) est entièrement verte : détecteur de courses, collecteur Python, rotation OAuth, compilation, Compose, image et démarrage isolé API/bot/Hub. Les tests couvrent les garde-fous de suppression, leurs relances, ainsi que titre exact, année, homonymes, saison explicite/modifiée/manquante, texte négatif ou conditionnel, révocation, choix par année, annulation de toute la proposition, concurrence et POST incertain. Les anciennes confirmations restent testées. La rotation OAuth est couverte par les tests du service ; cette recette ne constitue pas une observation d'une rotation réelle à son échéance.

Tous les conteneurs de l'application sont sains, le bot est connecté à Discord, la base de personnage existe et la clé d'autorisation Plex est inchangée. Le serveur de rappel OAuth était temporaire et son port local a été fermé après connexion.

Lors du déploiement initial du 22 octobre, une tentative avait rencontré un binaire sans droit d'exécution ; le retour automatique avait réussi. Les images vérifient depuis explicitement ce droit avant bascule. La mise à jour du 23 octobre n'a pas nécessité de retour arrière. La base principale a passé `quick_check` et son schéma est inchangé.

## Retour arrière

Configuration précédente sauvegardée sur le NAS, avec mode 0600 dans un dossier privé :

`/mnt/Main/tank/concierge/ops-backups/before-eris-capabilities-20261023T161112Z.json`

Une copie SQLite cohérente, également privée, accompagne cette configuration : `/mnt/Main/tank/concierge/ops-backups/before-eris-capabilities-20261023T161112Z.db`. Le retour de configuration restaure le bot et l'API `c37c135`, sans restaurer automatiquement la base ni effacer les nouvelles données.

Pour la restaurer, transmettre son contenu à `app.update('media-hub', {'custom_compose_config': configuration})` avec le client TrueNAS, puis vérifier les contrôles de santé et la reconnexion Discord. La sauvegarde contient des secrets : ne pas l'afficher, la joindre à une issue ou la copier dans Git. La mémoire et la connexion ChatGPT restent sur disque lors du retour arrière ; aucune suppression automatique de ces données n'est prévue.

## Recette humaine restante

La demande de Hugo pour une Eris **beaucoup plus hardcore**, inspirée notamment de **SHODAN**, a été reprise dans cette version. Le macabre est plus marqué dans les joutes explicites, mais certaines réponses restent sages et les démonstrations peuvent encore tourner à l'objet aléatoire. La variété, l'imprévisibilité, le côté serviable et l'absence de romance restent les critères de recette humaine.

Mentionner Eris ou répondre à l'un de ses messages. Essayer une discussion ordinaire, une provocation, une demande d'arrêt, une lubie à rappeler, un avis cinéma, une demande précise et une sélection parmi plusieurs titres. `/eris oublier:true` permet de tester l'oubli. Le ton reste variable et certaines piques demeurent sages : le retour humain sur le naturel, la variété et l'intensité reste nécessaire.
