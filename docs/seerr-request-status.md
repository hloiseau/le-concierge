# États des demandes Seerr

Le suivi lit uniquement les demandes de l’identité Seerr rattachée au compte Plex vérifié, avec contrôle du propriétaire de chaque ligne. Une page contient au plus dix demandes ; son enrichissement est limité à onze lectures et six secondes. Un titre absent reste un libellé avec l’identifiant TMDB, sans réutiliser une autre identité.

Le code de demande Seerr `FAILED=4` donne « En erreur » et `DECLINED=3` donne « Refusée », même si le média possède un autre état. Pour les autres demandes, les états média `PROCESSING=3`, `PARTIALLY_AVAILABLE=4` et `AVAILABLE=5` donnent respectivement en cours, disponibilité partielle et disponible. Une demande 4K lit `status4k` : la présence de la version HD ne suffit pas. Sans état reconnu, aucune disponibilité n’est déduite.

Ces valeurs et la priorité échec/refus sont vérifiées dans les sources officielles Seerr : [constantes](https://github.com/seerr-team/seerr/blob/develop/server/constants/media.ts) et [carte de demande](https://github.com/seerr-team/seerr/blob/develop/src/components/RequestCard/index.tsx), consultées le 16 octobre 2026. Cette dernière confirme également les liens de fiche `/movie/{tmdbId}` et `/tv/{tmdbId}`. Le Hub les construit depuis le lien public Seerr configuré et l’identifiant de la demande autorisée ; aucun lien interne Radarr/Sonarr n’est transmis.

Le détail du Hub décrit l’état fourni par Seerr. Il ne prouve ni la lecture effective dans Plex, ni la disponibilité de chaque épisode demandé, ni une cause technique précise d’échec. L’administrateur peut utiliser `/diagnostic demande:<id>` pour une vérification séparée. Les filtres portent explicitement sur la page chargée.

La version installée et la validation avec deux vrais comptes restent à effectuer sur l’instance administrée. Les tests locaux utilisent des services simulés, notamment l’échec, le refus, les états HD/4K différents, les valeurs inconnues et une page amont trop grande.
