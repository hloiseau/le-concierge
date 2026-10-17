# Collections et continuité de lecture

Le parcours `/collections` du Hub regroupe les séries présentes sur une page de notices Grimmory autorisées. Il permet de choisir une série exacte, parcourir ses livres, ouvrir leurs fiches pour lire ou corriger les métadonnées, et consulter le guide Kobo. L’association OPDS personnelle se fait dans `/livres` ; les mêmes identifiants en mémoire et les mêmes droits sont utilisés. Aucun compte administrateur partagé ni nouveau stockage n’est ajouté.

## Données et incertitude

Une **œuvre** n’est pas une **édition**. Le flux consulté ne fournit pas d’identité d’œuvre fiable : Le Concierge ne fusionne jamais deux notices sur leur titre, auteur, ISBN ou position. Chaque notice conserve son identifiant Grimmory ; ses formats restent ceux annoncés par le catalogue, pas une preuve de copie présente sur une liseuse. ISBN, éditeur et langue sont affichés lorsqu’ils sont fournis, sinon explicitement inconnus.

La **série** est le libellé de métadonnées Grimmory. Ce nom n’est pas une identité éditoriale universelle ; des homonymes peuvent exister. Le **tome** reste une chaîne source : `1.5` ne devient pas `1`, et un libellé hors-série n’est pas transformé en numéro. Le contrat accepte les libellés, mais la version Grimmory v3.5.0 auditée produit une position numérique : aucun statut hors-série n’est déduit d’un zéro, d’une fraction ou d’une absence.

La provenance est `grimmory_opds`, et `fetchedAt` indique la lecture actuelle. La balise OPDS `updated` est alimentée par `addedOn` dans la version auditée ; elle ne prouve pas la dernière correction des métadonnées et n’est donc pas présentée comme telle. Le bouton « Lire ou corriger dans Grimmory » ouvre la fiche officielle ; les corrections s’effectuent avec les droits du compte Grimmory et sont relues à l’actualisation.

Aucune référence bibliographique complète des tomes publiés n’a été validée. `referenceStatus: unavailable` signifie que **les manques sont inconnus**, même si toute la pagination de la bibliothèque a été parcourue. Aucune liste vide de « tomes manquants » ne laisse entendre qu’une collection est complète. Les nouveautés éditoriales, leur source et leur cadence restent à définir ; les dates de sortie et téléchargements automatiques ne sont pas inventés. C15 reste partiellement réalisé sur ces points.

## Contrat et accès

`GET /api/v1/book-collections?series=Nom%20exact&page=1` renvoie `items: [{name, books}]`, `page`, `pageSize: 20`, `hasMore`, `source`, `fetchedAt`, `expiresAt` et `referenceStatus`. Sans filtre `series`, seuls les noms observés dans les notices accessibles de cette page sont regroupés. Les notices sans série restent consultables dans le catalogue `/livres`. Une page vide peut avoir une suite, en raison des filtres de droits en amont ; aucun total brut de bibliothèque n’est exposé.

Le client utilise uniquement le catalogue OPDS filtré, jamais la navigation brute `/opds/series` : dans Grimmory v3.5.0, `getDistinctSeries` filtre les bibliothèques mais n’applique pas les restrictions de contenu ; `getBooksBySeriesName` applique `applyBookFilters`. Une réponse dont les notices ne correspondent pas au filtre exact est refusée. Le filtre utilisateur ne choisit ni propriétaire, ni URL, ni identifiants d’un autre compte. Les lectures restent limitées à six secondes, 2 Mio et 20 notices, avec contrôle de l’association après la réponse.

Sources : [génération OPDS v3.5.0](https://github.com/grimmory-tools/grimmory/blob/v3.5.0/backend/src/main/java/org/booklore/service/opds/OpdsFeedService.java), [filtres OPDS v3.5.0](https://github.com/grimmory-tools/grimmory/blob/v3.5.0/backend/src/main/java/org/booklore/service/opds/OpdsBookService.java), [audit et limites de production](books-contracts.md).

## Kobo

Le Hub propose `/guides/kobo`, qui renvoie à la [procédure officielle Grimmory](https://grimmory.org/docs/integration/kobo/) et au Grimmory configuré. Aucun jeton de périphérique n’entre dans Le Hub. Le guide est conditionnel : les options et la version réellement installées doivent être vérifiées. Aucune API de statut Kobo personnel n’est raccordée, donc aucun état de synchronisation n’est affiché. La modification de la liseuse, les envois et leur vérification restent dans Grimmory et Kobo.

Les tests fictifs vérifient deux éditions de même titre et ISBN, langues distinctes, position fractionnaire, libellé hors-série, page filtrée vide avec suite, droits personnels, retrait d’association pendant lecture, repli vers Grimmory, retrait visuel après erreur et déconnexion. Les essais réels du NAS, les références éditoriales complètes et Kobo restent à effectuer.
