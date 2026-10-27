# Fiches cinéma et séries dans Eris

Chantier #90 de la feuille de route #78, après le web/YouTube #89. Les données passent par l'intégration Seerr existante ; aucune nouvelle clé TMDB ou plateforme de streaming n'est ajoutée.

Le modèle v2 dispose du plan `cinema`, avec un titre, un type facultatif `movie`/`show`, une année facultative et un ciblage facultatif : `cast`, `director`, `trailer` ou `overview`. Une discussion ou un avis reste une conversation. Les disponibilités et demandes utilisent toujours les outils Plex/Seerr existants.

La recherche examine la première page Seerr (vingt résultats maximum) avant de sélectionner le titre. Les correspondances exactes sont privilégiées ; des homonymes ou plusieurs correspondances année/type demandent une précision. Aucun choix n'est enregistré comme une demande média. Une relance précise peut être traitée à partir du contexte récent.

L'identité Discord, l'accès Plex et l'association Seerr sont vérifiés selon le parcours existant, y compris la revalidation après lecture. Les appels catalogue sont effectués avec l'identité Seerr du membre. La fiche renvoyée retient le synopsis, les crédits, la durée et une bande-annonce référencée. Pour un film, la réalisation vient des crédits `Director` ; pour une série, la création vient de `createdBy`. Un crédit ou une bande-annonce absent est annoncé comme tel. Le lien vidéo est construit à partir d'une clé YouTube valide fournie par Seerr, sans utiliser une URL arbitraire du catalogue. Sa présence dans la fiche ne garantit pas que la vidéo reste lisible sur YouTube.

La restitution est construite par l'application avec un lien vers la fiche TMDB. Elle ne renvoie ni les demandes des autres membres, ni leurs données de compte, ni les champs de fournisseurs de streaming. Aucune route de disponibilités Netflix/Prime/etc. n'est consultée et aucune note ou réception critique n'est inventée.

Validation synthétique : homonymes, année/type, titre au-delà des trois premiers résultats de demandes, identité de fiche incohérente, crédit/rôle absent, bande-annonce absente ou clé malformée, conservation du ciblage par le modèle, absence de fournisseurs et de données personnelles. `go test ./internal/upstream ./internal/eris ./internal/httpapi` et `go vet ./...` passent.

Le 27 octobre 2026, le contrat de Seerr **v3.5.1**, révision `564bfb08b54caa374bddaf820abfd226e5e69151`, a été vérifié en lecture sur le NAS : Dune (2021, TMDB 438631) expose crédits et bandes-annonces ; Dark (TMDB 70523) expose `createdBy`, distribution et bande-annonce. Seuls des champs de catalogue publics ont été retenus pour ce contrôle. Aucun message Discord ou ajout média effectué.

Contrats : [fiche film](https://docs.seerr.dev/api/get-movie-details/), [fiche série](https://docs.seerr.dev/api/get-tv-details/), [schéma de la version installée](https://github.com/seerr-team/seerr/blob/564bfb08b54caa374bddaf820abfd226e5e69151/seerr-api.yml).
