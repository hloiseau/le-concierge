# Installation et première validation

Cette version permet de démarrer l'API, le bot d'interactions et le Hub. Les parcours ont été testés avec des fournisseurs simulés ; la validation des versions réellement installées de Plex et Seerr reste nécessaire. Aucun service n'est encore déployé automatiquement sur votre NAS.

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

## Discord

1. Créer une application dédiée au Concierge dans le Developer Portal Discord.
2. Configurer `DISCORD_CLIENT_ID`, `DISCORD_CLIENT_SECRET`, `DISCORD_GUILD_ID` et la clé publique Ed25519 `DISCORD_PUBLIC_KEY`.
3. Déclarer exactement le callback `https://hub.example.com/api/v1/auth/discord/callback` (ou le callback localhost du développement). Le parcours demande `identify` et `guilds.members.read` pour vérifier l'appartenance au serveur ; les membres en attente de validation sont refusés.
4. Générer un `BOT_API_SECRET` aléatoire d'au moins 32 caractères, identique pour API et bot. Exemple : `openssl rand -hex 32`. Ce secret ne quitte jamais les services backend.
5. Exposer le bot via Caddy et renseigner l'Interactions Endpoint URL `https://concierge.example.com/interactions`. Le bot répond au PING signé. Cette architecture utilise les interactions HTTP, pas une connexion Gateway.
6. Installer l'application dédiée dans la guild avec la portée `applications.commands` et le bot si requis par la configuration Discord ; ne pas lui donner la permission Administrateur.
7. Renseigner temporairement `DISCORD_BOT_TOKEN` et exécuter `./bin/bot --register`. Cette opération remplace la liste des commandes de cette application dans cette guild par `/aide`, `/mes-demandes`, `/nouveautes` ; utiliser une application dédiée. Le token de bot n'est plus nécessaire au fonctionnement des réponses HTTP après l'enregistrement.

Les commandes sont privées, acquittées immédiatement puis complétées. Les listes Discord montrent les premiers résultats et renvoient vers le Hub ; la pagination complète dans Discord reste à réaliser. Le bot vérifie les signatures, le serveur, l'application, les rejouements et limite les traitements simultanés. L'API revérifie la signature originale : le seul secret de service ne permet pas d'inventer une identité utilisateur.

## Plex et Seerr

Configurer `PLEX_URL` avec l'adresse privée du PMS et `PLEX_SERVER_ID` avec son `machineIdentifier`. Le backend vérifie le serveur retourné par `/identity` avant de lire les nouveautés. Il n'utilise jamais les adresses contenues dans la réponse Plex resources. Les lectures se font avec le token de ressource de l'utilisateur, pas un token propriétaire global.

L'utilisateur se connecte avec Discord, choisit « Lier Plex », ouvre le lien Plex et autorise l'application. Le PIN est associé à la session initiatrice, expire après cinq minutes maximum et est consommé une seule fois. Les comptes Plex Home gérés/restrictifs ne sont pas pris en charge par ce premier parcours.

Configurer `SEERR_URL` et `SEERR_API_KEY` pour la lecture des demandes. L'association Seerr est explicitement vérifiée par l'administrateur dans cette version : comparer le compte Plex lié et le compte Seerr réel avant de lancer :

```bash
./bin/manage link-seerr --database data/concierge.db --discord 123456789012345678 --seerr 42
```

Ne pas se baser sur le pseudonyme. La commande exige un utilisateur déjà connecté et lié à Plex ; les identifiants Plex et Seerr sont uniques dans la base. Une réponse Seerr contenant une demande d'un autre utilisateur est entièrement refusée. En conteneur : `docker compose --env-file .env -f deploy/compose.yaml exec api /app/manage link-seerr --discord ID --seerr ID`.

Une erreur d'association se corrige avec `manage unlink --discord ID`, puis une nouvelle liaison. Les anciennes autorisations en mémoire deviennent inutilisables dès que l'identité de la base ne correspond plus.

## Conteneurs et TrueNAS

Construction locale, sans registre :

```bash
docker compose --env-file .env -f deploy/compose.yaml up -d --build
# Après configuration Discord :
docker compose --env-file .env -f deploy/compose.yaml --profile discord up -d --build
```

Le profil Discord n'est pas requis pour consulter les liens du Hub. L'API n'a aucun port publié ; seul le Hub peut la joindre via le réseau Compose. Caddy rejoint le port Hub 3000 et, si utilisé, le port bot 8081. Par défaut, les ports locaux sont liés à `127.0.0.1`. Si Caddy tourne dans un autre conteneur, utiliser un réseau Docker partagé ou définir `BIND_IP` sur une adresse LAN joignable, puis adapter Caddy. Ne pas exposer directement ces ports sur Internet.

