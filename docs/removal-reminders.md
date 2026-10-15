# Rappels avant retrait — priorité de phase 2

Suivi : [sources #25](https://github.com/hloiseau/le-concierge/issues/25), [politique et livraison #26](https://github.com/hloiseau/le-concierge/issues/26), [interface Hub #16](https://github.com/hloiseau/le-hub/issues/16).

Le package `internal/reminders` contient un moteur de décision pur, testé. Il ne collecte aucune donnée, ne délivre aucun message et ne modifie jamais Maintainerr. Les connecteurs, l'outbox durable, la planification et les demandes de délai restent à développer.

| Situation vérifiée | Décision |
| --- | --- |
| Film demandé/suivi, jamais commencé, retrait imminent | Candidat à un rappel privé |
| Film commencé mais non terminé | Candidat, même sans abonnement explicite |
| Saison/série suivie avec épisodes confirmés restant à voir | Candidat pour le périmètre concerné |
| Contenu terminé | Pas de rappel |
| Historique absent/incomplet ou état non reconnu | Pas d'affirmation « non vu », pas d'envoi automatique |
| Règle inactive ou action qui conserve les fichiers | Pas d'alerte de suppression |
| Source périmée, accès révoqué ou échéance inconnue/passée | Pas d'envoi |
| Retrait pendant les heures calmes | Pas d'envoi forcé pendant la plage silencieuse |

Les paliers de référence sont J-7 et J-2. Si un candidat est découvert tardivement, seul le palier courant le plus proche est retenu. La clé de déduplication inclut l'utilisateur, le serveur, le contenu, l'action, la date et le palier. Une échéance modifiée invalide l'ancienne clé : le futur worker devra annuler les tâches caduques et revalider toutes les conditions juste avant l'envoi. Une simple clé ne constitue pas une outbox persistante.

Les heures calmes utilisent un fuseau IANA, Europe/Paris par défaut, et sont testées lors du changement d'heure. La portée « tous les contenus non vus » exige une préférence dédiée ; le défaut cible les demandes, suivis et lectures commencées. Les futures limites quotidiennes et regroupements doivent être appliqués par le planificateur.

Avant le connecteur, valider les API de la version installée de Maintainerr, la complétude du résultat (pas l'aperçu d'une collection), l'action de suppression effective et les dates. La date annoncée est une échéance estimée, pas une garantie sur l'heure de passage du planificateur. Tautulli doit être corrélé à l'identité Plex et son historique paginé ; absence de trace ne veut pas dire absence de visionnage.

Références : [Maintainerr collections](https://docs.maintainerr.info/api/collections/), [notifications Maintainerr](https://docs.maintainerr.info/notifications/), [API Tautulli](https://github.com/Tautulli/Tautulli/wiki/Tautulli-API-Reference).
