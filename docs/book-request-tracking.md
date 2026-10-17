# Suivi personnel des demandes de livres

Ce lot avance [C14](https://github.com/hloiseau/le-concierge/issues/14) avec la consultation personnelle des demandes et livraisons Shelfmark. Il ne termine pas la corrélation avec Grimmory ni les événements de disponibilité. Voir aussi l’[audit des contrats livres](books-contracts.md).

## Contrat confirmé

Référence : **Shelfmark v1.4.0**, sources consultées le 16 octobre 2026. La version installée reste non vérifiée avec une session authentifiée. Le mode public observé sur l’installation était `builtin`, sans utilisateur connecté.

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

Chaque élément expose `id`, `title`, `authors`, `status`, `deliveryState`, `createdAt`, éventuellement `reviewedAt` et `deliveryUpdatedAt`, et `catalogStatus:"unverified"`. Notes, chemins, détails de release, identifiants de source, paramètres de livraison et identité du propriétaire ne sont pas renvoyés.

Les changements d’association exigent l’origine du Hub. Cinq connexions par minute au maximum évitent les tentatives répétées ; Shelfmark conserve ses propres limites. Une consultation lit trois GET, avec un délai total de six secondes et 2 Mio par réponse. Le login utilise quatre appels : contrôle du mode, authentification, contrôle de session et lecture de l’identité. Aucun redirect n’est suivi, même avec un client HTTP injecté. Une page comprend au plus vingt éléments, et une ligne supplémentaire sert uniquement à annoncer une suite. Une liste peut changer entre deux pages : elle n’est pas un instantané transactionnel.

La consultation se fait à l’ouverture ou sur **Actualiser les demandes**. Les réponses tardives sont rejetées après retrait, expiration, changement de compte ou déconnexion. Une panne retire la liste affichée, sans restitution de données anciennes non revérifiées.

## Pourquoi l’import reste non vérifié

Le connecteur Shelfmark vers Grimmory téléverse soit dans une bibliothèque, soit dans **BookDrop**. Sa fonction `booklore_upload_file` vérifie le succès HTTP mais retourne `None` : aucun ID livre Grimmory n’est enregistré dans la demande. Le succès de livraison peut donc précéder la validation et l’import BookDrop. [Connecteur officiel v1.4.0](https://github.com/calibrain/shelfmark/blob/v1.4.0/shelfmark/download/outputs/booklore.py).

Le catalogue OPDS Grimmory confirme les notices autorisées et leurs formats annoncés. Un ID fournisseur Shelfmark n’est pas un ID livre Grimmory, et un titre commun ne prouve pas une édition commune. Les sources auditées ne garantissent pas que l’ISBN des métadonnées d’une demande identifie précisément le fichier livré. Aucun rapprochement automatique par titre, nom de fichier ou ISBN non vérifié n’est donc réalisé.

Pour achever C14, il reste à obtenir une corrélation stable issue de l’import, ou une validation explicite de correspondance vers une notice Grimmory accessible au membre ; définir sa durée de validité ; puis ajouter le polling borné avec curseur durable et l’événement métier de disponibilité, revérifié avec les droits personnels. Les notifications C20 ne doivent pas être déclenchées par `deliveryState=complete`. Aucun curseur ni événement fictif n’est écrit par ce lot.

## Vérification

Tests Go : `go test ./internal/upstream ./internal/httpapi ./internal/config -run TestShelfmark`. Couverture : mode de connexion, cookie personnel, refus de redirect, identité changeante, autre propriétaire, états inconnus, dates, pagination, nettoyage des champs privés, origine, retrait/révocation et réponses tardives.

Tests Hub : `tests/book-requests.spec.ts` dans les projets desktop et mobile ; connexion, états distincts, texte HTML échappé, pagination, panne, révocation, mot de passe effacé, déconnexion pendant une réponse et désactivation de la capacité. Aucun appel réel de demande, téléchargement ou import n’est effectué. La validation sur les versions installées et deux comptes aux demandes différentes reste nécessaire avant activation en production.
