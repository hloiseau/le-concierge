# Le Concierge

Bot Discord et API Go commune pour un écosystème multimédia auto-hébergé.

**Statut : application en développement, déjà installée par l’administrateur.** Le parcours Downloadarr initial a été confirmé fonctionnel. Les corrections et intégrations décrites ci-dessous disposent de tests automatisés ; leur présence dans le code ne confirme pas leur configuration ni leur validation sur le NAS. Voir le détail dans la [roadmap](docs/roadmap.md).

- [Roadmap et tickets](docs/roadmap.md)
- [Architecture commune](docs/architecture.md)
- [Contribuer](CONTRIBUTING.md)
- [Projet complémentaire : le-hub](https://github.com/hloiseau/le-hub)

Le Hub utilise une connexion Plex obligatoire avec vérification de l’accès au serveur. Discord se lie ensuite, de façon facultative, pour utiliser le bot.

## Démarrer

**TrueNAS :** [guide rapide et YAML prêts à adapter](docs/truenas-quickstart.md) · [mini tutoriel Discord](docs/discord-quickstart.md).

Voir le [guide de déploiement](docs/deployment.md) pour Go, Discord, Plex, Seerr, Docker, TrueNAS et Caddy. `make test`, `make build`, puis `./bin/api` après chargement de `.env` permettent de tester le backend. Le bot est un second processus : `./bin/bot`. Si son token est configuré, il met à jour automatiquement les commandes au démarrage ; `--register` reste disponible pour une opération ponctuelle.

Le [contrat HTTP 0.9.0](api/openapi.json) décrit l’API du portail. Les réponses réelles des handlers Go sont vérifiées contre ce contrat dans les tests, y compris les erreurs, champs requis, types, statuts et dates UTC. Le Hub utilise une copie synchronisée et des types TypeScript générés. Les [téléchargements personnels Downloadarr](docs/downloadarr.md) proposent une recherche Plex et réutilisent la connexion du Hub pour obtenir un accès personnel Downloadarr, avec une révision de service explicitement vérifiée ; cette intégration reste désactivée par défaut.

Les nouveautés Plex sont paginées dans le Hub et `/nouveautes`. [Mes demandes](docs/subscriptions.md) regroupe les demandes personnelles Seerr et leurs réglages de notifications ; `/mes-demandes` possède sa navigation Discord. Les anciens suivis ajoutés séparément restent modifiables dans cette même page. Les demandes Seerr et nouveautés Plex disposent d’affiches ; les images Plex exigent une session et un accès personnel encore valides. [`/nouveautes`, `/surprise` et `/recap`](docs/discord-discovery.md) répondent en privé par défaut ; l’option `public:true` partage les titres du catalogue dans le salon sans publier de liens Plex personnels.

Les [rappels de salon Discord](docs/discord-channel-reminders.md) mentionnent les demandeurs Seerr avant un retrait estimé par Maintainerr, sans exposer leur visionnage. Les [rappels personnels](docs/removal-reminders.md) conservent une boîte Hub et des [DM facultatifs](docs/discord-reminders.md), avec consentement distinct, file durable et vérification des accès juste avant envoi. Ils couvrent lectures commencées, demandes, suivis et, sur option, contenus marqués non vus par Plex. « Demander un délai » ouvre une file administrateur sans modifier Maintainerr. La mise à jour migre SQLite vers le schéma **v6**, en conservant les préférences. Aucun rappel réel de salon ou DM n’a été envoyé par les tests.

Le [catalogue de livres](docs/books-contracts.md) et les [collections](docs/book-collections.md) utilisent les identifiants OPDS personnels Grimmory. Les demandes ouvrent la recherche Shelfmark et restent confirmées dans ce service. Le [monitoring](docs/monitoring.md) active l’activité Plex agrégée dès que Tautulli est configuré, sauf désactivation explicite. Les mesures matérielles nécessitent un collecteur isolé en lecture seule ; son absence ne masque plus l’activité Plex. Les détails nominatifs et d’hôte sont réservés aux administrateurs. Les [diagnostics Radarr/Sonarr](docs/diagnostics.md) corrèlent une demande Seerr à son instance de téléchargement, sans exposer les chemins privés. Les [pages communautaires](docs/community-publication.md) utilisent une sélection explicitement publiée.

## Périmètre

- V1 : `/mes-demandes`, `/nouveautes`, `/aide`.
- Rappels : boîte personnelle Hub, DM facultatifs, ciblage des demandes/suivis et demandes de délai.
- Découverte : `/surprise`, `/recap`, partage des résultats sur choix explicite et alertes de disponibilité. [`/a-voir`](docs/discord-discovery.md) retrouve en privé les départs estimés qui vous concernent.
- V3 : `/livres`, `/demander-livre` ; suivi d’import des lectures restant dépendant des API utilisateur installées.
- V4 : `/diagnostic`, réservé aux administrateurs.
- Socle commun : identité, API, données et intégrations nécessaires au Hub.

Le bot complète Seerr et ses notifications déjà opérationnelles. Deux exécutables Go sont disponibles : API et bot. Le backend reste l’unique propriétaire de la persistance.

## Suivi

Les issues décrivent les objectifs, critères d’acceptation, phases, priorités et dépendances. Une priorité P0 est relative à sa phase. Les états des issues font référence ; une case non cochée ne signifie pas qu’une application est disponible.

Les tableaux GitHub Projects et milestones ne sont pas configurés à ce stade. La [roadmap liée](docs/roadmap.md) permet de naviguer immédiatement dans le backlog.

## Licence

[MIT](LICENSE), Copyright 2026 Hugo Loiseau.
