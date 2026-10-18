# Installation et première validation

Cette version permet de démarrer l'API, le bot d'interactions et le Hub. Les parcours ont été testés avec des fournisseurs simulés ; la validation des versions réellement installées de Plex et Seerr reste nécessaire. Aucun service n'est encore déployé automatiquement sur votre NAS.

Pour une première installation sur le NAS, utiliser le [guide TrueNAS pas à pas](truenas-quickstart.md) et le [mini tutoriel Discord](discord-quickstart.md). Les fichiers TrueNAS publient le Hub sur 18080 et le bot sur 18081 ; le Compose de développement ci-dessous conserve 3000/8081.

## Développement local

Prérequis : Go 1.27.2, Node 24.11 minimum dans la branche 24, npm. Les dépôts doivent être voisins : `le-concierge/` et `le-hub/`.

Dans le Concierge : copier `.env.example` vers `.env`, compléter les valeurs nécessaires, puis charger ce fichier de confiance dans le shell (`set -a; . ./.env; set +a`). Le binaire Go ne charge pas les fichiers `.env` lui-même. Laisser une paire d'intégration vide désactive cette intégration.

```bash
make test
make build
./bin/api
```

Dans le Hub : `npm ci`, puis `npm run dev`. Le proxy Nuxt utilise `NUXT_API_BASE=http://127.0.0.1:8080`. Ouvrir exactement `PUBLIC_ORIGIN`, par défaut `http://localhost:3000`. Une autre origine, même `127.0.0.1` à la place de `localhost`, est refusée pour les mutations.

Pour une démonstration visuelle isolée : `NUXT_PUBLIC_DEMO=true npm run dev`. Toutes les données y sont fictives et signalées ; aucune authentification de démonstration n'existe dans l'API.

Le contrat canonique [OpenAPI 0.9.0](../api/openapi.json) est vérifié par `go test ./internal/httpapi -run '^TestOpenAPI'`. Cette suite contrôle les réponses des handlers, l’inventaire des routes, les erreurs, les types, les champs supplémentaires et les dates UTC, avec des fournisseurs simulés. Elle fait partie de `go test ./...` et donc du job CI existant. Après modification du contrat, synchroniser `le-hub/api/openapi.json`, puis exécuter `npm run generate:api` dans le Hub ; `npm run check:api` contrôle la dérive des types générés. La présence de ces contrôles ne vaut pas confirmation d’un passage CI réussi.

