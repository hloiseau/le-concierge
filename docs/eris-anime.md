# Anime, manga et éditions françaises

Chantier #91 de la feuille de route #78. Les métadonnées publiques et recommandations utilisent AniList ; la vérification d'une édition française passe séparément par le lecteur web existant. Aucun compte AniList ou catalogue de livres du NAS n'est connecté.

`anime` exige le type `anime` ou `manga`. Avec un titre, l'outil examine cinq candidats, leurs titres anglais/japonais et synonymes. L'année d'origine peut départager des homonymes. Plusieurs œuvres possibles demandent une précision ; le premier résultat n'est pas choisi arbitrairement.

Pour une recommandation, le titre sert de référence aux suggestions communautaires AniList ; sans référence, jusqu'à trois titres du catalogue populaire peuvent être filtrés par genre. Ces données ne constituent pas l'historique ou un profil personnel du membre. Les appels sont des requêtes GraphQL publiques prédéfinies, sans authentification ni mutation. Deux appels maximum, dix secondes et douze demandes d'outil par minute pour l'ensemble du serveur, en plus de la limite individuelle du web.

Les preuves bornées indiquent explicitement format, nombre d'épisodes/volumes renseigné et date d'origine partiellement connue. Elles ne prouvent aucune sortie française, disponibilité Plex ou présence dans Grimmory/Shelfmark. La synthèse réutilise les citations construites par l'application ; aucun outil d'ajout de livre ne découle de cette lecture.

Avec `relation:"french"`, la recherche porte sur le titre, le tome et l'édition demandés. Seules des pages effectivement lues sur les domaines d'éditeurs configurés peuvent être conservées : Glénat, Pika, Kana, Delcourt, Kurokawa, Ki-oon et Panini. Les extraits seuls, sites ressemblants, autres domaines et redirections hors éditeur sont écartés. L'absence de page exploitable est annoncée sans remplacer la date française par celle d'AniList. La couverture de ces éditeurs est bornée et la distinction tome/édition reste indispensable dans les preuves. Pour une annonce française d'anime, le web général et une source officielle appropriée restent disponibles.

Validation synthétique : titres alternatifs, homonymes/année/type, recommandation, erreur GraphQL, champ de plan non autorisé, date d'origine distincte, page éditeur inaccessible, extrait non vérifié et domaine ressemblant. Les tests d'injection et de citations du lecteur web restent applicables.

Recette réelle : recherches AniList de Cowboy Bebop et One Piece puis recommandations associées réussies, sans compte ni données privées. La recette de recherche d'une édition française sur le NAS n'a pas fourni de page d'éditeur exploitable ; aucune date française réelle n'est déclarée certifiée par cette recette. Un moteur SearXNG a également signalé un quota temporaire, tandis que l'autre moteur continuait à donner des résultats. Aucun contournement, fournisseur payant de secours ou message Discord de test ajouté.

Contrats : [requêtes Media officielles](https://github.com/AniList/docs/blob/master/docs/guide/graphql/queries/media.md), [métadonnées et dates](https://github.com/AniList/docs/blob/master/docs/reference/object/media.md), [recommandations](https://github.com/AniList/docs/blob/master/docs/reference/object/recommendation.md), [dates non annoncées chez Pika](https://www.pika.fr/faq/je-cherche-une-date-de-publication-dun-manga-sur-votre-site-mais-je-ne-la-trouve-pas/).
