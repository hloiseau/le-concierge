# Roadmap — Le Concierge

## État vérifié au 22 octobre 2026

Le [bilan de livraison et de recette](status-2026-10-22.md) est la référence datée pour Concierge, le Hub et Eris : révisions installées, PR livrées, autorisations Plex, essais à deux comptes, mesures A310 et limites restantes. Il remplace l’état du 19 octobre, conservé dans l’historique Git.

API/bot installés : `20f96b6` ; Hub : `d86b929` ; runtime Eris : `5dd08ded`. Les quatre services sont sains. Eris est active, utilise Qwen3.5 9B Q4_K_M avec huit couches sur A310, répond aux mentions/réponses Discord et dispose d’outils Plex/Seerr contrôlés par Concierge. La refonte Hub, les notifications limitées aux contenus encore à voir et les autorisations Plex chiffrées persistantes sont déployées. La création Seerr est possible via Eris avec choix et confirmation, et ne se limite plus au site Seerr.

La recette réelle de deux comptes a vérifié les lectures personnelles, la séparation des droits, la déconnexion et la reprise des autorisations après redémarrage. Elle ne prouve pas la réception de DM ou une attente de douze heures. Les sources de livres et Downloadarr conservent leurs recettes distinctes.

### Suite

1. [Eris #62](https://github.com/hloiseau/le-concierge/issues/62) : qualité de conversation, faux refus de capacité, références et recette des confirmations Discord.
2. [Collecte #16](https://github.com/hloiseau/le-concierge/issues/16), [activité #18](https://github.com/hloiseau/le-concierge/issues/18) : A310/Plex sous charge prolongée et mesures réelles de latence. Les 11–12 s isolées ne garantissent pas la production, mesurée à 19–21 s sur deux réponses initiales.
3. [Rappels #26](https://github.com/hloiseau/le-concierge/issues/26) : réception consentie, reports et révocations en situation.
4. [Communauté #23](https://github.com/hloiseau/le-concierge/issues/23) : sélection explicite des issues de roadmap publique ; les forums autorisés sont déjà synchronisés.
5. Livres en dernier selon la priorité utilisateur : rapprochement Shelfmark–Grimmory, import vérifié, tomes manquants et Kobo. Le travail local non publié n’est pas déployé.

Les phases et références Cxx/Hxx ci-dessous organisent le découpage initial, sans échéances promises. Les critères restants sont dans les issues ; une implémentation seule ne justifie pas leur fermeture.

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
- [#26 — Rappels personnalisés avant retrait](https://github.com/hloiseau/le-concierge/issues/26) : moteur, candidats personnels demandés/suivis/non vus, préférences, boîte Hub durable, DM facultatifs et demandes de délai implémentés. La [relance des anciennes demandes](available-request-reminders.md) possède son consentement distinct ; la recette avec les comptes et versions installés reste à effectuer.
- [Hub #16](https://github.com/hloiseau/le-hub/issues/16) : vue personnelle et réglages.

Les rappels et les disponibilités des suivis disposent de files durables, de récapitulatifs Hub et de DM facultatifs avec plafond commun. Les critères de recette réelle de #20 restent ouverts ; cette implémentation testée avec des fournisseurs simulés ne suffit pas à clore le ticket.
