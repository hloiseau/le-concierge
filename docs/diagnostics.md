# Diagnostics Radarr et Sonarr

Issue [C22](https://github.com/hloiseau/le-concierge/issues/22), contrat vérifié le 16 octobre 2026. Le diagnostic explique les éléments observés pour **une demande Seerr**. Il ne déclenche ni recherche, téléchargement, import, déplacement ou suppression.

## Sources et versions

Les contrats de référence sont les fichiers OpenAPI officiels de [Radarr v6.4.4.10685](https://github.com/Radarr/Radarr/blob/v6.4.4.10685/src/Radarr.Api.V3/openapi.json) et [Sonarr v4.0.20.3014](https://github.com/Sonarr/Sonarr/blob/v4.0.20.3014/src/Sonarr.Api.V3/openapi.json). L’API reste nommée `v3` ; la [documentation Sonarr](https://sonarr.tv/docs/api/) confirme son application aux versions 3 et 4.

**Ces versions sont celles des sources auditées, pas des installations.** Aucun couple URL/clé Radarr ou Sonarr n’était disponible pour une lecture authentifiée locale. Chaque diagnostic lit `GET /api/v3/system/status` et expose la version effectivement reçue, après vérification du nom d’application. Une recette sur les instances installées reste nécessaire avant de considérer C22 validé en production. Les fixtures sont synthétiques, sans données NAS.

La demande et son instance de destination proviennent des routes officielles Seerr [Get MediaRequest](https://docs.seerr.dev/api/get-media-request/), [Get Radarr settings](https://docs.seerr.dev/api/get-radarr-settings/) et [Get Sonarr settings](https://docs.seerr.dev/api/get-sonarr-settings/). Les états Seerr approuvé, échoué et terminé sont tous diagnostiquables ; une demande en attente ou refusée produit un résultat explicite. « Terminé » dans Seerr ne constitue pas une preuve de lecture Plex. [États de demande Seerr](https://github.com/seerr-team/seerr/blob/develop/server/constants/media.ts).

## Configuration et accès

- `RADARR_URL` et `RADARR_API_KEY`, ou `RADARR_API_KEY_FILE` ; couple facultatif mais complet, sans suffixe `/api/v3`.
- `SONARR_URL` et `SONARR_API_KEY`, ou `SONARR_API_KEY_FILE` ; même règle.
- `SEERR_URL` et la clé Seerr existante servent à lire la demande et sa destination.
- Le compte doit être connecté à Plex, lié à Discord et présent dans `ADMIN_DISCORD_IDS`. Ce droit est revérifié après les lectures, ainsi que la session et le lien Plex.

Les clés Arr sont des secrets administrateur : le client ne les emploie que pour les GET documentés ci-dessous. Le diagnostic n’utilise jamais une clé ou une adresse distante renvoyée par Seerr. Il compare le serveur choisi par la demande, ou l’unique serveur par défaut de sa qualité HD/4K, à l’URL explicitement configurée. Schéma, hôte, port et chemin de base doivent correspondre. Une instance 4K différente doit être configurée comme destination correspondante ; une seule instance par application est prise en charge. Une divergence interrompt le diagnostic, même si le même titre existe ailleurs.

`GET /api/v1/admin/diagnostics?requestId=7` accepte un identifiant Seerr entier de 1 à 2147483647, sans autre paramètre ni doublon. Limite : 12 lectures par minute selon la politique API existante. Codes : 401 sans session, 403 sans droit ou lien Plex, 400 pour une saisie invalide, 404 pour une ressource amont absente, 503 pour une source inaccessible ou incohérente.

La réponse contient `source`, `version`, `stage`, `summary`, `evidence[{stage,message,at?}]`, `checkedAt`, `queueTruncated`, `historyTruncated`, `plexStatus` et éventuellement `guideUrl`. Les dates sont UTC. Elle n’expose ni nom de fichier, chemin, titre de release, nom de tracker, clé, utilisateur, identifiant de téléchargement ou adresse privée.

`/diagnostic demande:7` utilise le même service. La commande est enregistrée avec la permission Discord administrateur et reste contrôlée côté API. Sa réponse est éphémère, bornée à 2000 caractères ; elle n’est pas publiée dans un salon ni envoyée périodiquement.

## Lectures et corrélation

| Source | GET | Contrôle |
| --- | --- | --- |
| Seerr | `/api/v1/request/{id}` | ID exact, type, date, ID TMDB, ID TVDB et saisons pour une série. |
| Seerr | `/api/v1/settings/radarr` ou `/sonarr` | Instance de la demande, ou défaut de même qualité ; aucune clé renvoyée n’est conservée. |
| Arr | `/api/v3/system/status` | Application et version. |
| Radarr | `/api/v3/movie?tmdbId={id}` | Un seul film, ID TMDB exact ; son ID interne filtre les lectures suivantes. |
| Sonarr | `/api/v3/series?tvdbId={id}` | Une seule série, ID TVDB exact et TMDB cohérent lorsqu’il est fourni. |
| Arr | `/api/v3/queue?page=1&pageSize=50&movieIds={id}` ou `seriesIds` | Chaque ligne appartient au média attendu. Pour Sonarr, seules les saisons demandées avec numéro explicite sont retenues. |
| Arr | `/api/v3/history?page=1&pageSize=30&sortKey=date&sortDirection=descending&movieIds={id}` ou `seriesIds` | Même ID ; événements depuis la création de la demande. Sonarr ajoute `includeEpisode=true` pour contrôler la saison. |
| Plex, facultatif | `/identity`, puis `/library/metadata/{ratingKey}` | Serveur configuré, jeton du compte administrateur, ID exact, type et GUID TMDB ; variante HD/4K sélectionnée par Seerr. |

La durée totale est de six secondes, avec au plus huit GET et 2 Mio par réponse. Aucun parcours complet des files n’est lancé. Les indicateurs de troncature rendent visibles les limites de 50 lignes de file et 30 événements récents ; des saisons peuvent donc manquer. Douze éléments explicatifs au maximum sont rendus. Les IDs proviennent des sources, jamais d’une recherche de titre. Les erreurs HTTP ne divulguent pas leur corps brut.

## Interprétation et résolution

Les étapes possibles sont `unknown`, `approval`, `declined`, `not_found`, `downloading`, `download_failed`, `importing`, `manual_import`, `space`, `permissions`, `rename_import`, `imported` et `plex_indexing`. Les erreurs courantes sont reconnues par leurs clauses stables : manque d’espace, refus d’accès, dossier absent/inaccessible et échec de déplacement ou renommage. Le reste du texte libre est remplacé par une indication neutre ; les détails restent dans l’application administrateur. Un ancien échec dans l’historique n’est pas présenté comme un blocage actuel.

Le cas « matched to movie by ID » avec « Manual Import required » conserve la clause utile et renvoie à la [FAQ officielle Radarr](https://wiki.servarr.com/radarr/faq). L’administrateur vérifie d’abord que le fichier correspond au film attendu : une correspondance fournie par un indexeur peut contredire le nom de release. Il utilise ensuite l’interface d’import manuel de Radarr uniquement après cette vérification. Une mauvaise correspondance doit être corrigée auprès de sa source ; Le Concierge ne force aucun import. Les montages, droits, renommages et espaces disponibles sont à vérifier dans l’interface et la [documentation des réglages Radarr](https://wiki.servarr.com/radarr/settings).

Un fichier signalé par Arr, ou un import historique, ne démontre pas que la demande entière est disponible. `plexStatus=indexed` atteste uniquement que les métadonnées exactes ont été lues avec les droits du compte administrateur ; ni lecture effective, ni présence de tous les épisodes, ni droits d’un autre utilisateur ne sont attestés. `not_verified` n’affirme pas que Plex est absent : la vérification peut manquer, échouer ou dépasser le délai.

## Vérification locale

`go test ./internal/upstream ./internal/httpapi ./internal/discord -run 'TestDiagnostic|TestCommandOptions|TestRegisterCommands|TestOpenAPI'`

Les tests couvrent requêtes GET uniquement, corrélation stable, séparation HD/4K, saisons, historique ancien, troncature, erreurs nettoyées, absence de média, états Seerr, distinction fichier/indexation Plex, paramètres invalides, refus non-admin, retrait du rôle ou de la session pendant la lecture, réponse Discord privée et rejeu refusé. La recette authentifiée sur les versions installées reste à effectuer ; aucun téléchargement ou import réel n’a été créé pour ces tests.
