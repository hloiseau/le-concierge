# Rappels avant retrait — priorité de phase 2

Suivi : [sources #25](https://github.com/hloiseau/le-concierge/issues/25), [politique et livraison #26](https://github.com/hloiseau/le-concierge/issues/26), [interface Hub #16](https://github.com/hloiseau/le-hub/issues/16).

Le package `internal/reminders` contient un moteur de décision pur, testé. Il ne collecte aucune donnée, ne délivre aucun message et ne modifie jamais Maintainerr. Les premiers collecteurs en lecture seule sont dans `internal/upstream`, avec un diagnostic opérateur décrit ci-dessous. Leur raccordement aux droits de chaque utilisateur, exclusions et inventaires d’épisodes, l’outbox durable, la planification et les demandes de délai restent à développer. Aucune liste personnelle ni notification de retrait n’est active.

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

Avant activation des rappels, valider les API de la version installée de Maintainerr, la complétude du résultat (pas l'aperçu d'une collection), l'action de suppression effective et les dates. La date annoncée est une échéance estimée, pas une garantie sur l'heure de passage du planificateur. Tautulli doit être corrélé à l'identité Plex et son historique paginé ; absence de trace ne veut pas dire absence de visionnage.

Références : [Maintainerr collections](https://docs.maintainerr.info/api/collections/), [notifications Maintainerr](https://docs.maintainerr.info/notifications/), [API Tautulli](https://github.com/Tautulli/Tautulli/wiki/Tautulli-API-Reference).

## Diagnostic en lecture seule

Les contrats sont vérifiés pour **Maintainerr 3.30.1** et **Tautulli 2.18.2** ; une autre version est refusée jusqu’à vérification de son contrat. L’[audit versionné](removal-source-contracts.md) précise les endpoints, limites et divergences avec la documentation amont. Les observations des collecteurs restent distinctes des candidats à un rappel : leur succès ne prouve ni absence d’exclusion, ni autorisation de lecture d’un utilisateur, ni suppression certaine.

Dans l’environnement de l’API, conserver `PLEX_URL` et `PLEX_SERVER_ID`, puis renseigner les sources désirées :

```dotenv
MAINTAINERR_URL=http://maintainerr:6246
TAUTULLI_URL=http://tautulli:8181
TAUTULLI_API_KEY_FILE=/run/secrets/tautulli_api_key
```

Adapter les noms et ports au réseau interne. La clé Tautulli se trouve dans **Settings → Web Interface → API** ; la conserver dans un fichier monté en lecture seule, lisible par l’UID 65532, ou dans `TAUTULLI_API_KEY` dans l’administration TrueNAS. Ne pas définir la variable et sa variante `_FILE` ensemble. Aucun secret n’est nécessaire au protocole natif Maintainerr ; utiliser uniquement son adresse privée. Les YAML d’exemple laissent ces nouvelles valeurs vides. Ne jamais ajouter la clé dans le Hub, dans un lien ou dans une commande conservée dans l’historique du shell.

Après livraison d’une image contenant le diagnostic, dans le **Shell du conteneur API** :

```bash
/app/manage check-sources
# Facultatif : vérifier le parcours d’historique pour un identifiant Plex connu.
/app/manage check-sources --plex-user IDENTIFIANT_PLEX
```

La commande n’ouvre pas SQLite et ne modifie aucun service. Sa sortie JSON contient versions, correspondance avec `PLEX_SERVER_ID`, nombres de collections/appartenances destructives, échéances inconnues et instant du relevé. Avec `--plex-user`, elle ajoute seulement le nombre de lectures enregistrées. Aucun titre, identifiant personnel, historique détaillé, adresse ou secret n’est imprimé. `remindersEnabled` reste `false`.

Limites du relevé : 2 Mio par réponse, 500 collections, 10 000 appartenances destructives et 5 000 lignes d’historique par utilisateur ; pages Tautulli de 100 lignes et délai total de 15 secondes par collecte. Un dépassement, un compte discordant, un total changeant ou une page manquante invalide le résultat entier. Les identités de serveur sont comparées avant de lire les contenus ; le diagnostic Tautulli vérifie sa configuration de serveur, pas la disponibilité de Plex.

Les collections inactives et actions non destructives sont écartées. Les délais nuls, zéro, négatifs ou hors borne ne deviennent pas des dates certaines. Une échéance positive est calculée depuis l’instant source par blocs de 24 heures, comme le worker audité ; elle reste estimée et peut différer de l’affichage calendaire amont lors d’un changement d’heure. L’historique Tautulli conserve ses identifiants Plex et son indicateur de complétion configuré, sans rapprochement par pseudo ni seuil arbitraire. Une absence de ligne reste inconnue, même si l’enregistrement de l’historique est activé aujourd’hui.

Le diagnostic a été exécuté avec ces collecteurs sur les instances réelles : versions attendues, collections Maintainerr complètes, identité serveur identique entre les deux sources et historique Tautulli du compte administrateur parcouru jusqu’au total annoncé. Le rapprochement avec `PLEX_SERVER_ID` de l’API déployée et avec les comptes Discord reste à vérifier. Les relevés sont ponctuels, sans cache partagé ni conservation de données nominatives. Avant les rappels, il reste notamment les exclusions et hiérarchies, l’accès Plex individuel, les éditions multiples et la revalidation avant chaque envoi.
