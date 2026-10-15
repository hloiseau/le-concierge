# Le Concierge

Bot Discord et API Go commune pour un écosystème multimédia auto-hébergé.

**Statut : première version exécutable en validation.** Les fondations et premiers parcours sont implémentés et testés localement. La configuration et la validation sur les services réels restent nécessaires ; aucune installation NAS n’a été effectuée.

- [Roadmap et tickets](docs/roadmap.md)
- [Architecture commune](docs/architecture.md)
- [Contribuer](CONTRIBUTING.md)
- [Projet complémentaire : le-hub](https://github.com/hloiseau/le-hub)

## Démarrer

Voir le [guide de déploiement](docs/deployment.md) pour Go, Discord, Plex, Seerr, Docker, TrueNAS et Caddy. `make test`, `make build`, puis `./bin/api` après chargement de `.env` permettent de tester le backend. Le bot est un second processus : `./bin/bot`.

Le [contrat HTTP](api/openapi.json) décrit l’API du portail. La [politique de rappels avant retrait](docs/removal-reminders.md) est codée et testée, mais ses connecteurs et envois ne sont pas encore actifs.

## Périmètre

- V1 : `/mes-demandes`, `/nouveautes`, `/aide`.
- V2 : `/suivre`, `/mes-suivis`, `/surprise`, `/recap` et notifications ciblées.
- V3 : `/livres`, `/demander-livre` et disponibilité des lectures selon les API.
- V4 : `/diagnostic`, réservé aux administrateurs.
- Socle commun : identité, API, données et intégrations nécessaires au Hub.

Le bot complète Seerr et ses notifications déjà opérationnelles. Deux exécutables Go sont prévus : API et bot. Le backend reste l’unique propriétaire de la persistance.

## Suivi

Les issues décrivent les objectifs, critères d’acceptation, phases, priorités et dépendances. Une priorité P0 est relative à sa phase. Les états des issues font référence ; une case non cochée ne signifie pas qu’une application est disponible.

Les tableaux GitHub Projects et milestones ne sont pas configurés à ce stade. La [roadmap liée](docs/roadmap.md) permet de naviguer immédiatement dans le backlog.

## Licence

[MIT](LICENSE), Copyright 2026 Hugo Loiseau.
