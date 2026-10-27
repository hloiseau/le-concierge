# Diagnostic pédagogique pour les membres

Chantier #85, regroupant les propositions 5, 10 et 11 de #78. Eris peut expliquer l'état d'une demande personnelle sans rôle administrateur. La route historique de diagnostic administrateur conserve son contrôle d'accès.

Le plan `diagnostic` accepte un titre ou un `requestId`. Par titre, la recherche utilise la liste personnelle Seerr et demande une précision s'il reste plusieurs demandes. Par numéro, le propriétaire de la demande Seerr est vérifié **avant** toute lecture technique. Le numéro ne permet donc pas d'accéder aux demandes d'un autre compte. L'identité Discord et l'autorisation Plex restent celles du parcours d'outils existant, avec revalidation après lecture.

L'implémentation réutilise le diagnostic Seerr → Radarr/Sonarr → Plex. Les états d'attente, de téléchargement, d'import, de droits, d'espace, d'échec et d'indexation sont traduits en français pédagogique. Un résultat partiel ou sans cause confirmée conserve cette incertitude. Une entrée historique d'import n'est pas une garantie de disponibilité complète ou de lecture Plex.

La progression vient de la file du client remontée par Arr (`size`, `sizeleft`, `status`). Les fichiers sont dédupliqués par identifiant de téléchargement avant agrégation ; les tailles incohérentes, absentes, les doublons contradictoires ou une file tronquée empêchent d'annoncer un pourcentage complet. Les compteurs d'épisodes manquants restent limités aux saisons demandées et renseignées. Les files Sonarr utilisant `seasonNumber` ou `seasonNumbers` sont prises en compte sans inclure une autre saison.

Les alertes de recherche des indexeurs sont utilisées seulement lorsqu'aucune cause plus précise n'est confirmée, avec une mise en garde sur le lien causal. Les erreurs libres ne sont jamais restituées. Seuls des motifs reconnus deviennent des explications prédéfinies. Une alerte d'indexeur ne prouve pas que cet indexeur cause le blocage du média demandé.

Les noms de trackers, empreintes de téléchargement, chemins, journaux, versions techniques et clés ne sont pas transmis au modèle ni présentés dans cette synthèse. Il n'y a pas de nouvelle connexion directe à qBittorrent, torrent-watcher ou Prowlarr : les données métier déjà remontées par Arr sont utilisées. Si elles ne suffisent pas, Eris le dit. Aucune pause/reprise, suppression, recherche forcée, import ou opération de maintenance n'est ajoutée. TrueNAS et proxy/DNS restent exclus.

Validation : tests sans rôle administrateur, propriétaire différent ou absent refusé avant lecture Arr, demandes ambiguës, états reconnus, progression complète/partielle/invalide, saisons et doublons Sonarr, champs sensibles filtrés et maintien des tests de diagnostic administrateur. Tests ciblés et `go vet ./...` passent.

Le 27 octobre 2026, les services configurés sur le NAS ont été vérifiés en lecture : Radarr **7.0.0.10815** expose les champs de progression attendus dans sa file ; Sonarr **5.0.0.2800** répond sur l'API compatible v3, mais sa file était vide et ne permettait pas une recette de progression réelle. Aucun texte de journal, titre de téléchargement ou détail d'indexeur n'a été conservé dans ce contrôle. La recette Discord reste à faire après déploiement.

Contrats : [file Radarr](https://github.com/Radarr/Radarr/blob/develop/src/Radarr.Api.V3/Queue/QueueResource.cs), [file Sonarr v3](https://github.com/Sonarr/Sonarr/blob/develop/src/Sonarr.Api.V3/Queue/QueueResource.cs), [file Sonarr v5](https://github.com/Sonarr/Sonarr/blob/v5-develop/src/Sonarr.Api.V5/Queue/QueueResource.cs), [vérification des indexeurs](https://github.com/Sonarr/Sonarr/blob/develop/src/NzbDrone.Core/HealthCheck/Checks/IndexerSearchCheck.cs).
