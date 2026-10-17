# Roadmap — Le Concierge

Le découpage initial comprend 26 issues de développement, dont 2 priorités ajoutées pour les retraits. Des correctifs et compléments s’y ajoutent ensuite. Le statut actuel est celui de chaque issue GitHub ; cette page organise le périmètre, sans échéances promises.

Les dépendances sont des liens vers les issues des deux dépôts. Les références Cxx/Hxx permettent de relier le découpage initial aux numéros GitHub.

## Avancement au 18 octobre 2026

Les changements de [Concierge #40](https://github.com/hloiseau/le-concierge/pull/40), [Hub #29](https://github.com/hloiseau/le-hub/pull/29) et [Downloadarr #1](https://github.com/hloiseau/LibraryDownloadarr/pull/1) sont fusionnés. Les images `edge` du Concierge/Hub et `quality-preview` de Downloadarr ont été publiées ; l’administrateur confirme leur installation et le fonctionnement du parcours Downloadarr. Les tests automatisés utilisent des fournisseurs simulés : cette confirmation ne valide pas les autres intégrations ni l’isolation avec deux comptes réels.

La simplification des demandes et le partage Discord sont fusionnés dans [Concierge #44](https://github.com/hloiseau/le-concierge/pull/44) et [Hub #32](https://github.com/hloiseau/le-hub/pull/32). Les images `edge` correspondantes sont publiées et déployées sur le NAS ; API, bot et Hub passent leurs sondes, y compris après l’activation des diagnostics Arr. Huit commandes ont été synchronisées dans la guilde. Le collecteur hôte tourne chaque minute ; CPU/RAM, température, charge, réseau, disques et capacité ZFS sont ingérés dans SQLite. L’activité Plex a aussi été vérifiée. Le raccordement GPU utilise l’API Toparr existante ; son client a été vérifié sur l’Arc A310 du NAS, sans ajout de privilèges. Ces constats d’exploitation ne remplacent pas la recette des parcours personnels avec deux comptes ni la réception consentie des messages Discord.

### Ce qui est utilisable et ce qui reste incomplet

| Parcours livré | Configuration nécessaire | Limite actuelle |
| --- | --- | --- |
| Nouveautés Plex et demandes personnelles | Plex et Seerr | La création de demandes reste dans Seerr. |
| Suivis et alertes de disponibilité | Plex, Seerr ; Discord facultatif | Les demandes Seerr sont suivies automatiquement ; les anciens suivis distincts restent modifiables. |
| Rappels avant retrait et demandes de délai | Maintainerr, Tautulli, Plex ; consentement aux rappels | Une réponse à une demande de délai ne modifie pas Maintainerr. |
| Catalogue, couvertures et collections de livres | Connexion OPDS Grimmory personnelle | Pas de détection fiable des tomes manquants ni de synchronisation Kobo intégrée. |
| Suivi des demandes de livres | Connexion personnelle Shelfmark en mode builtin | Création dans Shelfmark ; import Grimmory non vérifié. |
| Performances et historiques | Collecteur hôte, Tautulli et API Toparr facultative | Douze mesures hôte/Plex disponibles dans l’historique ; client GPU vérifié sur Toparr 2.0.1, moteurs absents conservés indisponibles. Affichage sous charge à recetter. |
| Communauté et roadmap | Sélection et exporteur à configurer | Discussions et votes restent dans Discord. |

Ces fonctions sont présentes dans le code publié ; leur présence ne prouve pas que les services correspondants sont configurés sur l’installation. Le détail et les critères encore ouverts suivent.

La version 0.9 reprend les retours d'utilisation : affiches Plex privées et TMDB pour les demandes, suivis Seerr automatiques, activation des DM sans étape cachée et rappels collectifs dans un salon Discord mentionnant les demandeurs. L'activité Tautulli active les performances par défaut lorsque le réglage explicite est absent ; une panne d'historique ne masque plus les données actuelles. Le bot synchronise son catalogue de commandes au démarrage si son token est fourni. Le salon est configuré ; la réception réelle et les permissions nécessaires à l’envoi restent à vérifier. Les lectures Seerr et Tautulli ont été vérifiées sans envoi Discord.

La version déployée regroupe les demandes Seerr et leurs notifications dans **Mes demandes**. L'ancien lien `/suivis` redirige vers cette page, les suivis manuels existants restent modifiables, et `/mes-demandes` devient l'unique commande proposée pour ce parcours. `/nouveautes`, `/surprise` et `/recap` proposent `public:true` pour partager leur résultat dans le salon ; les erreurs, les demandes et les informations personnelles restent privées. Les trois options publiques sont enregistrées dans Discord. Les tests utilisent des interactions signées et des services simulés, sans publication dans un vrai salon.

| Tickets | Implémentation | Vérification externe restante |
| --- | --- | --- |
| C02/C03, H01 | OpenAPI 0.9 synchronisé, types générés, migrations SQLite v6, contraintes et restauration testées ; lint, types, build et tests du Hub en CI. | Socle livré et déployé ; la recette de chaque intégration reste dans son ticket. |
| C04, H04/H06 | Sessions Plex privées, Discord facultatif, effacement des associations en mémoire à la déconnexion/révocation. | Recette à deux comptes sur le NAS. |
| C05–C09, H02/H03/H05 | Demandes Seerr personnelles, pagination Hub/bot, nouveautés Plex paginées et dates vérifiées. Les derniers livres de l’accueil utilisent le flux OPDS récent personnel. Huit commandes synchronisées sur la guilde. | Invocation personnelle des commandes et accès réels aux bibliothèques distinctes. |
| C10, H07 | CI Go/typecheck/Playwright/contrat/images et exemples TrueNAS/Caddy ; CLI de sauvegarde, collecteur Python testé en CI, sondes privées. Images publiées, déployées et trois services en bonne santé sur le NAS. | C10 terminé ; H07 conserve la configuration des budgets CPU/RAM. Les recettes fonctionnelles des services restent distinctes. |
| C25/C26, H16 | Maintainerr 3.30.1 et Tautulli v2.18.2 vérifiés en lecture seule, serveur Plex correspondant. Demandes/suivis et saisons ciblées, état Plex positif « non vu », lectures commencées, option tous non vus, boîte Hub et DM sur consentement. File durable, revalidation finale, plafond commun et demandes de délai. | Recette personnelle du visionnage, DM réel consenti, report/retrait et révocation. La relance tardive des demandes garde son consentement distinct. Voir [les rappels](removal-reminders.md). |
| C19/C20, H06 | Suivis films/séries/saisons, choix de canal, polling borné, disponibilité jouable vérifiée par Plex, déduplication des demandes personnelles Seerr, boîte et DM. | Réception réelle ; reprise seulement après reconnexion Plex puisque les jetons restent en mémoire. |
| C11–C13, H08/H09 | Contrats Grimmory v3.5.0 et Shelfmark v1.4.0 audités ; versions/révisions des images NAS confirmées et refus des lectures anonymes vérifiés. Catalogue/couvertures OPDS privés, recherche Shelfmark préremplie, `/livres` et `/demander-livre` implémentés. | Connexion personnelle, droits et réponses anonymisées de deux vrais comptes non validés. Voir [les contrats](books-contracts.md). |
| C14, H09 | Connexion Shelfmark personnelle, liste des demandes et étapes réelles de livraison, pagination et erreurs privées. | Shelfmark ne fournit pas d’ID d’import Grimmory dans le contrat audité ; `complete` ne signifie pas « lisible ». Le catalogue reste non vérifié. Voir [le suivi](book-request-tracking.md). |
| C15, H10 | Collections à partir des notices autorisées, éditions/langues/ISBN distincts, positions fractionnaires conservées, provenance/date de collecte, correction Grimmory et guide Kobo conditionnel. | Aucun référentiel éditorial complet validé : pas de tomes manquants ou de sorties inventés ; configuration Kobo réelle inconnue. Voir [les collections](book-collections.md). |
| C16–C18, H11/H12 | Collecteur exécuté chaque minute sur TrueNAS 25.10.7. Douze clés hôte/Plex disponibles dans deux échantillons SQLite successifs : CPU, température, charge, RAM, réseau, stockage et activité Plex. Cinq disques du pool, démarrage exclu ; montage API en lecture seule. Client GPU vérifié par les GET de Toparr 2.0.1 existant, sans nouvelle capacité matérielle. | Comparaison pendant un transcodage réel et coût sur une journée à recetter. Voir [le monitoring](monitoring.md). |
| C21/C22 | `/surprise` et `/recap` enregistrés avec partage public facultatif. Client de diagnostic Seerr→Radarr 6.4.4.10685/Sonarr 5.0.0.2608 vérifié sur un film et une série : 12 GET réussis, sans mutation. | Invocation Discord personnelle et incident d’import réel à recetter ; aucune instance 4K distincte configurée, aucune réparation automatique. Voir [les diagnostics](diagnostics.md). |
| C23, H13/H14 | Publication sélectionnée des fils Discord et d’issues, identifiants stables, statuts explicites, TTL, retrait des éléments non autorisés ; votes uniquement dans Discord. Exporteur périodique en lecture seule. | Configurer le manifeste et les droits publics réels ; jamais exporter les corps d’issues privées. Voir [la publication](community-publication.md). |
| C24, H15 | Contrat personnel fusionné dans la [PR amont #1](https://github.com/hloiseau/LibraryDownloadarr/pull/1), client Go, connexion personnelle Hub, choix et confirmation, jobs et transfert MP4/ZIP diffusé sans ticket navigateur. Fonctionnement confirmé par l’administrateur. La version 0.8 ajoute recherche Plex et connexion automatique avec le compte courant. | Recette à deux comptes et transfert long encore à vérifier ; nouveau sélecteur et connexion automatique à valider après mise à jour. Voir [le parcours](downloadarr.md). |

La migration v6 conserve les anciens réglages et livraisons et ajoute la déduplication du salon. Les messages privés restent sur consentement. Les sessions et autorisations restent en mémoire, au plus douze heures ; après redémarrage les traitements personnels reprennent à la prochaine connexion. Les rappels collectifs du salon fonctionnent sans session personnelle. Les historiques techniques sont conservés sept jours, les rappels quatre-vingt-dix jours après expiration. Voir [le déploiement](deployment.md) et [les rappels en salon](discord-channel-reminders.md).

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
