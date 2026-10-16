# Roadmap — Le Concierge

Le découpage initial comprend 26 issues de développement, dont 2 priorités ajoutées pour les retraits. Des correctifs et compléments s’y ajoutent ensuite. Le statut actuel est celui de chaque issue GitHub ; cette page organise le périmètre, sans échéances promises.

Les dépendances sont des liens vers les issues des deux dépôts. Les références Cxx/Hxx permettent de relier le découpage initial aux numéros GitHub.

## Avancement local au 16 octobre 2026

Les éléments ci-dessous sont implémentés dans l’arbre de travail et vérifiés localement avec des fournisseurs simulés. Ils ne constituent pas une annonce de publication d’image, de CI distante réussie ou de recette sur le NAS. Les observations antérieures des API sources ne remplacent pas la validation réelle des nouveaux parcours.

| Tickets concernés | Résultat local | Limites restantes |
| --- | --- | --- |
| [#2 — Contrat commun](https://github.com/hloiseau/le-concierge/issues/2) | OpenAPI 0.5.0, copie Hub synchronisée, types TypeScript régénérés ; tests Go des réponses réelles, routes, erreurs, types, champs requis/supplémentaires et dates UTC. Les tests prouvent aussi le rejet de dérives volontaires. | Le job CI existant exécute cette suite ; son prochain résultat distant reste à vérifier. |
| [#6 — Nouveautés](https://github.com/hloiseau/le-concierge/issues/6), [#9 — Commande Discord](https://github.com/hloiseau/le-concierge/issues/9) | Curseur Plex, regroupement par série dans chaque page et identifiants stables ; pages de 12 entrées brutes pour le Hub, 6 pour le bot ; cache utilisateur de 30 s plafonné à 128 entrées, avec nouvelle vérification des accès. `/nouveautes` présente dates, liens, Début et Suivant. | Sélection bornée à l’offset 10 000, regroupement limité à la page, aucune affiche ; essais nécessaires avec les bibliothèques réellement partagées. Une panne ne sert pas de cache périmé. |
| [#8 — Demandes Discord](https://github.com/hloiseau/le-concierge/issues/8), [PR #37 — Pagination](https://github.com/hloiseau/le-concierge/pull/37) | Code de pagination `/mes-demandes` repris localement par cherry-pick, sans fusion distante de la PR : dix demandes, Précédent/Suivant, réponse éphémère et contrôle de l’identité à chaque clic. | Recette Discord réelle à refaire avec API et bot de même version. |
| [#3 — Persistance](https://github.com/hloiseau/le-concierge/issues/3), [#20 — Livraison](https://github.com/hloiseau/le-concierge/issues/20), [#26 — Rappels](https://github.com/hloiseau/le-concierge/issues/26) | Migration SQLite v4, préférences, file durable et boîte Hub ; regroupement quotidien, paliers J-7/J-2 configurables, heures calmes Paris, annulation et rétention. Sources et accès relus sans cache avant livraison et affichage. | Livraison limitée aux films/épisodes commencés présents dans l’historique. DM facultatifs avec consentement distinct et demandes de délai administrateur implémentés. Demandes jamais commencées, abonnements et saisons complètes restent hors portée des rappels. #20 et #26 restent partiellement réalisés. |

| [#19 — Suivis](https://github.com/hloiseau/le-concierge/issues/19) | API propriétaire, identités TMDB film/série/saison, `/suivre`, `/mes-suivis`, page Hub, suppression et déduplication de disponibilité. | Disponibilité vérifiée à la demande dans le Hub ; les alertes Discord de disponibilité restent à raccorder. Une série/saison devient disponible au premier épisode accessible. |
| [#21 — Découverte](https://github.com/hloiseau/le-concierge/issues/21) | `/surprise` filtré dans le catalogue personnel complet, `/recap` sur sept jours civils terminés à Paris ; réponses privées et regroupées. | Lecture bornée à 5 000 entrées et six secondes ; aucune publication programmée. Recette Plex/Discord réelle à faire. |

Les tests Go locaux et `go vet` ont été exécutés, notamment sur le contrat et la normalisation des dates Seerr. Une date source absente/invalide est explicitement inconnue ; les dates valides sont converties en UTC. Les validations du Hub sont suivies dans son propre dépôt.

Avant mise à jour, sauvegarder la base avec l’ancienne version : le passage à v4 conserve les utilisateurs déjà inscrits aux notifications, qui recevront les réglages de rappel par défaut. Un retour à un ancien binaire exige aussi la restauration de son ancienne base. Le traitement des rappels parcourt les autorisations Plex actives chaque minute, séquentiellement ; expiration à 12 h maximum, déconnexion ou redémarrage suspendent ce traitement jusqu’à une nouvelle connexion. La purge à 90 jours après expiration reste active même lorsque les sources sont désactivées. Voir les [rappels et leurs limites](removal-reminders.md) et le [déploiement](deployment.md).

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


## Priorité ajoutée — après le socle, en phase 2

- [#25 — Sources Maintainerr/Tautulli](https://github.com/hloiseau/le-concierge/issues/25) : versions, échéances et historique individuel.
- [#26 — Rappels personnalisés avant retrait](https://github.com/hloiseau/le-concierge/issues/26) : moteur, connecteurs de lectures enregistrées, préférences et boîte Hub durable implémentés ; élargissement des candidats, transport Discord et demandes de délai à réaliser.
- [Hub #16](https://github.com/hloiseau/le-hub/issues/16) : vue personnelle et réglages.

La file durable prévue en #20 sert désormais cette première boîte de rappels. Les abonnements communautaires complets et les transports externes restent à réaliser ; cette réutilisation ne clôt pas #20.
