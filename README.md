# Le Concierge

Bot Discord et API Go commune pour un écosystème multimédia auto-hébergé.

**Statut : première version exécutable en validation.** Les fondations et premiers parcours sont implémentés et testés localement. La configuration et la validation sur les services réels restent nécessaires ; aucune installation NAS n’a été effectuée.

- [Roadmap et tickets](docs/roadmap.md)
- [Architecture commune](docs/architecture.md)
- [Contribuer](CONTRIBUTING.md)
- [Projet complémentaire : le-hub](https://github.com/hloiseau/le-hub)

Le Hub utilise une connexion Plex obligatoire avec vérification de l’accès au serveur. Discord se lie ensuite, de façon facultative, pour utiliser le bot.

## Démarrer

**TrueNAS :** [guide rapide et YAML prêts à adapter](docs/truenas-quickstart.md) · [mini tutoriel Discord](docs/discord-quickstart.md).

Voir le [guide de déploiement](docs/deployment.md) pour Go, Discord, Plex, Seerr, Docker, TrueNAS et Caddy. `make test`, `make build`, puis `./bin/api` après chargement de `.env` permettent de tester le backend. Le bot est un second processus : `./bin/bot`.

Le [contrat HTTP 0.7.0](api/openapi.json) décrit l’API du portail. Les réponses réelles des handlers Go sont vérifiées contre ce contrat dans les tests, y compris les erreurs, champs requis, types, statuts et dates UTC. Le Hub utilise une copie synchronisée et des types TypeScript générés. Les [téléchargements personnels Downloadarr](docs/downloadarr.md) utilisent une connexion Plex distincte et une révision de service explicitement vérifiée ; cette intégration reste désactivée par défaut.

Les nouveautés Plex sont paginées dans le Hub et `/nouveautes`. `/mes-demandes` possède sa navigation Discord. Les [suivis communs](docs/subscriptions.md) se gèrent dans le Hub et avec `/suivre` et `/mes-suivis` ; leur disponibilité est vérifiée avec les droits Plex personnels. Les commandes privées [`/surprise` et `/recap`](docs/discord-discovery.md) complètent la découverte.

Les [rappels avant retrait](docs/removal-reminders.md) proposent une boîte Hub et des [DM Discord facultatifs](docs/discord-reminders.md), avec consentement distinct, file durable et vérification des accès juste avant envoi. Ils couvrent lectures commencées, demandes, suivis et, sur option, contenus marqués non vus par Plex. « Demander un délai » ouvre une file administrateur sans modifier Maintainerr. La mise à jour migre SQLite vers le schéma **v5** : sauvegarder la base avant le premier démarrage. Aucun DM ni déploiement réel n’a été exécuté pendant le développement.

Le [catalogue de livres](docs/books-contracts.md) et les [collections](docs/book-collections.md) utilisent les identifiants OPDS personnels Grimmory. Les demandes ouvrent la recherche Shelfmark et restent confirmées dans ce service. Le [monitoring optionnel](docs/monitoring.md) fournit l’activité Plex agrégée et les historiques d’un collecteur isolé en lecture seule ; les détails nominatifs et d’hôte sont réservés aux administrateurs. Les [diagnostics Radarr/Sonarr](docs/diagnostics.md) corrèlent une demande Seerr à son instance de téléchargement, sans exposer les chemins privés. Les [pages communautaires](docs/community-publication.md) utilisent une sélection explicitement publiée.

## Périmètre

- V1 : `/mes-demandes`, `/nouveautes`, `/aide`.
- Rappels : boîte personnelle Hub, DM facultatifs, ciblage des demandes/suivis et demandes de délai.
- V2 : `/suivre`, `/mes-suivis`, `/surprise`, `/recap` et alertes proactives de disponibilité.
- V3 : `/livres`, `/demander-livre` ; suivi d’import des lectures restant dépendant des API utilisateur installées.
- V4 : `/diagnostic`, réservé aux administrateurs.
- Socle commun : identité, API, données et intégrations nécessaires au Hub.

Le bot complète Seerr et ses notifications déjà opérationnelles. Deux exécutables Go sont disponibles : API et bot. Le backend reste l’unique propriétaire de la persistance.

## Suivi

Les issues décrivent les objectifs, critères d’acceptation, phases, priorités et dépendances. Une priorité P0 est relative à sa phase. Les états des issues font référence ; une case non cochée ne signifie pas qu’une application est disponible.

Les tableaux GitHub Projects et milestones ne sont pas configurés à ce stade. La [roadmap liée](docs/roadmap.md) permet de naviguer immédiatement dans le backlog.

## Licence

[MIT](LICENSE), Copyright 2026 Hugo Loiseau.
