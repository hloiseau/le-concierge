# Contrats livres : Grimmory et Shelfmark

Audit des sources du 16 octobre 2026, complété par les observations de l’installation au 18 octobre 2026, lié aux issues Concierge [11](https://github.com/hloiseau/le-concierge/issues/11), [12](https://github.com/hloiseau/le-concierge/issues/12), [13](https://github.com/hloiseau/le-concierge/issues/13) et Hub [8](https://github.com/hloiseau/le-hub/issues/8), [9](https://github.com/hloiseau/le-hub/issues/9).

## Périmètre et preuves

Sources de référence : Grimmory **v3.5.0**, ref `402e89b4452f8e2b17ab95f16c1621c003516cd2`, et Shelfmark **v1.4.0**, ref `d2b4a0290ec118c541803f31329d6e89e5fe9701`. Les images en cours d’exécution sur le NAS ont été vérifiées : leurs versions et révisions correspondent à ces sources. La documentation REST Grimmory annonce explicitement un contrat instable. [Version Grimmory](https://github.com/grimmory-tools/grimmory/releases/tag/v3.5.0), [version Shelfmark](https://github.com/calibrain/shelfmark/releases/tag/v1.4.0), [API Grimmory](https://grimmory.org/api/).

Les lectures sans authentification ont confirmé les frontières suivantes : Grimmory `GET /api/v1/version` répond **401** ; les routes OPDS `catalog` et `recent` répondent **401** avec une demande d’authentification HTTP Basic. La route `search.opds` répond **403** ; ce refus ne permet pas de conclure sur une recherche avec un compte autorisé. Les paramètres publics indiquent OIDC et authentification distante désactivés. Shelfmark `GET /api/auth/check` répond **200**, avec `auth_required=true`, `auth_mode=builtin`, `authenticated=false` ; `GET /api/users/me/edit-context` et `GET /api/requests` répondent **401**.

Aucun identifiant personnel n’a été utilisé, aucun contenu de catalogue ou de demande n’a été lu et aucun téléchargement ou import n’a été lancé. Ces contrôles confirment les versions déployées et le refus des lectures anonymes ; ils ne valident ni la connexion OPDS, ni l’isolation entre utilisateurs, ni les règles d’approbation ou l’import réel. C11 reste ouvert pour ces vérifications.

Les fixtures de tests sont **synthétiques**, conformes aux sources épinglées, et ne prétendent pas être des captures de production anonymisées. Elles couvrent plusieurs éditions, séries, ISBN, formats, réponses vides, refus et erreurs. Pour terminer C11, il reste à vérifier les parcours personnels et fournir, depuis deux comptes de test aux bibliothèques différentes, des réponses OPDS nettoyées de toute donnée privée. Ne jamais joindre cookies, mots de passe, jetons Kobo, domaines privés ou chemins de fichiers.

## Authentification et droits

Grimmory propose des identifiants **OPDS dédiés**, distincts de la connexion web. Ils utilisent HTTP Basic et héritent des bibliothèques autorisées au compte Grimmory créateur. La permission « Access OPDS » est nécessaire. Un compte créé par un administrateur hérite de ses droits : chaque membre doit créer ses propres identifiants. [Guide OPDS officiel](https://grimmory.org/docs/integration/opds/).

Le catalogue et les couvertures appliquent les droits amont. Le code vérifié contrôle les bibliothèques et restrictions de contenu avant de délivrer une couverture ou un livre. Les totaux OPDS sont calculés avant certains filtres : Le Concierge expose une pagination avec `hasMore`, sans publier le total brut ; une page peut être vide alors qu’une suivante existe. [Contrôles OPDS v3.5.0](https://github.com/grimmory-tools/grimmory/blob/v3.5.0/backend/src/main/java/org/booklore/service/opds/OpdsBookService.java).

La navigation brute des séries (`getDistinctSeries`) filtre les bibliothèques mais n’applique pas les restrictions de contenu du catalogue : elle ne doit pas être exposée directement. Les regroupements utilisent les notices déjà autorisées ; le filtre `catalog?series=…` applique les restrictions du catalogue. Le champ Atom `updated` provient de `addedOn`, ou de l’instant courant lorsqu’il manque : ce n’est pas une date de modification des métadonnées. Une date de collecte doit rester présentée comme telle.

Les derniers livres de l’accueil utilisent `GET /api/v1/books/recent`, qui lit le flux personnel `GET /api/v1/opds/recent?page=N&size=20`. Grimmory sélectionne les ajouts récents avant d’appliquer le classement OPDS choisi par le compte ; le Hub conserve ce classement et les éditions distinctes. Il n’affiche pas `updated` comme une date d’ajout garantie, puisque Grimmory remplace une date absente par l’instant courant. La réponse expire au plus après une minute ou à la fin de l’association personnelle ; erreur, retrait de l’association et déconnexion effacent la liste. La pagination conserve les pages vides dues aux restrictions de contenu. [Flux récents v3.5.0](https://github.com/grimmory-tools/grimmory/blob/v3.5.0/backend/src/main/java/org/booklore/service/opds/OpdsFeedService.java), [filtrage personnel](https://github.com/grimmory-tools/grimmory/blob/v3.5.0/backend/src/main/java/org/booklore/service/opds/OpdsBookService.java).

Shelfmark conserve ses sessions utilisateurs et règles de demandes. Sa clé `SHELFMARK_API_KEY` agit comme administrateur, même si elle accompagne un cookie personnel : elle n’est jamais utilisée par Le Concierge. La clé distincte en lecture seule ne permet que les statistiques, pas un catalogue personnel. Les modes de politique incluent téléchargement direct, demande de livre, demande d’une édition et blocage. [Accès API v1.4.0](https://github.com/calibrain/shelfmark/blob/v1.4.0/docs/api-access.md), [droits et demandes v1.4.0](https://github.com/calibrain/shelfmark/blob/v1.4.0/docs/users-and-requests.md).

## Matrice des fonctions

« Confirmé » signifie présent dans les sources citées. L’ensemble des opérations personnelles ci-dessous reste **non testé sur l’installation réelle**. « Non confirmé » ne signifie pas « absent ».

| Fonction | Grimmory v3.5.0 | Shelfmark v1.4.0 | Raccordement Concierge / repli |
| --- | --- | --- | --- |
| Recherche et catalogue | Confirmé : OPDS `GET /api/v1/opds/catalog?q=…&page=1&size=20`, authentification personnelle. | Confirmé : `GET /api/metadata/search?query=…` dépend des fournisseurs et de la session. | Catalogue OPDS personnel ; recherche Shelfmark par lien prérempli. |
| Demande | Workflow de demande utilisateur non confirmé. | Confirmé : `GET /api/request-policy`, `POST /api/requests`, `GET /api/requests`, contrôle de politique et propriétaire. | Pas de création API sous identité partagée. Choix de l’édition et confirmation dans Shelfmark. |
| Téléchargement | Confirmé : OPDS `GET /api/v1/opds/{id}/download?fileId=…`. | Confirmé : `POST /api/releases/download`, sous politique utilisateur. | Fiche web officielle ; aucun fichier téléchargé par le bot ou Le Hub. |
| Import | Confirmé : BookDrop et finalisation d’import. | Confirmé : livraison après téléchargement ; destination personnelle configurable. Compatibilité précise avec Grimmory installé non confirmée. | Pas d’état « importé » déduit d’un téléchargement terminé. Vérification réelle requise pour C14. |
| Notifications | Résumé BookDrop documenté ; webhook de disponibilité personnelle non confirmé. | Notifications personnelles et progression de livraison documentées. | Aucun nouvel envoi ou suivi de disponibilité de livre. |
| Collections | Confirmé : bibliothèques, étagères, séries OPDS. | Catalogue personnel/collections de lecture non confirmés. | Séries et tomes publiés dans OPDS, sans inventer une collection personnelle. |
| Lecture | Lecteurs web et progression documentés. | Lecteur et progression non confirmés. | Fiche `/book/{id}` Grimmory, avec sa connexion web propre. |
| Kobo | Intégration native et jeton de périphérique documentés. | Intégration Kobo directe non confirmée. | Configuration dans Grimmory ; aucune promesse de synchronisation ni statut Kobo simulé. |

Preuves détaillées des routes : [contrôleur OPDS](https://github.com/grimmory-tools/grimmory/blob/v3.5.0/backend/src/main/java/org/booklore/controller/OpdsController.java), [routes de demandes Shelfmark](https://github.com/calibrain/shelfmark/blob/v1.4.0/shelfmark/core/request_routes.py), [BookDrop](https://github.com/grimmory-tools/grimmory/tree/v3.5.0#bookdrop), [intégration Kobo](https://grimmory.org/docs/integration/kobo/), [routes web Grimmory](https://github.com/grimmory-tools/grimmory/blob/v3.5.0/frontend/src/app/app.routes.ts).

Le flux OPDS fournit `urn:booklore:book:{id}`, auteurs, série/position, ISBN10 lorsqu’il est renseigné et liens d’acquisition par fichier. Le client conserve deux IDs d’éditions différents, même si titres ou ISBN sont identiques. Les formats affichés sont ceux annoncés par ces liens ; ils ne prouvent ni la présence physique actuelle du fichier ni sa synchronisation Kobo. Un livre sans format demeure une fiche de métadonnées. Aucun chemin NAS ni résumé brut n’est exposé. [Génération des flux v3.5.0](https://github.com/grimmory-tools/grimmory/blob/v3.5.0/backend/src/main/java/org/booklore/service/opds/OpdsFeedService.java).

## Configuration et contrat livré

- `GRIMMORY_URL` : base du service OPDS, sans `/api/v1/opds`. HTTPS requis hors développement. Aucun secret global Grimmory.
- `SERVICE_LINKS_JSON.grimmory` : URL publique de lecture ; le client construit uniquement la fiche `/book/{id}`.
- `SERVICE_LINKS_JSON.shelfmark` : URL publique de demande. Le repli ajoute le fragment `#q=…&search_by=general`. Ce fragment déclenche une recherche, **pas une demande**. Les paramètres sont encodés et les règles d’approbation restent dans Shelfmark. [Contrat officiel des liens v1.4.0](https://github.com/calibrain/shelfmark/blob/v1.4.0/docs/url-search-parameters.md).
- Le Hub `/livres` permet l’association OPDS, le catalogue et le retrait de l’association. `/livres [recherche] [page]` dans Discord utilise les mêmes identifiants ; `/demander-livre recherche:…` retourne le lien Shelfmark en réponse privée.

| API Concierge | Résultat |
| --- | --- |
| `GET /api/v1/books/link` | `{linked, expiresAt?}` ; jamais d’identifiant OPDS ou mot de passe. |
| `POST /api/v1/books/link` | `{username,password}` vérifiés par une lecture de catalogue ; même résultat. |
| `DELETE /api/v1/books/link` | Retrait immédiat, y compris d’une association encore en cours. |
| `GET /api/v1/books?q=…&page=1` | `{items,page,pageSize:20,hasMore}` ; page de 1 à 1001. |
| `GET /api/v1/books/{id}/cover` | Image raster vérifiée, droits du compte OPDS, `private, no-store`. |
| `GET /api/v1/book-requests/link?q=…` | `{url,mode:"external",message}` ; aucune requête réseau à Shelfmark. |

Un livre expose `id`, `title`, `authors`, `formats`, éventuellement `series`, `volume`, `isbn`, `url`, `coverUrl`. Aucun état de demande n’est enregistré : le repli ne crée rien, donc il ne revendique ni corrélation avec une demande distante ni déduplication de créations. Ces opérations restent à implémenter seulement après validation d’une authentification déléguée et d’un contrat de création sur la version installée.

L’association OPDS est conservée uniquement dans la mémoire du processus, liée au jeton Plex courant et à son expiration. Déconnexion, retrait, révocation Plex, changement d’identité ou redémarrage la suppriment. Un refus OPDS la supprime aussi. Aucun secret n’est enregistré dans SQLite, localStorage, URL ou réponse API. Les lectures sont limitées à six secondes et 2 Mio ; un redirect n’est jamais suivi avec les identifiants. Les résultats tardifs sont rejetés après changement de session ou retrait du lien. Les images sont servies uniquement comme JPEG, PNG, GIF ou WebP vérifiés par leur contenu.

## Vérification

Tests ciblés : `go test ./internal/upstream ./internal/httpapi ./internal/discord` ; tests Hub `tests/books.spec.ts` dans les projets desktop et mobile. Ils couvrent isolation, pagination, doublons d’édition, contenu vide, erreur, révocation, retrait pendant lecture, réponse tardive, borne de taille, redirection, image non autorisée, échappement HTML et lien Shelfmark sans création distante. Les versions installées et les refus anonymes sont maintenant vérifiés ; les tests d’intégration réels avec deux comptes OPDS, l’import et Kobo restent à consigner.
