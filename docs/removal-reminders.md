# Rappels avant retrait — priorité de phase 2

Suivi : [sources #25](https://github.com/hloiseau/le-concierge/issues/25), [politique et livraison #26](https://github.com/hloiseau/le-concierge/issues/26), [interface Hub #16](https://github.com/hloiseau/le-hub/issues/16).

Le package `internal/reminders` contient le moteur de décision pur ; `internal/upstream` collecte les sources et `internal/departures` produit la liste personnelle des départs. L’API raccorde désormais ces composants à des préférences, une file SQLite durable et une boîte de rappels dans le Hub. Cette première livraison concerne uniquement les films et épisodes commencés, encore accessibles et menacés par une action destructive vérifiée. Un transport de DM Discord est disponible sur activation explicite du serveur et consentement individuel distinct, décrit dans le [guide des DM](discord-reminders.md).

Les demandes jamais commencées, suivis explicites, inventaires complets d’épisodes et rappels de saisons entières restent à raccorder à la décision de retrait. Les suivis de disponibilité et les demandes de délai sont désormais utilisables séparément. La présence de cette boîte ne clôt pas l’ensemble des objectifs de [#20](https://github.com/hloiseau/le-concierge/issues/20) ou de [#26](https://github.com/hloiseau/le-concierge/issues/26).

## Liste personnelle des départs

`GET /api/v1/departures` utilise uniquement le compte Plex de la session, sans paramètre d’identité. La capacité `departures` indique que les sources sont configurées ; leur disponibilité est vérifiée au chargement. Le Hub affiche les départs estimés dans les 30 prochains jours sur l’accueil et dans « Avant leur départ ».

La portée `recorded_history` croise l’historique Tautulli de cet identifiant Plex avec les appartenances destructives Maintainerr. Chaque contenu est relu avec le jeton Plex individuel, après vérification du serveur. Les bibliothèques, types, règles actives et exclusions globales ou de groupe doivent correspondre. Une collection sans groupe vérifié, une évaluation de règle en échec ou une échéance inconnue/passée est écartée. Une exclusion de série protège ses descendants ; celle d’un épisode ne protège pas toute sa saison.

Un film ou épisode peut être commencé, terminé ou incertain. Le rapprochement historique exige la même clé et le même GUID ; une édition ambiguë ne reçoit pas l’état d’une autre. L’état Plex individuel courant peut confirmer une complétion. Une saison ou série reste incertaine tant que son inventaire entier n’est pas vérifié. L’absence d’historique ne signifie jamais « non commencé ». Les contenus terminés sont masqués par défaut, avec une case pour les inclure dans la vue complète.

Seul le relevé Maintainerr est partagé en mémoire pendant moins de 30 secondes. L’historique et les droits Plex sont relus à chaque requête ; les listes personnelles ne sont pas conservées. La collecte est bornée à 18 secondes et 100 métadonnées distinctes. Les relevés expirent au plus tard une minute après leur collecte. Une erreur invalide toute la réponse ; le Hub retire la liste au rechargement, à l’expiration ou à la déconnexion. La session est revérifiée après les lectures distantes. Les dates restent estimées : aucun bouton ne promet la conservation du contenu.

## Politique et portée des rappels

| Situation vérifiée | Décision |
| --- | --- |
| Film ou épisode commencé mais non terminé | Candidat à la boîte personnelle actuelle, même sans abonnement explicite |
| Film demandé/suivi, jamais commencé, retrait imminent | Parcours futur : demandes et suivis non raccordés à la collecte actuelle |
| Saison/série suivie avec épisodes confirmés restant à voir | Parcours futur : inventaire complet non disponible |
| Contenu terminé | Pas de rappel |
| Historique absent/incomplet ou état non reconnu | Pas d'affirmation « non vu », pas d'envoi automatique |
| Règle inactive ou action qui conserve les fichiers | Pas d'alerte de suppression |
| Source périmée, accès révoqué ou échéance inconnue/passée | Pas d'envoi |
| Retrait pendant les heures calmes | Pas d'envoi forcé pendant la plage silencieuse |

`GET` et `PUT /api/v1/reminders/preferences` lisent et enregistrent les réglages du compte connecté. L’activation reprend la préférence existante `notifications` : elle reste désactivée par défaut, mais un utilisateur qui l’avait déjà activée reçoit les nouveaux réglages par défaut. La capacité `notifications` et `notificationsAvailable` deviennent vraies lorsque Plex, Maintainerr et Tautulli sont configurés ; elles annoncent cette boîte Hub, pas un transport Discord ni une garantie de disponibilité des sources.

Les paliers par défaut sont J-7 et J-2 ; les réglages acceptent un à cinq jours distincts entre 1 et 30. Si un candidat est découvert tardivement, seul le palier courant le plus proche est retenu. Les heures calmes sont 22 h–8 h par défaut, en `Europe/Paris`, avec prise en compte des changements d’heure ; des heures identiques désactivent la plage silencieuse. La préférence `allUnwatched` est conservée mais n’élargit pas la portée de cette première collecte : l’absence d’historique ne prouve jamais qu’un contenu n’a pas été vu.

La livraison crée au maximum un regroupement par jour et par utilisateur dans la boîte du Hub, contenant jusqu’à dix rappels par défaut, réglable de 1 à 100. La date du jour et les heures calmes sont calculées à Paris. Les candidats excédentaires restent en attente et doivent encore être valides au prochain passage ; aucune livraison ne force les heures calmes pour rattraper une échéance.

## File durable et vérification avant affichage

Le schéma SQLite v4 conserve les préférences, les décisions en attente, leur état et les regroupements livrés. La clé de déduplication inclut l’utilisateur, le serveur, le contenu, l’action, l’échéance et le palier. Un changement de règle, d’échéance ou de préférence annule les décisions devenues caduques lors de leur revalidation. La désactivation de `notifications` annule immédiatement les rappels en attente par un déclencheur SQLite. La livraison dans la boîte et son historique quotidien sont enregistrés dans une même transaction.

Le processus API parcourt les autorisations Plex actives à chaque déclenchement d’une minute. Les comptes sont traités séquentiellement : une collecte lente peut allonger un passage ; il ne s’agit pas d’une garantie de livraison à la minute. `GET /api/v1/reminders` effectue également une actualisation. Avant chaque livraison et chaque liste active, les sources Maintainerr, Tautulli et les accès Plex sont collectés sans réutiliser le cache des départs. La session et l’identité sont revérifiées après ces lectures. Seuls les rappels livrés qui correspondent encore à un contenu commencé, une action et une échéance actuelles sont affichés.

Une panne de source conserve les décisions en attente pour une prochaine vérification et refuse toute ancienne liste à l’affichage. Une révocation, une déconnexion ou une suppression du compte empêche une livraison à partir d’une autorisation périmée. Les jetons restent exclusivement en mémoire et expirent après 12 h au maximum : redémarrage et déconnexion suspendent le traitement du compte jusqu’à une nouvelle connexion Plex, même si sa file persiste.

L’API purge les rappels 90 jours après leur expiration et l’historique quotidien après 90 jours, même si les sources sont désactivées. La livraison externe dispose de sa propre file transactionnelle, de cinq tentatives maximum, d’un bail de traitement et d’un délai progressif. Les DM fermés et les changements d’autorisation arrêtent la livraison. Le transport utilise un nonce persistant ; la déduplication distante Discord reste limitée dans le temps, sans garantie exactement une fois. Les détails et limites figurent dans le [guide des DM](discord-reminders.md).

Sauvegarder la base avant la migration v4. Pour revenir à une image antérieure, restaurer aussi la sauvegarde de son schéma : les anciens binaires refusent v4. Le [guide de déploiement](deployment.md#sauvegarde-et-retour-arrière) décrit la sauvegarde et la restauration.

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

La commande n’ouvre pas SQLite et n’envoie aucune commande de mutation. Sa sortie JSON contient versions, correspondance avec `PLEX_SERVER_ID`, nombres de collections/appartenances destructives, groupes de règles vérifiés, échéances inconnues et instant du relevé. Avec `--plex-user`, elle ajoute seulement le nombre de lectures enregistrées. Aucun titre, identifiant personnel, historique détaillé, adresse ou secret n’est imprimé. Son champ `remindersEnabled` reste `false` : ce diagnostic ne lance pas le planificateur de l’API. L’initialisation de la connexion Plex par Maintainerr peut mettre à jour ses propres paramètres de découverte ; voir l’audit des sources.

Limites du relevé : 2 Mio par réponse, 500 collections, 10 000 appartenances destructives et 5 000 lignes d’historique par utilisateur ; pages Tautulli de 100 lignes et délai total de 15 secondes par collecte. Un dépassement, un compte discordant, un total changeant ou une page manquante invalide le résultat entier. Les identités de serveur sont comparées avant de lire les contenus ; le diagnostic Tautulli vérifie sa configuration de serveur, pas la disponibilité de Plex.

Les collections inactives et actions non destructives sont écartées. Les délais nuls, zéro, négatifs ou hors borne ne deviennent pas des dates certaines. Une échéance positive est calculée depuis l’instant source par blocs de 24 heures, comme le worker audité ; elle reste estimée et peut différer de l’affichage calendaire amont lors d’un changement d’heure. L’historique Tautulli conserve ses identifiants Plex et son indicateur de complétion configuré, sans rapprochement par pseudo ni seuil arbitraire. Une absence de ligne reste inconnue, même si l’enregistrement de l’historique est activé aujourd’hui.

Le diagnostic a été exécuté sur les instances réelles : versions attendues, collections Maintainerr complètes, règles et exclusions lisibles, identité serveur identique entre les sources et la configuration locale, historique Tautulli du compte administrateur parcouru jusqu’au total annoncé. L’identité Plex directe et la lecture des utilisateurs Seerr ont aussi été vérifiées avec la configuration locale. Ces vérifications antérieures ne constituent pas une recette du déploiement NAS ni de la nouvelle boîte de rappels. Les changements du 16 octobre 2026 sont validés localement avec des sources simulées ; leur recette réelle nécessite une connexion Plex utilisateur et une vérification des règles de suppression installées.

## Demander un délai

Lorsque les sources de départ et `ADMIN_DISCORD_IDS` sont configurés, la capacité `extensionRequests` active le formulaire sur les départs personnels. `POST /api/v1/extension-requests` accepte seulement l’identifiant Plex déjà affiché, 1 à 30 jours et un motif de 500 caractères maximum. Le serveur relit les sources et les droits ; titre, action et échéance proviennent de cette observation. Une même personne ne crée qu’une demande par contenu/action/échéance ; dix demandes en attente ou créées dans les dernières 24 heures au maximum.

La liste personnelle présente les 100 dernières demandes. La file `/api/v1/admin/extension-requests`, protégée par l’identité Discord administrateur et une autorisation Plex active, présente les 100 plus anciennes en attente. Un administrateur peut enregistrer « examinée » ou « refusée » avec une note ; cette action ne change aucune règle, exclusion ou date Maintainerr. Le demandeur voit la réponse dans le Hub. Ces demandes sont conservées 90 jours et supprimées avec le compte. Aucun message à un administrateur n’est envoyé automatiquement.
