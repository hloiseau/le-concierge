# Suivi personnel des demandes de livres

Ce lot avance [C14](https://github.com/hloiseau/le-concierge/issues/14) avec la consultation personnelle des demandes Shelfmark et la confirmation explicite d’une édition Grimmory. Le contrat API passe à **0.11.0** et SQLite à **v9**. Il ne déduit pas automatiquement l’identité d’un fichier importé et n’envoie pas encore de notification de disponibilité. Voir aussi l’[audit des contrats livres](books-contracts.md).

## Contrat confirmé

Référence : **Shelfmark v1.4.0**, sources consultées le 16 octobre 2026. La version et la révision de l’image en cours d’exécution sur le NAS ont été confirmées au 18 octobre. La route publique indique `auth_required=true`, `auth_mode=builtin`, `authenticated=false` ; les routes de contexte personnel et de demandes refusent la lecture anonyme avec HTTP 401. Aucune session personnelle n’a été ouverte : connexion, propriétaire des demandes et isolation entre comptes restent à vérifier sur l’installation.

Le [contrôleur d’authentification v1.4.0](https://github.com/calibrain/shelfmark/blob/v1.4.0/shelfmark/main.py) expose `GET /api/auth/check` et `POST /api/auth/login` avec `{username,password,remember_me:false}`. Le Concierge prend en charge uniquement le mode `builtin` avec connexion locale activée. Il refuse les modes sans authentification, proxy, CWA et OIDC ; ces parcours restent dans Shelfmark. Le login crée une session distincte de celle du navigateur Shelfmark.

L’identité est confirmée par `GET /api/users/me/edit-context` : le compte doit être actif, son nom doit correspondre à la connexion et son ID doit rester identique à chaque consultation. Le client ignore tous les autres champs, notamment les préférences de livraison susceptibles de contenir des secrets. [Route personnelle v1.4.0](https://github.com/calibrain/shelfmark/blob/v1.4.0/shelfmark/core/self_user_routes.py).

`GET /api/requests?limit=21&offset=…` filtre toujours sur le `db_user_id` de la session, y compris pour un administrateur connecté personnellement. Le Concierge vérifie en plus le propriétaire de chaque ligne, refuse les IDs répétés et ne lit jamais `/api/admin/requests`. Les demandes sont ordonnées par date de création puis ID décroissants. Les téléchargements directs ne produisent pas nécessairement une demande. [Routes v1.4.0](https://github.com/calibrain/shelfmark/blob/v1.4.0/shelfmark/core/request_routes.py), [stockage et ordre v1.4.0](https://github.com/calibrain/shelfmark/blob/v1.4.0/shelfmark/core/user_db.py).

Les états de demande sont `pending`, `fulfilled`, `rejected`, `cancelled`. La livraison reste un champ distinct : `none`, `queued`, `resolving`, `locating`, `downloading`, `complete`, `error`, `cancelled`. Une demande `fulfilled` est prise en charge ; son fichier peut encore être en cours de livraison. Les dates `created_at`, `reviewed_at` et `delivery_updated_at` sont conservées séparément et normalisées en UTC. SQLite crée `created_at` en UTC, sans suffixe de fuseau ; les dates ISO de décision précisent leur décalage. Une date inconnue ou un nouvel état amont invalide la page, sans état inventé. [États des demandes](https://github.com/calibrain/shelfmark/blob/v1.4.0/shelfmark/core/request_validation.py), [états de file](https://github.com/calibrain/shelfmark/blob/v1.4.0/shelfmark/core/models.py), [dates de décision](https://github.com/calibrain/shelfmark/blob/v1.4.0/shelfmark/core/requests_service.py).

## Configuration et parcours livré

Configurer `SHELFMARK_URL`, sans suffixe `/api`, avec HTTPS hors développement. `SERVICE_LINKS_JSON.shelfmark` reste le lien public pour ouvrir Shelfmark. **Aucune clé API Shelfmark globale** n’est utilisée : elle agirait comme administrateur et ne représenterait pas le membre connecté.

Dans le Hub, **Livres → Suivre mes demandes de livres** ouvre `/demandes-livres`. Le membre saisit ses identifiants Shelfmark personnels ; son mot de passe ne sert qu’à créer la session et n’est pas conservé. Le cookie est stocké seulement en mémoire du processus, lié au jeton Plex courant et à son expiration. Déconnexion du Hub, retrait de l’association, changement d’identité, expiration ou redémarrage le suppriment. Un refus amont retire aussi l’association. Aucun cookie ne retourne au navigateur du Hub, à SQLite, aux URL ou aux journaux. Retirer l’association supprime le cookie détenu par Le Concierge ; cela ne déconnecte pas les autres navigateurs Shelfmark.

| API Concierge | Contrat |
| --- | --- |
| `GET /api/v1/book-requests/link-session` | `{linked,expiresAt?}` ; état local de l’association. |
| `POST /api/v1/book-requests/link-session` | `{username,password}` ; vérification personnelle, puis même réponse. |
| `DELETE /api/v1/book-requests/link-session` | Retrait immédiat, même pendant une connexion en cours. |
| `GET /api/v1/book-requests?page=1` | `{items,page,pageSize:20,hasMore,checkedAt}` ; page de 1 à 1001. |
| `POST /api/v1/book-requests/{id}/catalog-match` | `{requestPage,bookId,catalogPage,catalogQuery}` ; confirmation d’une édition relue avec les deux comptes source personnels. |
| `DELETE /api/v1/book-requests/{id}/catalog-match` | Retrait de la correspondance et invalidation des confirmations concurrentes en cours. |

Chaque élément expose `id`, `title`, `authors`, `status`, `deliveryState`, `createdAt`, éventuellement `reviewedAt` et `deliveryUpdatedAt`, ainsi que `catalogStatus`. Sans correspondance personnelle vérifiée, celui-ci vaut `unverified`. Une correspondance expose sa méthode `user_confirmed`, sa date de confirmation et celle de vérification ; `available` exige une preuve de moins de cinq minutes et une livraison toujours terminée. Sinon, le statut devient `check_required` et les détails du livre ne sont plus exposés. Notes, chemins, détails de release, paramètres de livraison et identité du propriétaire ne sont pas renvoyés.

Les changements d’association exigent l’origine du Hub. Cinq connexions par minute au maximum évitent les tentatives répétées ; Shelfmark conserve ses propres limites. Une consultation lit trois GET, avec un délai total de six secondes et 2 Mio par réponse. Le login utilise quatre appels : contrôle du mode, authentification, contrôle de session et lecture de l’identité. Aucun redirect n’est suivi, même avec un client HTTP injecté. Une page comprend au plus vingt éléments, et une ligne supplémentaire sert uniquement à annoncer une suite. Une liste peut changer entre deux pages : elle n’est pas un instantané transactionnel.

La consultation se fait à l’ouverture ou sur **Actualiser les demandes**. Les réponses tardives sont rejetées après retrait, expiration, changement de compte ou déconnexion. Une panne retire la liste affichée, sans restitution de données anciennes non revérifiées.

## Pourquoi l’import reste non vérifié

Le connecteur Shelfmark vers Grimmory téléverse soit dans une bibliothèque, soit dans **BookDrop**. Sa fonction `booklore_upload_file` vérifie le succès HTTP mais retourne `None` : aucun ID livre Grimmory n’est enregistré dans la demande. Le succès de livraison peut donc précéder la validation et l’import BookDrop. [Connecteur officiel v1.4.0](https://github.com/calibrain/shelfmark/blob/v1.4.0/shelfmark/download/outputs/booklore.py).

Le catalogue OPDS Grimmory confirme les notices autorisées et leurs formats annoncés. Un ID fournisseur Shelfmark n’est pas un ID livre Grimmory, et un titre commun ne prouve pas une édition commune. Les sources auditées ne garantissent pas que l’ISBN des métadonnées d’une demande identifie précisément le fichier livré. Aucun rapprochement automatique par titre, nom de fichier ou ISBN non vérifié n’est donc réalisé.

## Correspondance confirmée et revérification

Le membre choisit lui-même l’édition dans son catalogue Grimmory, après une demande Shelfmark `fulfilled` et une livraison `complete`. La confirmation relit la demande et la page OPDS choisie avec ses droits personnels ; la notice doit annoncer au moins un format. Une recherche sert seulement à naviguer. Ni un titre commun, ni une livraison terminée ne suffisent à créer une correspondance.

SQLite conserve les métadonnées normalisées, l’identité de la correspondance et les curseurs des deux sources, sans cookie ni mot de passe. La portée inclut le membre Plex, les comptes personnels et les URL des sources. Un changement d’identité Plex efface les correspondances ; changer de compte source empêche de réutiliser celles de l’ancien compte. Le retrait invalide également une confirmation tardive et les écritures de polling concurrentes.

Chaque minute, le worker traite au plus vingt correspondances anciennes par membre, dans un budget global de 45 secondes, avec rotation des membres. Une vérification lit une page de demandes et une page de catalogue, chacune bornée, et reprend le curseur si l’élément a changé de page. Les correspondances vérifiées depuis moins d’une minute sont ignorées. La preuve expire au bout de cinq minutes : une source lente ou une longue pagination peut donc afficher `check_required`, sans prétendre que le livre a disparu. Les droits en mémoire sont indispensables ; après redémarrage, le suivi reprend quand le membre relie ses deux comptes personnels.

La confirmation crée un événement durable unique par paire demande/édition. Une reconfirmation conserve son identité pour la déduplication ; le retrait ou le remplacement l’annule. Aucun consommateur de notification n’est encore branché. Pour C20, une future livraison devra relire la preuve actuelle et les droits personnels, respecter les préférences et dédupliquer cet événement ; son existence historique seule n’autorise pas un envoi.

Limites : 100 correspondances actives par compte source, 1 000 avec l’historique par membre. Les correspondances retirées sont purgées après 90 jours. Le Hub masque les détails à l’expiration, au retour au premier plan et dès qu’une livraison cesse d’être terminée. Cette confirmation humaine ne prouve pas la provenance du fichier téléversé par Shelfmark.

## Vérification

Tests Go : `go test ./internal/upstream ./internal/httpapi ./internal/config -run TestShelfmark`. Couverture : mode de connexion, cookie personnel, refus de redirect, identité changeante, autre propriétaire, états inconnus, dates, pagination, nettoyage des champs privés, origine, retrait/révocation et réponses tardives.

Tests Hub : `tests/book-requests.spec.ts` dans les projets desktop et mobile ; connexion, états distincts, texte HTML échappé, pagination, panne, révocation, mot de passe effacé, déconnexion pendant une réponse et désactivation de la capacité. Aucun appel réel de demande, téléchargement ou import n’est effectué. La version installée est confirmée ; la validation personnelle avec deux comptes aux demandes différentes reste nécessaire.

Le 24 octobre, la suite Go complète passe (1 952 cas) ainsi que `go vet ./...`. Les tests supplémentaires couvrent la migration v8→v9, la portée personnelle, les confirmations/retraits concurrents, la déduplication des événements, la reprise des curseurs et les bornes du polling. Côté Hub, lint, types, build et 62 tests navigateur ciblés sur les livres passent sur ordinateur et mobile, dont l’expiration après suspension d’un onglet. La base de production reste en v8 : ce lot n’est pas déployé et la recette avec de vraies sessions Shelfmark/Grimmory reste à effectuer.