Pour TrueNAS, utiliser `deploy/truenas.yaml` après avoir remplacé les exemples et vérifié que les images existent. La CI construit une image sur PR ; la publication GHCR n'a lieu que lors d'un push sur main ou un tag. `edge` suit main, `sha-<commit>` permet de figer une version, et un tag `vX.Y.Z` produit une version stable. Vérifier la visibilité GHCR ou fournir un accès en lecture au registre si le package est privé. Aucun tag n'est supposé déjà publié dans ce guide.

Créer le dataset de données avec droit d'écriture pour l'UID/GID **65532** ; ne pas changer les droits d'un dataset de médias existant. L'image Go est non-root et ne nécessite aucun volume de médias. Les valeurs sensibles peuvent être lues via `DISCORD_CLIENT_SECRET_FILE`, `BOT_API_SECRET_FILE`, `DISCORD_BOT_TOKEN_FILE`, `SEERR_API_KEY_FILE`, avec fichiers montés en lecture seule. Ne jamais définir simultanément la variable et sa variante `_FILE`.

Ajout du bot dans le YAML TrueNAS :

```yaml
  bot:
    image: ghcr.io/hloiseau/le-concierge:edge
    entrypoint: [/app/bot]
    environment:
      APP_ENV: production
      PUBLIC_ORIGIN: https://hub.example.com
      DISCORD_CLIENT_ID: 'ID_APPLICATION'
      DISCORD_GUILD_ID: 'ID_SERVEUR'
      DISCORD_PUBLIC_KEY: 'CLE_PUBLIQUE_ED25519'
      BOT_API_SECRET: 'MEME_SECRET_ALEATOIRE_QUE_API'
      BOT_API_URL: http://api:8080
    ports: ['192.0.2.10:8081:8081']
    restart: unless-stopped
    read_only: true
    cap_drop: [ALL]
    security_opt: [no-new-privileges:true]
```

Ce bloc suppose le réseau Compose commun avec `api`. Pour l'enregistrement initial, ajouter temporairement le token de bot à l'environnement de ce service puis lancer `/app/bot --register` via un conteneur ponctuel. Le bot n'a pas besoin de lire la base SQLite.

## Sauvegarde et retour arrière

`manage backup --database /app/data/concierge.db --destination /app/data/backup-YYYYMMDD.db` utilise une sauvegarde SQLite cohérente (`VACUUM INTO`) même avec WAL. Un fichier existant n'est jamais écrasé. Conserver la sauvegarde dans un emplacement privé et tester sa lecture avant une mise à jour.

Pour restaurer : arrêter l'API et le bot, conserver l'ancienne base et ses fichiers `-wal`/`-shm` ensemble dans un dossier de secours, placer la sauvegarde validée sous le nom de base configuré avec les droits UID 65532, puis redémarrer avec l'image correspondante. Ne pas laisser d'anciens fichiers WAL au nom de la base restaurée. Une base provenant d'une version de schéma plus récente est refusée par le binaire.

## Validation sur l'installation réelle

- Deux comptes Discord/Plex distincts, un compte sans partage Plex, un membre hors guild.
- Liaison de chaque compte, vérification que les demandes personnelles ne se mélangent pas.
- Comparaison des nouveautés visibles avec les bibliothèques réellement partagées.
- Révocation Plex puis nouvel appel après cinq minutes ; la panne de vérification doit bloquer l'accès.
- Reconnexion après redémarrage, déconnexion, suppression du compte local et sauvegarde/restauration.
- Commandes sur la guild autorisée, titre contenant une mention, panne Seerr et bot temporairement occupé.

## Limites actuelles

Les sessions et tokens sont uniquement en mémoire, expirent après 12 heures, et sont perdus au redémarrage. Le bot ne peut lire les contenus personnels que pendant une autorisation Plex active ; il invite à reconnecter le Hub sinon. La déconnexion supprime aussi l'autorisation en mémoire utilisée par le bot pour cet utilisateur, mais ne révoque pas l'application dans Plex.

Le partage Plex est revérifié toutes les cinq minutes lors des accès protégés. L'appartenance Discord est vérifiée à la connexion ; un retrait Discord ne coupe donc pas immédiatement une session existante (maximum 12 heures). Les quotas sont basés sur l'adresse directe, sans faire confiance aux en-têtes de proxy : derrière Caddy, les visiteurs partagent ce quota.

Les nouveautés n'ont pas encore de regroupement avancé ni d'affiches ; l'API ne fait aucun cache partagé de données privées. Le titre des demandes est enrichi par Seerr avec une valeur de repli si les métadonnées manquent. La préférence de notifications est stockée mais aucun envoi proactif n'est actif. Maintainerr/Tautulli, livres, métriques et conversions restent à intégrer.
