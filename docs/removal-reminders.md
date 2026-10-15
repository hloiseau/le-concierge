# Rappels avant retrait — priorité de phase 2

Suivi : [sources #25](https://github.com/hloiseau/le-concierge/issues/25), [politique et livraison #26](https://github.com/hloiseau/le-concierge/issues/26), [interface Hub #16](https://github.com/hloiseau/le-hub/issues/16).

Le package `internal/reminders` contient un moteur de décision pur, testé. Il ne collecte aucune donnée et ne délivre aucun message. Les collecteurs sont dans `internal/upstream`. Le service `internal/departures` les raccorde à une première liste personnelle des départs, limitée aux lectures enregistrées. Les demandes jamais commencées, suivis explicites, inventaires complets d’épisodes, préférences de rappel, outbox durable, planification et demandes de délai restent à développer. Aucun rappel automatique n’est envoyé.

## Liste personnelle des départs

`GET /api/v1/departures` utilise uniquement le compte Plex de la session, sans paramètre d’identité. La capacité `departures` indique que les sources sont configurées ; leur disponibilité est vérifiée au chargement. Le Hub affiche les départs estimés dans les 30 prochains jours sur l’accueil et dans « Avant leur départ ».

La portée `recorded_history` croise l’historique Tautulli de cet identifiant Plex avec les appartenances destructives Maintainerr. Chaque contenu est relu avec le jeton Plex individuel, après vérification du serveur. Les bibliothèques, types, règles actives et exclusions globales ou de groupe doivent correspondre. Une collection sans groupe vérifié, une évaluation de règle en échec ou une échéance inconnue/passée est écartée. Une exclusion de série protège ses descendants ; celle d’un épisode ne protège pas toute sa saison.

Un film ou épisode peut être commencé, terminé ou incertain. Le rapprochement historique exige la même clé et le même GUID ; une édition ambiguë ne reçoit pas l’état d’une autre. L’état Plex individuel courant peut confirmer une complétion. Une saison ou série reste incertaine tant que son inventaire entier n’est pas vérifié. L’absence d’historique ne signifie jamais « non commencé ». Les contenus terminés sont masqués par défaut, avec une case pour les inclure dans la vue complète.

Seul le relevé Maintainerr est partagé en mémoire pendant moins de 30 secondes. L’historique et les droits Plex sont relus à chaque requête ; les listes personnelles ne sont pas conservées. La collecte est bornée à 18 secondes et 100 métadonnées distinctes. Les relevés expirent au plus tard une minute après leur collecte. Une erreur invalide toute la réponse ; le Hub retire la liste au rechargement, à l’expiration ou à la déconnexion. La session est revérifiée après les lectures distantes. Les dates restent estimées : aucun bouton ne promet la conservation du contenu.

## Politique des futurs rappels

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

La commande n’ouvre pas SQLite et n’envoie aucune commande de mutation. Sa sortie JSON contient versions, correspondance avec `PLEX_SERVER_ID`, nombres de collections/appartenances destructives, groupes de règles vérifiés, échéances inconnues et instant du relevé. Avec `--plex-user`, elle ajoute seulement le nombre de lectures enregistrées. Aucun titre, identifiant personnel, historique détaillé, adresse ou secret n’est imprimé. `remindersEnabled` reste `false`. L’initialisation de la connexion Plex par Maintainerr peut mettre à jour ses propres paramètres de découverte ; voir l’audit des sources.

Limites du relevé : 2 Mio par réponse, 500 collections, 10 000 appartenances destructives et 5 000 lignes d’historique par utilisateur ; pages Tautulli de 100 lignes et délai total de 15 secondes par collecte. Un dépassement, un compte discordant, un total changeant ou une page manquante invalide le résultat entier. Les identités de serveur sont comparées avant de lire les contenus ; le diagnostic Tautulli vérifie sa configuration de serveur, pas la disponibilité de Plex.

Les collections inactives et actions non destructives sont écartées. Les délais nuls, zéro, négatifs ou hors borne ne deviennent pas des dates certaines. Une échéance positive est calculée depuis l’instant source par blocs de 24 heures, comme le worker audité ; elle reste estimée et peut différer de l’affichage calendaire amont lors d’un changement d’heure. L’historique Tautulli conserve ses identifiants Plex et son indicateur de complétion configuré, sans rapprochement par pseudo ni seuil arbitraire. Une absence de ligne reste inconnue, même si l’enregistrement de l’historique est activé aujourd’hui.

Le diagnostic a été exécuté sur les instances réelles : versions attendues, collections Maintainerr complètes, règles et exclusions lisibles, identité serveur identique entre les sources et la configuration locale, historique Tautulli du compte administrateur parcouru jusqu’au total annoncé. L’identité Plex directe et la lecture des utilisateurs Seerr ont aussi été vérifiées avec la configuration locale. Ces vérifications ne constituent pas une recette du déploiement NAS ni des futurs rappels. La liste personnelle possède des tests d’isolation et de révocation ; sa recette réelle nécessite une connexion Plex utilisateur.
