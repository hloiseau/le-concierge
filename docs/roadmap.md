# Roadmap — Le Concierge

24 issues ouvertes à l’initialisation. Le statut actuel est celui de chaque issue GitHub ; cette page organise le périmètre, sans échéances promises.

Les dépendances sont des liens vers les issues des deux dépôts. Les références Cxx/Hxx permettent de relier le découpage initial aux numéros GitHub.

## Phase 1 — Fondations et bot V1

| Ticket | Version cible | Priorité | Dépendances |
| --- | --- | --- | --- |
| [#1 — Initialiser le backend Go et le bot avec deux exécutables](https://github.com/hloiseau/le-concierge/issues/1) | Socle | P0 | — |
| [#2 — Définir le contrat HTTP versionné et les clients](https://github.com/hloiseau/le-concierge/issues/2) | Socle | P0 | [C01](https://github.com/hloiseau/le-concierge/issues/1) |
| [#3 — Persister utilisateurs, identités et préférences avec migrations](https://github.com/hloiseau/le-concierge/issues/3) | Socle | P0 | [C01](https://github.com/hloiseau/le-concierge/issues/1) |
| [#4 — Implémenter connexion Discord, liaison Plex et autorisation](https://github.com/hloiseau/le-concierge/issues/4) | Socle | P0 | [C02](https://github.com/hloiseau/le-concierge/issues/2), [C03](https://github.com/hloiseau/le-concierge/issues/3) |
| [#5 — Intégrer Seerr et exposer les demandes personnelles](https://github.com/hloiseau/le-concierge/issues/5) | V1 | P0 | [C02](https://github.com/hloiseau/le-concierge/issues/2), [C04](https://github.com/hloiseau/le-concierge/issues/4) |
| [#6 — Intégrer Plex/Tautulli pour les derniers ajouts](https://github.com/hloiseau/le-concierge/issues/6) | V1 | P1 | [C02](https://github.com/hloiseau/le-concierge/issues/2), [C04](https://github.com/hloiseau/le-concierge/issues/4) |
| [#7 — Installer le bot Discord et enregistrer ses commandes](https://github.com/hloiseau/le-concierge/issues/7) | V1 | P0 | [C01](https://github.com/hloiseau/le-concierge/issues/1), [C02](https://github.com/hloiseau/le-concierge/issues/2), [C04](https://github.com/hloiseau/le-concierge/issues/4) |
| [#8 — Ajouter /mes-demandes et /aide](https://github.com/hloiseau/le-concierge/issues/8) | V1 | P0 | [C05](https://github.com/hloiseau/le-concierge/issues/5), [C07](https://github.com/hloiseau/le-concierge/issues/7) |
| [#9 — Ajouter /nouveautes](https://github.com/hloiseau/le-concierge/issues/9) | V1 | P1 | [C06](https://github.com/hloiseau/le-concierge/issues/6), [C07](https://github.com/hloiseau/le-concierge/issues/7) |
| [#10 — Mettre en place CI, images GHCR et déploiement TrueNAS/Caddy](https://github.com/hloiseau/le-concierge/issues/10) | Socle | P0 | [C01](https://github.com/hloiseau/le-concierge/issues/1), [C03](https://github.com/hloiseau/le-concierge/issues/3) |

## Phase 3 — Livres et mangas

| Ticket | Version cible | Priorité | Dépendances |
| --- | --- | --- | --- |
| [#11 — Auditer les API Grimmory et Shelfmark installées](https://github.com/hloiseau/le-concierge/issues/11) | V3 | P0 | — |
| [#12 — Connecter le catalogue Grimmory et /livres](https://github.com/hloiseau/le-concierge/issues/12) | V3 | P1 | [C11](https://github.com/hloiseau/le-concierge/issues/11), [C04](https://github.com/hloiseau/le-concierge/issues/4), [C07](https://github.com/hloiseau/le-concierge/issues/7) |
| [#13 — Connecter Shelfmark et /demander-livre](https://github.com/hloiseau/le-concierge/issues/13) | V3 | P1 | [C11](https://github.com/hloiseau/le-concierge/issues/11), [C04](https://github.com/hloiseau/le-concierge/issues/4), [C07](https://github.com/hloiseau/le-concierge/issues/7) |
| [#14 — Suivre le cycle demande → téléchargement → import des livres](https://github.com/hloiseau/le-concierge/issues/14) | V3 | P1 | [C12](https://github.com/hloiseau/le-concierge/issues/12), [C13](https://github.com/hloiseau/le-concierge/issues/13) |
| [#15 — Modéliser collections, tomes manquants et nouvelles sorties](https://github.com/hloiseau/le-concierge/issues/15) | V3 | P2 | [C12](https://github.com/hloiseau/le-concierge/issues/12), [C14](https://github.com/hloiseau/le-concierge/issues/14) |

## Phase 4 — Monitoring

| Ticket | Version cible | Priorité | Dépendances |
| --- | --- | --- | --- |
| [#16 — Valider et collecter les métriques TrueNAS et Intel Arc](https://github.com/hloiseau/le-concierge/issues/16) | Monitoring | P0 | — |
| [#17 — Stocker les historiques et exposer une API de métriques](https://github.com/hloiseau/le-concierge/issues/17) | Monitoring | P1 | [C16](https://github.com/hloiseau/le-concierge/issues/16), [C04](https://github.com/hloiseau/le-concierge/issues/4) |
| [#18 — Exposer activité Plex et sessions de transcodage](https://github.com/hloiseau/le-concierge/issues/18) | Monitoring | P1 | [C06](https://github.com/hloiseau/le-concierge/issues/6), [C17](https://github.com/hloiseau/le-concierge/issues/17) |

## Phase 5 — Fonctionnalités avancées

| Ticket | Version cible | Priorité | Dépendances |
| --- | --- | --- | --- |
| [#19 — Ajouter /suivre, /mes-suivis et les abonnements communs](https://github.com/hloiseau/le-concierge/issues/19) | V2 | P1 | [C03](https://github.com/hloiseau/le-concierge/issues/3), [C04](https://github.com/hloiseau/le-concierge/issues/4), [C05](https://github.com/hloiseau/le-concierge/issues/5), [C07](https://github.com/hloiseau/le-concierge/issues/7) |
| [#20 — Notifier les abonnés et demandeurs sans doubler Seerr](https://github.com/hloiseau/le-concierge/issues/20) | V2 / V3 | P1 | [C19](https://github.com/hloiseau/le-concierge/issues/19), [C06](https://github.com/hloiseau/le-concierge/issues/6) |
| [#21 — Ajouter /surprise et /recap](https://github.com/hloiseau/le-concierge/issues/21) | V2 | P2 | [C06](https://github.com/hloiseau/le-concierge/issues/6), [C07](https://github.com/hloiseau/le-concierge/issues/7) |
| [#22 — Ajouter diagnostics Radarr/Sonarr et /diagnostic admin](https://github.com/hloiseau/le-concierge/issues/22) | V4 | P1 | [C05](https://github.com/hloiseau/le-concierge/issues/5), [C06](https://github.com/hloiseau/le-concierge/issues/6), [C07](https://github.com/hloiseau/le-concierge/issues/7) |
| [#23 — Synchroniser les forums Discord et la roadmap publique](https://github.com/hloiseau/le-concierge/issues/23) | Communauté | P2 | [C04](https://github.com/hloiseau/le-concierge/issues/4), [C07](https://github.com/hloiseau/le-concierge/issues/7) |
| [#24 — Intégrer le fork Downloadarr et ses conversions](https://github.com/hloiseau/le-concierge/issues/24) | Téléchargements | P1 | [C04](https://github.com/hloiseau/le-concierge/issues/4) |

