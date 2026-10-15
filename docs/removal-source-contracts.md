# Contrats des sources de retrait

Audit du 15 octobre 2026 pour [l’issue #25](https://github.com/hloiseau/le-concierge/issues/25). Les conclusions ci-dessous portent sur les versions indiquées, pas sur `main`. Ce document décrit les lectures nécessaires ; il n’autorise aucune action de suppression et ne constitue pas une recette des futurs rappels.

## Versions et niveau de vérification

| Source | Version application indiquée par l’utilisateur | Version du catalogue TrueNAS | Source auditée |
| --- | --- | --- | --- |
| Maintainerr | `3.30.1` | `1.1.47` | Tag `v3.30.1`, commit `3e11c99b8bdaf567f2463c9975a46a8259616616` |
| Tautulli | `v2.18.2` | `1.3.14` | Tag `v2.18.2`, commit `9484667bc1f2d159368a999320b8f495bbfbd00a` |

Les tags ont été résolus avec `git ls-remote`, puis leurs sources lues dans des clones temporaires. Références : [Maintainerr][M-tag], [Tautulli][T-tag]. Les numéros de catalogue ne sont pas des versions d’API.

La lecture de l’instance Maintainerr, effectuée pendant cette session, a confirmé `status: 1`, `version: "3.30.1"`, `commitTag: "latest-78ee155"`, le type `plex`, une identité serveur non vide et la forme complète d’`overlay-data`, avec `media.length == mediaCount` pour chaque collection. Aucun titre, identifiant nominatif, secret ni adresse privée n’est conservé ici.

Le commit du build est [`78ee15578f25493fea7196a0e4d9780d10f7460d`][M-build]. Sa comparaison au tag montre seulement le changelog et les trois versions de `package.json` : les fichiers de code audités sont identiques. Cette comparaison explique l’écart de SHA ; elle ne vérifie pas le digest de l’image Docker.

Le diagnostic Go a ensuite confirmé Tautulli `v2.18.2` avec une clé autorisée lue localement : authentification par en-tête, même identité serveur que Maintainerr, compte administrateur trouvé par identifiant dans `get_users`, puis historique paginé jusqu’au total annoncé. Ces lectures n’ont modifié aucune source ni ouvert SQLite côté Concierge. La correspondance avec la configuration de l’API déployée et les identités Discord reste à vérifier ; la réussite du diagnostic ne remplace pas ce contrôle.

## Maintainerr : lectures utiles

Il n’existe pas d’authentification entrante native dans cette version. Une clé générée dans les paramètres n’est pas une protection `X-Api-Key`. Un éventuel contrôle d’accès appartient au proxy placé devant l’application. Préfixer les chemins par `BASE_PATH` si configuré. Ne pas lire les paramètres ou les configurations de notification pour découvrir des secrets. [Conventions officielles][M-api]

| Requête GET | Corps attendu | Usage / limite |
| --- | --- | --- |
| `/api/app/status` | `{status:1, version:"3.30.1", commitTag:string, updateAvailable:boolean}` | Le contrôleur renvoie du texte contenant un objet JSON sérialisé. Exiger `status == 1`; un échec peut produire `status:0, version:"0.0.1"`. La vérification des mises à jour peut contacter GitHub. |
| `/api/app/timezone` | Texte brut, par exemple `Europe/Paris` | Fuseau du processus Maintainerr ; ne pas le décoder comme objet JSON. |
| `/api/media-server/type` | `{type:"plex"}` | Valeurs possibles : `plex`, `jellyfin`, `emby`. Pour ce projet, refuser toute autre valeur. |
| `/api/media-server` | `{machineId:string, version:string, name?:string, platform?:string, url?:string}` | Avec Plex, `machineId` vient de `machineIdentifier` du serveur. Une réponse vide n’est pas une identité valide. |
| `/api/collections/overlay-data` | Tableau de collections, chacune avec `media` complet et `mediaCount` | Lecture SQL sans enrichissement distant, sans pagination ni plafond serveur. |
| `/api/collections/media?collectionId=N` | Tableau des appartenances brutes | Alternative complète pour une collection, sans pagination. |
| `/api/collections/media/count?collectionId=N` | Nombre JSON | Nombre d’appartenances, pas nombre d’œuvres distinctes. |
| `/api/rules` | Tableau de groupes | Inclut règles, collection et notifications ; voir les précautions ci-dessous. |
| `/api/rules/exclusion?rulegroupId=N` | Tableau d’exclusions | Exclusions du groupe N **et** exclusions globales. |

Sources : [contrôleur application][M-app], [service de version][M-version], [contrôleur serveur][M-server], [mapping Plex][M-mapper], [lectures collections][M-collections], [règles][M-rules-controller].

`GET /api/collections` n’offre qu’un aperçu de deux médias ; `mediaCount` est le vrai compte. La route `/api/collections/media/{id}/content/{page}` est paginée à partir de 1 (`size=25` par défaut), retourne `{totalSize,items}`, mais saute les médias non résolus. Avec certains tris, son total compte seulement les médias résolus. Elle peut aussi nettoyer le lien d’une collection distante disparue. Elle ne convient donc pas au collecteur minimal en lecture seule. `overlay-data` lit toutes les lignes puis construit `mediaCount` depuis leur longueur ; une réponse vide/non-tableau ou dépassant la limite locale doit invalider le relevé entier. [Implémentation de l’aperçu et du relevé complet][M-collections], [description des routes][M-collections-doc]

### Champs et identifiants

Une collection fournit notamment `id`, `mediaServerId`, `mediaServerType`, `libraryId`, `type`, `isActive`, `arrAction`, `deleteAfterDays`, et les références de configuration `*arr`. Son `mediaServerId` identifie la **collection distante**, pas le serveur. `libraryId` est stocké en `varchar` et renvoyé comme chaîne JSON. `type` appartient à `movie | show | season | episode`. Les relations ne sont pas chargées par `overlay-data` : ne pas attendre un `ruleGroup` imbriqué. [Entité collection][M-collection-entity], [requête de lecture][M-collections]

Chaque membre fournit `id`, `collectionId`, `mediaServerId`, `addDate`, `tmdbId?`, `tvdbId?`, `isManual`, `includedByRule`, `manualMembershipSource`, `ruleEvaluationFailed`, `sizeBytes?`, `image_path?`. Ici `mediaServerId` est le `ratingKey` lorsque la collection appartient à Plex. Les gros compteurs de taille peuvent être des chaînes ; ils ne sont pas nécessaires au rapprochement. `manualMembershipSource` vaut `legacy`, `local`, `shared` ou `null`; l’appartenance manuelle est reconnue si ce champ est non nul **ou** si `isManual` vaut vrai. [Entité appartenance][M-media-entity]

Normalisation recommandée : conserver `(machineIdentifier, ratingKey, type)`, `libraryId`, `collectionId`, `membershipId`, l’action numérique, les états d’activation séparés, la date d’ajout source et l’instant de collecte. Un TMDB/TVDB identique ne suffit pas à fusionner deux éditions ou deux entrées Plex.

### Règles et exclusions

`GET /api/rules` retourne notamment `{id,name,collectionId,isActive,useRules,dataType,ruleHandlerCronSchedule,rules,collection,notifications}`. Sans filtre, il exclut les groupes de bibliothèque `-1`. `GET /api/rules/collection/{collectionId}` retourne un groupe ou une valeur vide/nulle. Les deux lectures chargent les notifications : ne conserver que les champs nécessaires et ne jamais journaliser le corps brut. `activeOnly=true` filtrerait les groupes inactifs et masquerait une incohérence utile au diagnostic. [Service des règles][M-rules]

Une exclusion brute est `{id,mediaServerId,ruleGroupId,parent,type}` ; les quatre derniers champs peuvent être nuls pour des données anciennes. `ruleGroupId=null` signifie global. `GET /api/rules/exclusion` **sans paramètre retourne `[]`**, pas toutes les exclusions. Le filtre `rulegroupId=N` ajoute les globales ; le filtre `mediaServerId=K` seul cherche l’identifiant ou le champ `parent`. [Lecture des exclusions][M-exclusions-read], [entité][M-exclusion-entity]

Pour une exclusion typée, la cascade utilise `mediaServerId` et `type` : une série exclut ses saisons/épisodes, une saison ses épisodes. `parent` mémorise le point d’entrée d’une demande et ne doit pas être utilisé comme cascade générale ; il sert de repli pour les anciennes lignes sans type. Il faut donc connaître `parentId` et `grandparentId` des membres pour statuer sur leurs exclusions. Les métadonnées sont accessibles par `/api/media-server/meta/{id}`. Sans hiérarchie fiable, conserver un état d’exclusion inconnu et bloquer les rappels. [Cascade exacte][M-cascade], [route de métadonnées][M-server]

### Actions, activation et échéances

| `arrAction` | Sens | Menace de suppression de fichiers |
| --- | --- | --- |
| `0` | `DELETE` | Oui |
| `1` | `UNMONITOR_DELETE_ALL` | Oui |
| `2` | `UNMONITOR_DELETE_EXISTING` | Oui |
| `3` | `UNMONITOR` | Non |
| `4` | `DO_NOTHING` | Non |
| `5` | `DELETE_SHOW_IF_EMPTY` | Oui : pour une saison Sonarr, supprime ses fichiers puis la série si vide |
| `6` | `UNMONITOR_SHOW_IF_EMPTY` | Non |
| `7` | `CHANGE_QUALITY_PROFILE` | Non, pas une suppression directe |

L’action 5 n’est prise en charge par le chemin Sonarr que pour `season`. Ne pas présenter les autres combinaisons action/type comme un contrat garanti ; le repli sans `*arr` peut lui-même appeler la suppression du serveur média. Une action déclarée destructive reste une intention, pas une preuve de réussite. [Énumération][M-actions], [traitement Sonarr][M-sonarr], [traitement et replis][M-handler]

Le worker choisit les collections `isActive=true`, pas directement `ruleGroup.isActive`. Il saute l’action 4 et les délais `null`, puis sélectionne les membres arrivés à échéance. Une erreur d’évaluation de règle protège le membre sauf appartenance manuelle. Il vérifie les exclusions et reporte les contenus identifiés comme en lecture. Cette protection des lectures est une observation au début du passage, pas une garantie permanente. [Worker de la version exacte][M-worker]

Pour la politique du Concierge, conserver l’activation du groupe **et** celle de la collection. Une divergence rend le diagnostic incertain ; ne pas traduire « groupe inactif » en « fichiers certainement protégés ». Une collection manuelle peut exister sans groupe. [Relations de groupe][M-group], [sélection du worker][M-worker]

Le test d’échéance du worker est `addDate <= maintenant - deleteAfterDays * 86_400_000`. L’ajout écrit initialement le jour local (`new Date().toDateString()`), et un report remet la date à minuit local. En revanche, l’aide utilisée pour l’affichage calcule `Date.setDate(date + jours)` : lors d’un changement d’heure, jours calendaires et blocs de 24 heures peuvent différer. Conserver l’instant source exact et le fuseau, et nommer toute date dérivée « échéance estimée ». [Calcul du worker et écriture d’ajout][M-dates], [calcul partagé d’affichage][M-date-helper]

`deleteAfterDays=0` signifie éligible dès le prochain traitement ; il ne donne aucune date future certaine. `null` signifie ici absence d’échéance automatique. Une valeur invalide, absente ou non positive ne doit jamais créer une promesse de suppression dans le Concierge. Le calendrier du worker vient de `collection_handler_job_cron`. `/api/tasks/Collection%20Handler/status` expose `{time,running,runningSince}`, sans date d’exécution future. [Initialisation du worker][M-worker], [statut des tâches][M-tasks]

Il n’existe pas de date effective de suppression dans les membres. Une disparition d’une collection peut résulter d’un retrait d’appartenance, d’une exclusion, d’une modification de règle ou d’un traitement ; elle n’établit pas que des fichiers ont été effacés. Un résultat `handled` peut correspondre à une action non destructive, et `removed-missing` indique un média déjà absent. Le journal de collection est paginé et soumis à rétention ; une interprétation des événements resterait un adaptateur distinct. [Résultats du traitement][M-handler], [journaux][M-collections-doc]

**Divergences documentaires constatées.** Au moment de l’audit, la page officielle Collections indique encore que `null` est traité comme zéro et que les exclusions attendent le passage des règles. Le source du tag et du build installés contient déjà le saut des délais nuls et une vérification d’exclusions dans le worker. Pour ces deux points, utiliser le source épinglé ; ne pas recopier la documentation courante comme preuve du comportement installé. [Page consultée][M-collections-doc], [code exécuté][M-worker]

## Tautulli : identité et historique

Utiliser `GET {HTTP_ROOT}/api/v2?cmd=...` avec l’en-tête `X-Api-Key`. L’alternative `apikey` dans l’URL existe mais n’est pas recommandée. La réponse JSON est `{response:{result,message,data}}`; exiger `result == "success"` en plus du statut HTTP. N’appeler qu’une liste fermée de commandes de lecture : le même point d’entrée propose aussi des commandes de mutation. [Authentification et enveloppe, v2.18.2][T-api]

| `cmd` | `response.data` | Vérification |
| --- | --- | --- |
| `get_tautulli_info` | Objet avec `tautulli_version`, `tautulli_commit`, etc. | Version attendue `v2.18.2`; relever le commit sans exposer les détails de l’hôte. |
| `get_server_info` | Objet avec `pms_identifier`, `pms_version`, etc. | Comparer `pms_identifier` au `machineId` Maintainerr et au serveur Plex de l’identité vérifiée. Cette route lit la configuration, ce n’est pas une sonde de disponibilité Plex. |
| `get_users` | Tableau avec `user_id`, `row_id`, `is_active`, `keep_history`, `shared_libraries`, etc. | `user_id` est l’identifiant Plex, `row_id` est la clé interne Tautulli. Aucun rapprochement par nom ou email. |
| `get_history` | `{recordsTotal,recordsFiltered,draw,data:[...],filter_duration,total_duration}` | Historique filtré et paginé ; détails ci-dessous. |

Sources : [informations Tautulli][T-info], [identité configurée du serveur][T-server], [liste d’utilisateurs][T-users-route], [stockage utilisateurs][T-users]. Le `user_id` est alimenté depuis l’attribut `id` PlexTV, y compris pour les comptes partagés ; le compte local `0` n’est pas une preuve d’identité d’un utilisateur Discord. [Import PlexTV][T-plextv-users], [mise à jour utilisateurs][T-users-refresh]

`get_users` filtre directement `WHERE deleted_user = 0` et ne renvoie **pas** le champ `deleted_user`. Son absence est normale. `is_active` et `keep_history` proviennent de colonnes SQLite `INTEGER` (valeurs usuelles `0/1`), de même que `user_id` et `row_id`. Exiger un identifiant positif exact et les deux indicateurs à `1`; toute valeur manquante/non reconnue reste inconnue. Une erreur SQL renvoie `[]` : l’absence d’utilisateur ne peut jamais être assimilée à une validation. [Lecture SQL][T-users], [schéma][T-users-schema]

**Ne pas remplacer cette lecture par `get_user`.** Bien que la commande paraisse être une lecture, si l’utilisateur manque, elle appelle `refresh_users()` : requêtes PlexTV puis écritures dans la table utilisateurs. Si le compte reste absent, elle retourne un utilisateur de repli `Local` avec `user_id:0`, `row_id:0`, `is_active:1`, `keep_history:1`, `deleted_user:0`, au lieu d’un « introuvable » explicite. Les seuls indicateurs ne suffisent donc pas à identifier le compte. Le collecteur strictement en lecture seule utilise `get_users` et vérifie l’égalité de l’identifiant. [Route `get_user`][T-user-route], [repli et rafraîchissement][T-user-fallback]

### Pagination et données d’une lecture

Requête minimale proposée :

```text
GET /api/v2?cmd=get_history&user_id=<identifiant-Plex-verifie>&grouping=0&include_activity=0&start=0&length=100&order_column=date&order_dir=asc
X-Api-Key: <secret-cote-backend>
```

`start` est un décalage à partir de zéro. `length` borne la page ; ne pas envoyer `-1`, qui permet de lire tout l’historique. L’arrêt repose sur `recordsFiltered`, pas sur `recordsTotal` qui compte la table entière. `grouping=0` impose les sessions séparées ; sinon les valeurs par défaut dépendent des paramètres de l’instance. `include_activity=0` évite le mélange des sessions actives et terminées. [Contrat de `get_history`][T-history], [pagination SQL][T-pagination], [groupement][T-history-data]

Une ligne contient notamment `row_id`, `id` (alias de `row_id`), `reference_id`, `user_id`, `media_type`, `rating_key`, `parent_rating_key`, `grandparent_rating_key`, `guid`, `percent_complete`, `watched_status`, `started`, `stopped`, `live`. Les dates sont des secondes Unix. Les identifiants peuvent être des nombres ou des chaînes : normaliser sans flottants. **`machine_id` dans une ligne est l’appareil de lecture, pas le serveur Plex.** Pour le serveur, utiliser exclusivement le contexte `pms_identifier`. [Construction des lignes][T-history-data], [appareils des utilisateurs][T-devices]

Recommandation de robustesse : valider chaque `user_id` retourné contre celui demandé ; interdire les IDs vides, multiples ou non validés ; dédupliquer par `row_id` ; limiter pages, lignes, temps total et taille de réponse. Une page vide avant le total, un total changeant, des doublons ou un dépassement invalident la complétude. L’API n’offre pas de snapshot atomique entre pages : un import ou une suppression concurrente peut déplacer les offsets. Un parcours entièrement réussi ne démontre donc que la complétude du relevé API observé, jamais celle de tous les visionnages passés. [Pagination avec LIMIT/OFFSET][T-pagination]

Les filtres `rating_key`, `parent_rating_key`, `grandparent_rating_key`, `media_type` et `guid` existent. Attention : `guid` est recherché par préfixe après retrait de la query string ; ce n’est pas une égalité stricte. Les filtres temporels `before` et `after` utilisent dans le code des comparaisons strictes, malgré leur description inclusive. Éviter ces filtres dans le premier collecteur complet par utilisateur. [Construction des filtres][T-history-filters]

### Complétion, granularité et inconnues

`percent_complete` est un rapport de position de lecture sur la durée, arrondi ; ce n’est pas le temps effectivement regardé. `watched_status` prend `0`, `0.25`, `0.5`, `0.75` ou `1`. Le statut `1` utilise la politique configurée de Tautulli, y compris les marqueurs de crédits ; le direct est considéré terminé indépendamment. Exclure `live` pour les rappels de films/séries. [Calcul des lignes][T-history-data]

Le pourcentage n’est pas plafonné dans le code : `int(round(view_offset / duration * 100))` peut dépasser 100 si les données de position/durée le permettent. Le collecteur conserve ce rapport et utilise exclusivement `watched_status` pour son indicateur de complétion, sans imposer un plafond de 100. Les cinq valeurs de `watched_status` ci-dessus sont l’ensemble fermé produit par cette version. [Calcul et sérialisation][T-history-data]

Les défauts v2.18.2 sont 85 % pour films et épisodes, et `WATCHED_MARKER=3` : le premier marqueur de crédits ou le seuil, selon lequel arrive en premier. Les autres modes utilisent les crédits finaux, les premiers crédits ou le pourcentage. Les paramètres installés restent inconnus. Ne pas annoncer un seuil universel de 90 %, ni reconstruire `watched_status` à partir de son pourcentage arrondi. Pour une première politique, garder le statut source et l’étiquette « terminé selon Tautulli » ; toute politique propre au Concierge doit expliciter son seuil et sa source. [Algorithme][T-watched], [valeurs par défaut][T-config]

Une trace partielle prouve une lecture observée ; elle ne prouve pas que le contenu n’a jamais été terminé ailleurs. L’absence de trace produit `unknown`. Des sessions courtes sont ignorées (120 secondes par défaut), l’historique peut être désactivé par utilisateur ou bibliothèque, et il peut être purgé. `keep_history=true` aujourd’hui ne certifie pas une couverture historique passée. [Conditions de journalisation][T-logging], [purge utilisateur][T-purge]

Les historiques sont des films/épisodes, pas une preuve de complétion d’une saison entière. Pour saison/série, il faut d’abord un inventaire complet et actuel des épisodes du périmètre, puis une preuve individuelle pour chacun. Un épisode observé terminé ne termine pas la saison ; les épisodes absents restent inconnus. Les métadonnées Plex consultées avec le compte de Maintainerr ne représentent pas automatiquement l’état de chaque utilisateur : son propre code distingue l’historique multi-comptes du compteur lié au token. [Limite d’état Plex][M-watch]

Un `ratingKey` doit rester associé à son serveur. En cas de réimport, changement de clé, GUID différent ou éditions multiples, ne pas transférer automatiquement l’état de lecture par titre/TMDB/TVDB. Le GUID et les indices de saison/épisode sont des indices de rapprochement à vérifier, pas une preuve d’édition identique. Le retour `get_history` de cette version ne fournit pas `edition_title` ; la résolution de l’ambiguïté exige d’autres métadonnées. [Champs réellement retournés][T-history-data]

## Adaptateurs minimaux et travail restant

1. Collecteur Maintainerr : version/build, type et identité serveur, puis `overlay-data` sous limites locales. Produire un relevé normalisé et daté, distinct des candidats à un rappel. Ne pas déduire les membres de l’aperçu.
2. Collecteur Tautulli : version, identité serveur, utilisateur Plex exact présent dans `get_users`, puis historique paginé. Ne conserver ni pseudonymes, emails, adresses IP ni détails de lecteurs si la décision n’en a pas besoin.
3. Diagnostic opérateur : exposer versions, nombre de collections/appartenances, qualité et fraîcheur des relevés. Ne publier ni corps bruts ni identifiants dans les logs ; ne fournir aucune route utilisateur tant que l’accès par identité n’est pas vérifié.
4. Cache borné par serveur/utilisateur, invalidé sur changement de version, révocation, échec ou incohérence. Un dépassement de borne n’est jamais un relevé complet. Revalider avant chaque futur envoi.
5. Avant activation des rappels : valider la correspondance avec le serveur de l’API et les identités Discord/Plex ; lire les exclusions/hiérarchies ; confirmer la politique de complétion et les éditions ; tester calendrier et annulation après retrait d’une collection. Aucune de ces conditions n’est remplacée par le seul succès HTTP d’un collecteur.

Ces choix sont des recommandations du projet, et doivent rester cohérents avec [la politique existante](removal-reminders.md). La recette actuelle permet d’implémenter et de diagnostiquer les sources. Elle ne permet pas encore d’affirmer « jamais commencé », de fixer une date effective de suppression, ni d’envoyer des alertes personnalisées.

[M-tag]: https://github.com/Maintainerr/Maintainerr/tree/3e11c99b8bdaf567f2463c9975a46a8259616616
[M-build]: https://github.com/Maintainerr/Maintainerr/commit/78ee15578f25493fea7196a0e4d9780d10f7460d
[M-api]: https://docs.maintainerr.info/api/#api-conventions
[M-collections-doc]: https://docs.maintainerr.info/api/collections/
[M-app]: https://github.com/Maintainerr/Maintainerr/blob/3e11c99b8bdaf567f2463c9975a46a8259616616/apps/server/src/app/app.controller.ts#L12-L20
[M-version]: https://github.com/Maintainerr/Maintainerr/blob/3e11c99b8bdaf567f2463c9975a46a8259616616/apps/server/src/modules/version/version.service.ts#L28-L54
[M-server]: https://github.com/Maintainerr/Maintainerr/blob/3e11c99b8bdaf567f2463c9975a46a8259616616/apps/server/src/modules/api/media-server/media-server.controller.ts#L259-L268
[M-mapper]: https://github.com/Maintainerr/Maintainerr/blob/3e11c99b8bdaf567f2463c9975a46a8259616616/apps/server/src/modules/api/media-server/plex/plex.mapper.ts#L376-L385
[M-collections]: https://github.com/Maintainerr/Maintainerr/blob/3e11c99b8bdaf567f2463c9975a46a8259616616/apps/server/src/modules/collections/collections.service.ts#L2048-L2140
[M-collection-entity]: https://github.com/Maintainerr/Maintainerr/blob/3e11c99b8bdaf567f2463c9975a46a8259616616/apps/server/src/modules/collections/entities/collection.entities.ts
[M-media-entity]: https://github.com/Maintainerr/Maintainerr/blob/3e11c99b8bdaf567f2463c9975a46a8259616616/apps/server/src/modules/collections/entities/collection_media.entities.ts
[M-rules-controller]: https://github.com/Maintainerr/Maintainerr/blob/3e11c99b8bdaf567f2463c9975a46a8259616616/apps/server/src/modules/rules/rules.controller.ts#L84-L124
[M-rules]: https://github.com/Maintainerr/Maintainerr/blob/3e11c99b8bdaf567f2463c9975a46a8259616616/apps/server/src/modules/rules/rules.service.ts#L177-L298
[M-exclusions-read]: https://github.com/Maintainerr/Maintainerr/blob/3e11c99b8bdaf567f2463c9975a46a8259616616/apps/server/src/modules/rules/rules.service.ts#L1759-L1797
[M-exclusion-entity]: https://github.com/Maintainerr/Maintainerr/blob/3e11c99b8bdaf567f2463c9975a46a8259616616/apps/server/src/modules/rules/entities/exclusion.entities.ts
[M-cascade]: https://github.com/Maintainerr/Maintainerr/blob/3e11c99b8bdaf567f2463c9975a46a8259616616/apps/server/src/modules/rules/helpers/exclusion-cascade.helper.ts
[M-actions]: https://github.com/Maintainerr/Maintainerr/blob/3e11c99b8bdaf567f2463c9975a46a8259616616/packages/contracts/src/collections/servarr-action.ts
[M-sonarr]: https://github.com/Maintainerr/Maintainerr/blob/3e11c99b8bdaf567f2463c9975a46a8259616616/apps/server/src/modules/actions/sonarr-action-handler.ts#L245-L278
[M-handler]: https://github.com/Maintainerr/Maintainerr/blob/3e11c99b8bdaf567f2463c9975a46a8259616616/apps/server/src/modules/collections/collection-handler.ts#L128-L198
[M-worker]: https://github.com/Maintainerr/Maintainerr/blob/78ee15578f25493fea7196a0e4d9780d10f7460d/apps/server/src/modules/collections/collection-worker.service.ts#L48-L286
[M-group]: https://github.com/Maintainerr/Maintainerr/blob/3e11c99b8bdaf567f2463c9975a46a8259616616/apps/server/src/modules/rules/entities/rule-group.entities.ts
[M-dates]: https://github.com/Maintainerr/Maintainerr/blob/3e11c99b8bdaf567f2463c9975a46a8259616616/apps/server/src/modules/collections/collections.service.ts#L120-L128
[M-date-helper]: https://github.com/Maintainerr/Maintainerr/blob/3e11c99b8bdaf567f2463c9975a46a8259616616/packages/contracts/src/collections/index.ts#L39-L46
[M-tasks]: https://github.com/Maintainerr/Maintainerr/blob/3e11c99b8bdaf567f2463c9975a46a8259616616/apps/server/src/modules/tasks/tasks.controller.ts
[M-watch]: https://github.com/Maintainerr/Maintainerr/blob/3e11c99b8bdaf567f2463c9975a46a8259616616/apps/server/src/modules/api/media-server/plex/plex-adapter.service.ts#L274-L292
[T-tag]: https://github.com/Tautulli/Tautulli/tree/9484667bc1f2d159368a999320b8f495bbfbd00a
[T-api]: https://github.com/Tautulli/Tautulli/blob/9484667bc1f2d159368a999320b8f495bbfbd00a/plexpy/api2.py#L91-L156
[T-info]: https://github.com/Tautulli/Tautulli/blob/9484667bc1f2d159368a999320b8f495bbfbd00a/plexpy/webserve.py#L6599-L6628
[T-server]: https://github.com/Tautulli/Tautulli/blob/9484667bc1f2d159368a999320b8f495bbfbd00a/plexpy/plextv.py#L32-L49
[T-users-route]: https://github.com/Tautulli/Tautulli/blob/9484667bc1f2d159368a999320b8f495bbfbd00a/plexpy/webserve.py#L6294-L6341
[T-users]: https://github.com/Tautulli/Tautulli/blob/9484667bc1f2d159368a999320b8f495bbfbd00a/plexpy/users.py#L665-L707
[T-users-schema]: https://github.com/Tautulli/Tautulli/blob/9484667bc1f2d159368a999320b8f495bbfbd00a/plexpy/__init__.py#L705-L719
[T-user-route]: https://github.com/Tautulli/Tautulli/blob/9484667bc1f2d159368a999320b8f495bbfbd00a/plexpy/webserve.py#L1638-L1684
[T-user-fallback]: https://github.com/Tautulli/Tautulli/blob/9484667bc1f2d159368a999320b8f495bbfbd00a/plexpy/users.py#L372-L417
[T-users-refresh]: https://github.com/Tautulli/Tautulli/blob/9484667bc1f2d159368a999320b8f495bbfbd00a/plexpy/users.py#L34-L88
[T-plextv-users]: https://github.com/Tautulli/Tautulli/blob/9484667bc1f2d159368a999320b8f495bbfbd00a/plexpy/plextv.py#L332-L390
[T-history]: https://github.com/Tautulli/Tautulli/blob/9484667bc1f2d159368a999320b8f495bbfbd00a/plexpy/webserve.py#L1885-L2002
[T-history-filters]: https://github.com/Tautulli/Tautulli/blob/9484667bc1f2d159368a999320b8f495bbfbd00a/plexpy/webserve.py#L2004-L2074
[T-pagination]: https://github.com/Tautulli/Tautulli/blob/9484667bc1f2d159368a999320b8f495bbfbd00a/plexpy/datatables.py#L115-L173
[T-history-data]: https://github.com/Tautulli/Tautulli/blob/9484667bc1f2d159368a999320b8f495bbfbd00a/plexpy/datafactory.py#L46-L384
[T-devices]: https://github.com/Tautulli/Tautulli/blob/9484667bc1f2d159368a999320b8f495bbfbd00a/plexpy/datafactory.py#L2471-L2484
[T-watched]: https://github.com/Tautulli/Tautulli/blob/9484667bc1f2d159368a999320b8f495bbfbd00a/plexpy/helpers.py#L1696-L1722
[T-config]: https://github.com/Tautulli/Tautulli/blob/9484667bc1f2d159368a999320b8f495bbfbd00a/plexpy/config.py#L148-L197
[T-logging]: https://github.com/Tautulli/Tautulli/blob/9484667bc1f2d159368a999320b8f495bbfbd00a/plexpy/activity_processor.py#L222-L259
[T-purge]: https://github.com/Tautulli/Tautulli/blob/9484667bc1f2d159368a999320b8f495bbfbd00a/plexpy/webserve.py#L1784-L1838
