# Suivre un contenu demandé

Les suivis sont communs au Hub et aux commandes privées `/suivre` et `/mes-suivis`. Ils utilisent le compte Plex connecté et son association Seerr vérifiée. Un suivi ne crée ni n’approuve une demande Seerr.

Un contenu est identifié par son **type** (`movie` ou `tv`), son **identifiant TMDB** et, pour une série, un **numéro de saison facultatif**. La saison zéro représente les épisodes spéciaux ; l’absence de saison représente la série. Chaque compte possède au plus un suivi par combinaison, avec une limite de 100 suivis. Répéter une création actualise le canal du suivi existant. Il n’y a pas de distinction entre éditions, langues et qualités vidéo.

Pour une série ou une saison, « disponible » signifie qu’au moins un épisode est présent et accessible à ce compte Plex. Cela ne promet ni une saison complète ni un rappel pour chaque nouvel épisode. Le numéro de saison doit exister dans les métadonnées Seerr. Le suivi doit correspondre à une demande en attente ou approuvée, même si elle appartient à un autre membre. Aucun nom, identifiant ni détail du demandeur ne figure dans les réponses.

## API commune

- `GET /api/v1/subscriptions?offset=0` : dix suivis personnels par page, avec titre et état vérifiés lors de la lecture. Un contenu devenu inaccessible conserve une ligne « Contenu inaccessible » sans titre ni lien, pour permettre le désabonnement.
- `POST /api/v1/subscriptions` : corps `{"type":"movie","tmdbId":12,"channel":"hub"}`. Pour une saison : `{"type":"tv","tmdbId":34,"season":2,"channel":"hub"}`. Une création réussie renvoie `following` et le suivi. Un contenu déjà accessible renvoie immédiatement `already_available`, avec son lien Plex, et ne laisse aucun suivi en attente pour ce contenu.
- `DELETE /api/v1/subscriptions/{id}` : désabonnement du propriétaire, réponse `{"ok":true}`. Supprimer un identifiant appartenant à un autre compte renvoie la même erreur qu’un identifiant absent.

Les mutations du Hub exigent son origine configurée. Aucun paramètre ne permet de choisir un autre propriétaire. La suppression du compte efface les suivis et leurs observations de disponibilité. SQLite v4 conserve les suivis après un redémarrage, mais une nouvelle connexion Plex reste nécessaire pour en vérifier l’accès.

## Configuration et limites

Utiliser la configuration Plex et Seerr existante : `PLEX_URL`, `PLEX_SERVER_ID`, `SEERR_URL`, `SEERR_API_KEY` (ou sa variante `_FILE`). Aucun secret supplémentaire n’est nécessaire.

Le canal disponible est **Hub**. `discord` est réservé dans le contrat de stockage mais sa sélection renvoie `409 channel_unavailable` : les messages privés de disponibilité des suivis ne sont pas encore implémentés. Les commandes Discord consultent et créent le même suivi Hub. L’état est actualisé à l’ouverture ou à l’actualisation de la liste ; aucun travail de surveillance des suivis ne tourne en arrière-plan. Une observation de disponibilité est enregistrée une seule fois, sauf pour une demande de l’utilisateur lui-même, déjà suivie dans ses demandes Seerr. Cette observation n’est pas un envoi Discord.

Seerr autorise la consultation de son catalogue avant import ; il ne garantit pas les droits futurs dans la bibliothèque Plex de destination. L’API vérifie l’identité Seerr utilisée et ne déclare un contenu disponible qu’après une lecture Plex avec le jeton personnel, correspondance TMDB exacte et présence d’un film ou épisode avec média. Une source indisponible, un identifiant ambigu ou un contenu inaccessible ne produit pas de fausse confirmation. Les restrictions réelles et les agents de métadonnées du NAS restent à vérifier avant déploiement ; un ancien agent sans `Guid` TMDB est refusé.

Sources consultées : [contexte utilisateur Seerr](https://github.com/seerr-team/seerr/blob/develop/server/middleware/auth.ts), [routes Seerr](https://github.com/seerr-team/seerr/blob/develop/server/routes/index.ts), [modèle de média Seerr](https://github.com/seerr-team/seerr/blob/develop/server/entity/Media.ts), [API Plex officielle](https://developer.plex.tv/pms/). Les tests utilisent uniquement des fournisseurs simulés.