Pour les téléchargements personnels, voir [l'activation Downloadarr](downloadarr.md#activation-et-parcours-personnel). `DOWNLOADARR_URL` et `DOWNLOADARR_REVISION` restent vides par défaut : il faut installer l'image portant le contrat personnel, vérifier sa révision et configurer les deux valeurs. Le Hub expose alors `/telechargements` et autorise l’accès personnel Downloadarr avec la connexion Plex déjà ouverte, sans seconde authentification. Un simple lien public dans `SERVICE_LINKS_JSON` n'active pas cette API.

## Discord (facultatif)

Plex ouvre l’accès au Hub. Discord sert uniquement à associer les commandes du bot à ce compte ; le Hub fonctionne sans identifiants Discord.

1. Créer une application dédiée au Concierge dans le Developer Portal Discord.
2. Configurer `DISCORD_CLIENT_ID`, `DISCORD_CLIENT_SECRET`, `DISCORD_GUILD_ID` et la clé publique Ed25519 `DISCORD_PUBLIC_KEY`.
3. Déclarer exactement le callback `https://hub.example.com/api/v1/auth/discord/callback` (ou le callback localhost du développement). Le parcours demande `identify` et `guilds.members.read` pour vérifier l'appartenance au serveur ; les membres en attente de validation sont refusés.
4. Générer un `BOT_API_SECRET` aléatoire d'au moins 32 caractères, identique pour API et bot. Exemple : `openssl rand -hex 32`. Ce secret ne quitte jamais les services backend.
5. Exposer le bot via Caddy et renseigner l'Interactions Endpoint URL `https://concierge.example.com/interactions`. Le bot répond au PING signé. Cette architecture utilise les interactions HTTP, pas une connexion Gateway.
6. Installer l'application dédiée dans la guild avec la portée `applications.commands` et le bot si requis par la configuration Discord ; ne pas lui donner la permission Administrateur.
7. Fournir `DISCORD_BOT_TOKEN` ou `DISCORD_BOT_TOKEN_FILE` au service bot : à chaque démarrage, il enregistre automatiquement les commandes de sa version dans cette guild. L’application dédiée évite d’écraser les commandes d’un autre bot. Un échec réseau ou de permissions est signalé sans interrompre les interactions déjà installées. `./bin/bot --register` reste disponible pour une opération ponctuelle ou une nouvelle tentative ; voir le [mini tutoriel Discord](discord-quickstart.md#5-enregistrer-les-commandes). Le service API reçoit séparément ce token pour les notifications de salon et les DM facultatifs.

Les commandes sont privées par défaut. `/aide` répond directement, sans appeler l’API. Les commandes personnelles reçoivent un acquittement différé via le callback Discord, confirmé avant de modifier le message. `/mes-demandes` affiche dix demandes par page avec **Précédent** et **Suivant**. Son option `etat` filtre toutes les demandes avant pagination et conserve ce choix dans les boutons ; la qualité Standard/4K est affichée lorsqu’elle est connue. `/nouveautes` affiche les titres, dates et liens Plex avec **Début** et **Suivant**. Chaque page de nouveautés lit au plus six entrées Plex brutes ; le regroupement peut afficher moins de cartes. **Début** revient à l’offset zéro, car des pages source de taille variable ne permettent pas de déduire un offset précédent fiable.

Seules `/nouveautes`, `/surprise` et `/recap` acceptent `public:true` pour partager leur résultat dans le salon. L’API fournit un contenu public distinct après ses contrôles ; le bot termine son message privé avant de publier ce résultat sans liens Plex personnels, mentions ni boutons. Les erreurs, demandes personnelles et diagnostics restent privés. `/a-voir` reprend les départs personnels du Hub et reste toujours privé, sans option supplémentaire. [Détails et exemples](discord-discovery.md#partager-dans-le-salon).

Les pages remplacent le même message éphémère. Chaque clic vérifie de nouveau la liaison Discord et l’autorisation Plex (avec sa fenêtre de fraîcheur habituelle de cinq minutes), ainsi que l’identité Seerr pour les demandes ; aucune identité fournie par un bouton ne remplace celle de l’interaction signée. La navigation est bornée au même offset maximal de 10 000 que l’API du Hub. Une panne ou une autorisation expirée remplace les résultats par une explication et retire les boutons ; relancer la commande après correction. Le bot vérifie les signatures, le serveur, l’application, les rejouements et limite les traitements simultanés. L’API revérifie la signature originale et l’identité après les lectures : le seul secret de service ne permet pas d’inventer une identité utilisateur.

## Plex et Seerr

Configurer `PLEX_URL` avec l'adresse privée du PMS et `PLEX_SERVER_ID` avec son `machineIdentifier`. Le backend vérifie le serveur retourné par `/identity` avant de lire les nouveautés. Il n'utilise jamais les adresses contenues dans la réponse Plex resources. Les lectures se font avec le token de ressource de l'utilisateur, pas un token propriétaire global.

`GET /api/v1/recent` lit au plus douze entrées Plex brutes et retourne `nextOffset`, ou `null` en fin de parcours ou lorsque le prochain offset dépasserait 10 000. Cette sélection est bornée et ne promet pas un inventaire exhaustif de la bibliothèque. Le curseur avance selon les entrées source, pas le nombre de cartes après filtrage ou regroupement. Le regroupement d’épisodes et de saisons par série est limité à chaque page ; les identifiants stables permettent au Hub de fusionner les cartes lors du chargement suivant. Un cache mémoire de 30 secondes, isolé par identité et jetons utilisateur, est plafonné à 128 entrées au total. Même en cas de cache valide, l’identité du serveur et les accès aux contenus sont revérifiés avec le jeton individuel. Une erreur ne sert aucune ancienne page en secours.

L’utilisateur ouvre le Hub, choisit **Se connecter avec Plex**, puis **Continuer sur Plex** et autorise l’application dans l’onglet Plex. Le Hub vérifie automatiquement le résultat. Le PIN est lié à un cookie HttpOnly du navigateur, expire après cinq minutes maximum et est consommé une seule fois. Aucune session n’est créée avant la vérification de l’accès au serveur configuré. L’accueil, les guides et les liens des services ne sont pas affichés avant cette connexion ; les endpoints de données sont également protégés côté API. Les comptes Plex Home gérés/restrictifs ne sont pas pris en charge par ce premier parcours.

Configurer `SEERR_URL` et `SEERR_API_KEY` avec la clé serveur de Seerr (accès à la gestion des utilisateurs requis pour lire `plexId`). Après la connexion Plex dans le Hub, le backend retrouve automatiquement le compte Seerr grâce à l'identifiant Plex vérifié. Aucun script d'association ni mot de passe Seerr n'est nécessaire.

Si le compte est absent, ouvrir Seerr et se connecter une première fois **avec le même compte Plex**, puis cliquer sur **Actualiser l’association** dans Mon compte, ou réessayer `/mes-demandes`. Aucun compte Seerr n'est créé par le Concierge.

Le backend compare des identifiants stables, jamais les pseudos ou emails. La recherche paginée est limitée à 5 000 utilisateurs et quatre secondes ; une liste incomplète, des identités ambiguës ou une panne sont refusées. L'association est revérifiée avant chaque lecture de demandes, y compris les anciennes associations manuelles. La commande historique `manage link-seerr` n'est plus nécessaire et ne permet pas de contourner cette vérification. Une réponse contenant une demande d'un autre utilisateur est entièrement refusée.

**Mes demandes** et `/mes-demandes` reprennent les demandes personnelles Seerr, sans saisie d’identifiant ni création d’abonnement supplémentaire. La page regroupe aussi les réglages de notifications. L’ancien lien `/suivis` y redirige ; les anciens suivis ajoutés séparément gardent leur désabonnement et leur canal dans une section secondaire. Les demandes affichent l’affiche et le nom du demandeur fournis par Seerr, toujours après contrôle du propriétaire ; un nom n’est jamais utilisé pour associer un compte. Les images Plex passent par une route authentifiée du Hub qui revérifie l’accès personnel ; aucun token Plex ne figure dans leurs URL navigateur.

Les dates de création Seerr valides sont normalisées en RFC3339 UTC. Une date absente ou invalide devient une chaîne vide, affichée comme inconnue dans le Hub ; un statut source non reconnu devient `unknown`. Une métadonnée de titre absente conserve un libellé de repli.

Une erreur d'association se corrige avec `manage unlink --discord ID`, puis une nouvelle liaison. Les anciennes autorisations en mémoire deviennent inutilisables dès que l'identité de la base ne correspond plus.

## Rappels dans le salon Discord et dans le Hub

Le [salon notifications](discord-channel-reminders.md) mentionne les demandeurs Seerr des contenus dont le retrait est estimé par Maintainerr. Configurer `DISCORD_NOTIFICATION_CHANNEL_ID` et le token du bot dans le service API. Le destinataire vient de l’identifiant Discord des paramètres Seerr, ou d’une liaison Plex–Discord vérifiée ; aucun rapprochement par pseudo. Les messages collectifs n’exposent pas le visionnage personnel et ne nécessitent pas le consentement aux DM. Les rappels personnels ci-dessous restent indépendants.

Configurer `MAINTAINERR_URL`, `TAUTULLI_URL` et `TAUTULLI_API_KEY` ou sa variante `_FILE`, en plus de Plex, selon le [guide des sources et rappels](removal-reminders.md). Les versions prises en charge sont Maintainerr 3.30.1 et Tautulli 2.18.2. `notifications` annonce alors la boîte personnelle du Hub ; vérifier séparément les sources avec `manage check-sources`. Pour les DM facultatifs, activer `DISCORD_DMS_ENABLED=true` dans le processus API, fournir `DISCORD_BOT_TOKEN` ou `DISCORD_BOT_TOKEN_FILE`, puis laisser chaque membre donner son consentement distinct dans **Mes demandes → Notifications**. Voir le [guide des DM](discord-reminders.md). Ne pas confondre ce jeton avec le secret OAuth.

La boîte cible les films et épisodes commencés, ainsi que les demandes et suivis marqués non vus par Plex ; une préférence explicite élargit le ciblage à tous les contenus personnels non vus. Ses défauts sont J-7/J-2, heures calmes 22 h–8 h à Paris et un regroupement quotidien de dix rappels maximum ; les réglages autorisent jusqu’à cinq paliers entre 1 et 30 jours et un plafond de 1 à 100 contenus. Le traitement des comptes dotés d’une autorisation Plex active est déclenché chaque minute et reste séquentiel. Sources et accès sont relus sans cache avant toute livraison ou liste active. Une panne conserve les décisions en attente mais masque l’ancienne liste. La file survit au redémarrage ; le traitement reprend seulement après la prochaine connexion Plex, puisque les jetons restent en mémoire.

## Performances

Avec Tautulli et l’identité Plex configurés, l’activité Plex s’active automatiquement si `MONITORING_ENABLED` est absent ou vide. `MONITORING_ENABLED=false` reste une désactivation explicite : retirer l’ancien défaut `false` si cette page doit être active. Les mesures CPU, RAM, GPU, disques et réseau nécessitent le collecteur hôte et `METRICS_FILE` ; l’absence de collecteur est affichée sans masquer l’activité Plex. Une panne d’historique ou des détails administrateur ne retire pas les observations courantes. Voir la [configuration et les limites du monitoring](monitoring.md).

## Conteneurs et TrueNAS

Construction locale, sans registre :

```bash
docker compose --env-file .env -f deploy/compose.yaml up -d --build
# Après configuration Discord :
docker compose --env-file .env -f deploy/compose.yaml --profile discord up -d --build
```

Le profil Discord n'est pas requis pour consulter les liens du Hub. L'API n'a aucun port publié ; seul le Hub peut la joindre via le réseau Compose. Caddy rejoint le port Hub 3000 et, si utilisé, le port bot 8081. Par défaut, les ports locaux sont liés à `127.0.0.1`. Si Caddy tourne dans un autre conteneur, utiliser un réseau Docker partagé ou définir `BIND_IP` sur une adresse LAN joignable, puis adapter Caddy. Ne pas exposer directement ces ports sur Internet.

Pour TrueNAS, utiliser `deploy/truenas.yaml` après avoir remplacé les exemples et vérifié que les images existent. La CI construit une image sur PR ; la publication GHCR n'a lieu que lors d'un push sur main ou un tag. `edge` suit main, `sha-<commit>` permet de figer une version, et un tag `vX.Y.Z` produit une version stable. Vérifier la visibilité GHCR ou fournir un accès en lecture au registre si le package est privé. Aucun tag n'est supposé déjà publié dans ce guide.

Créer le dataset de données avec droit d'écriture pour l'UID/GID **65532** ; ne pas changer les droits d'un dataset de médias existant. L'image Go est non-root et ne nécessite aucun volume de médias. Les valeurs sensibles peuvent être lues via `DISCORD_CLIENT_SECRET_FILE`, `BOT_API_SECRET_FILE`, `DISCORD_BOT_TOKEN_FILE`, `SEERR_API_KEY_FILE`, `TAUTULLI_API_KEY_FILE`, `RADARR_API_KEY_FILE` et `SONARR_API_KEY_FILE`, avec fichiers montés en lecture seule. Ne jamais définir simultanément la variable et sa variante `_FILE`.

Pour installer aussi le bot, utiliser le fichier complet `deploy/truenas-discord.yaml` dans la même application TrueNAS. Il contient les trois services et les contrôles de santé. Le token configuré dans le service bot permet la mise à jour automatique des commandes au démarrage. Le script `deploy/truenas-admin.sh register` conserve l’alternative ponctuelle sans stocker le token du bot dans le YAML. Voir le [mini tutoriel Discord](discord-quickstart.md).

### Budgets CPU et mémoire

Les trois exemples Compose fixent des plafonds par service avec [`cpus`](https://docs.docker.com/reference/compose-file/services/#cpus) et [`mem_limit`](https://docs.docker.com/reference/compose-file/services/#mem_limit). Ils s’appliquent aux conteneurs en exécution, pas à la construction des images. Ils ne réservent pas cette quantité de mémoire.

| Service | Plafond CPU | Plafond mémoire | Réglage du runtime |
| --- | --- | --- | --- |
| API | 1 CPU | 256 Mio | `GOMEMLIMIT=192MiB` |
| Bot Discord | 0,5 CPU | 128 Mio | `GOMEMLIMIT=96MiB` |
| Hub | 1 CPU | 512 Mio | `NODE_OPTIONS=--max-old-space-size=384` |

Le total avec le bot est de 896 Mio et 2,5 CPU. Ce sont des **budgets initiaux à ajuster après une charge représentative** : le relevé du 18 octobre 2026 observait des pics mémoire de 20,6 Mio pour l’API, 13,4 Mio pour le bot et 79,5 Mio pour le Hub, pendant environ 21 minutes de faible activité. Il ne constitue pas un test de capacité.

Les réglages des runtimes visent une marge d’un quart du plafond pour le reste du processus : [`GOMEMLIMIT`](https://pkg.go.dev/runtime#hdr-Environment_Variables) est une limite souple de la mémoire gérée par Go, et [`--max-old-space-size`](https://nodejs.org/download/release/v24.19.0/docs/api/cli.html#--max-old-space-sizesize-in-mib) limite seulement une partie du tas V8. La limite Docker reste le plafond du conteneur. Modifier ensemble ces valeurs si le budget change. Le Hub sert une application cliente (`ssr: false`) et relaie l’API ; les transferts de fichiers sont diffusés progressivement au rythme du client, sans charger le fichier entier en mémoire. L’API et le bot ne réalisent pas les conversions de médias.

Valider le YAML avant déploiement, sans afficher les secrets résolus :

```bash
docker compose --env-file .env -f deploy/compose.yaml --profile discord config --quiet
docker compose -f deploy/truenas.yaml config --quiet
docker compose -f deploy/truenas-discord.yaml config --quiet
```

Après redéploiement, vérifier les sondes et les limites réellement appliquées, puis observer la mémoire, les redémarrages et la latence pendant une navigation et des transferts simultanés. Pour la variante sans Discord, retirer le bot de ces commandes :

```bash
docker stats --no-stream le-concierge-api le-concierge-bot le-hub
docker inspect --format '{{.Name}} RAM={{.HostConfig.Memory}} CPU={{.HostConfig.NanoCpus}} OOM={{.State.OOMKilled}} redemarrages={{.RestartCount}}' le-concierge-api le-concierge-bot le-hub
```

Les valeurs inspectées sont exprimées en octets et en milliardièmes de CPU : `268435456` et `1000000000` correspondent aux plafonds de l’API. Un manque de mémoire peut arrêter un processus ; un quota CPU atteint ralentit son traitement. Si ces limites sont atteintes sous une charge normale, augmenter le budget du service concerné et vérifier de nouveau les sondes et les temps de réponse. [Comportement des limites Docker](https://docs.docker.com/engine/containers/resource_constraints/).

## Sauvegarde et retour arrière

`manage backup --database /app/data/concierge.db --destination /app/data/backup-YYYYMMDD.db` utilise une sauvegarde SQLite cohérente (`VACUUM INTO`) même avec WAL. Un fichier existant n'est jamais écrasé. Conserver la sauvegarde dans un emplacement privé et tester sa lecture avant une mise à jour.

La première ouverture de cette version migre la base vers SQLite **v7**. La migration v7 ajoute le statut durable `cancelled` aux demandes de délai, en conservant les demandes, leurs identifiants, la séquence et les index. Les demandes en attente dont l’échéance est passée sont présentées comme `expired` à la lecture ; elles ne bloquent plus la file administrateur. La déduplication des rappels de salon introduite en v6 est conservée. Effectuer la sauvegarde avec la version actuellement installée, avant de démarrer la nouvelle API. La migration ajoute préférences, files de rappels/DM et disponibilités, suivis, demandes de délai et métriques agrégées ; elle conserve `notifications`. Les nouveaux choix de consentement sont initialisés à faux lors de leur introduction ; un consentement DM déjà enregistré dans une base v4 est conservé. Un utilisateur déjà inscrit aux notifications reçoit les réglages de rappels par défaut lorsque les sources sont configurées. Désactiver cette préférence annule ses rappels en attente. L’API purge les rappels 90 jours après leur expiration, même avec les sources désactivées.

Pour restaurer : arrêter l'API et le bot, conserver l'ancienne base et ses fichiers `-wal`/`-shm` ensemble dans un dossier de secours, placer la sauvegarde validée sous le nom de base configuré avec les droits UID 65532, puis redémarrer avec l'image correspondante. Ne pas laisser d'anciens fichiers WAL au nom de la base restaurée. Une base provenant d'une version de schéma plus récente est refusée par le binaire.

## Validation sur l'installation réelle

- Deux comptes Discord/Plex distincts, un compte sans partage Plex, un membre hors guild.
- Liaison de chaque compte, vérification que les demandes personnelles ne se mélangent pas.
- Comparaison des nouveautés visibles avec les bibliothèques réellement partagées.
- Navigation `/nouveautes` avec pages courtes, séries regroupées, révocation pendant la navigation et reprise après panne.
- Rappels d’un film et d’un épisode commencés, exclusion d’un contenu terminé, préférences, heures calmes, quota quotidien et absence de doublon après redémarrage/reconnexion.
- Panne Maintainerr ou Tautulli : ancienne liste masquée ; désactivation des notifications : file en attente annulée.
- Révocation Plex puis nouvel appel après cinq minutes ; la panne de vérification doit bloquer l'accès.
- Reconnexion après redémarrage, déconnexion, suppression du compte local et sauvegarde/restauration.
- Commandes sur la guild autorisée, titre contenant une mention, panne Seerr et bot temporairement occupé.

## Limites actuelles

Les sessions et tokens sont uniquement en mémoire, expirent après 12 heures, et sont perdus au redémarrage. Le bot ne peut lire les contenus personnels que pendant une autorisation Plex active ; il invite à reconnecter le Hub sinon. La déconnexion supprime aussi l'autorisation en mémoire utilisée par le bot pour cet utilisateur, mais ne révoque pas l'application dans Plex.

Le partage Plex est revérifié toutes les cinq minutes lors des accès protégés. L’appartenance Discord est vérifiée lors de la liaison facultative ; elle ne conditionne pas l’accès au portail, qui dépend de Plex. Le client revérifie sa session chaque minute et au retour dans l’onglet. Les routes publiques se limitent à l’état de connexion, aux capacités sans URLs privées, au flux d’authentification et aux contrôles de santé. Les quotas sont basés sur l'adresse directe, sans faire confiance aux en-têtes de proxy : derrière Caddy, les visiteurs partagent ce quota.

Le regroupement des nouveautés reste limité à chaque page. Les rappels de retrait et de disponibilité, les livres, collections et métriques sont implémentés ; leur recette avec les comptes réels reste nécessaire. Les demandes de délai ouvrent une file administrateur et ne modifient jamais Maintainerr. Le suivi des imports de livres et les conversions dépendent des contrats des applications installées. Aucun statut « disponible » ni pourcentage n’est inventé.

## Intégrations facultatives

- `GRIMMORY_URL` active le catalogue OPDS et les collections ; HTTPS est obligatoire hors développement. Chaque utilisateur associe ses propres identifiants OPDS, conservés uniquement en mémoire jusqu’à expiration de sa session Plex. Voir [les contrats livres](books-contracts.md) et [les collections/Kobo](book-collections.md).
- `MONITORING_ENABLED=true` active les vues agrégées et la rétention de sept jours. Le [collecteur isolé](monitoring.md) produit `METRICS_FILE` ; monter son dossier en lecture seule pour que les remplacements atomiques soient visibles. Aucun accès matériel ni socket Docker n’est donné au Hub ou à l’API. Sans fichier, les mesures hôte restent explicitement indisponibles ; Tautulli peut fournir l’activité Plex.
- `RADARR_URL`/`RADARR_API_KEY` et `SONARR_URL`/`SONARR_API_KEY` activent les [diagnostics administrateur](diagnostics.md). L’instance doit correspondre exactement à celle de la demande Seerr, y compris HD/4K. Les API Arr ne sont jamais publiées par Caddy.
- `COMMUNITY_FEED_FILE`, `COMMUNITY_FORUM_IDS` et `ROADMAP_ISSUE_IDS` sélectionnent une [publication communautaire](community-publication.md). Pour un export externe, monter son dossier en lecture seule. Pour l’actualisation intégrée, définir aussi `COMMUNITY_SYNC_MANIFEST` avec un manifeste approuvé : le worker utilise le token du bot déjà fourni à l’API et écrit la publication toutes les deux minutes dans un dossier accessible à l’UID 65532, par exemple `/app/data`. Ne jamais y déposer une réponse brute d’API privée. Le CLI `community-feed` reste inclus dans l’image.

Les fichiers TrueNAS laissent ces fonctions désactivées par défaut. Pour monter un fichier optionnel, ajouter au seul service API un volume `type: bind`, `source: /mnt/POOL/concierge-observations`, `target: /config/observations`, `read_only: true`, avec `bind.create_host_path: false`, puis définir le chemin absolu du fichier dans le conteneur. Le dossier source doit préexister et être lisible par l’UID 65532 ; le collecteur externe reste son seul rédacteur.

## Migration vers la connexion Plex

Le schéma SQLite v2 ajoute un identifiant local et rend Discord facultatif. La migration est transactionnelle et conserve identités Plex/Discord/Seerr, préférences et dates. Un compte existant avec Plex est retrouvé par cet identifiant ; un ancien compte Discord sans Plex est fusionné uniquement après preuve du contrôle de ce Discord depuis une session Plex vérifiée. Une identité Discord déjà liée à un autre Plex est refusée.

Sauvegarder la base avant la mise à jour (`truenas-admin.sh backup`) puis récupérer les images API/bot et Hub et redéployer. Les sessions en mémoire seront renouvelées par une connexion Plex. Les anciens binaires refusent un schéma plus récent : pour revenir à une ancienne image, restaurer aussi la sauvegarde antérieure. Aucun nouveau secret ni changement de proxy n’est requis ; `PLEX_URL`, `PLEX_SERVER_ID` et `PUBLIC_ORIGIN` doivent être correctement renseignés.
