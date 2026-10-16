# Client personnel Downloadarr

Le client Go utilise le contrat v1 de [LibraryDownloadarr PR #1](https://github.com/hloiseau/LibraryDownloadarr/pull/1), audité au commit `797dc55a07fad3ae457e6cc99a6ea7cf2efabaf2`. Les routes et leurs limites viennent des fichiers `backend/src/routes/auth.ts`, `routes/integrations.ts`, `routes/downloads.ts`, `services/plexAuthFlow.ts`, `services/downloadService.ts`, `services/downloadStreams.ts` et `models/database.ts` de ce fork. Aucun endpoint de conversion Plex n’est appelé directement par Concierge.

## Configuration et identification

L’intégration reste désactivée tant que `DOWNLOADARR_URL` et `DOWNLOADARR_REVISION` ne sont pas configurés ensemble. L’URL est choisie par l’administrateur, sans identifiants, query string ou fragment ; HTTPS est obligatoire hors développement. `DOWNLOADARR_REVISION` doit être le SHA Git complet de 40 caractères hexadécimaux minuscules annoncé par **l’image réellement déployée**, via `BUILD_REVISION`. La présence d’une PR ou d’une image construite ne prouve pas son déploiement.

`GET /api/integrations/concierge` doit confirmer, avec une session personnelle Downloadarr :

- version de contrat exactement 1 et révision exactement égale à la configuration ;
- identifiant Plex numérique identique à celui du compte Hub courant, et identifiant du serveur Plex identique à la configuration Concierge ;
- expiration réelle de la session en millisecondes, encore future et compatible avec sa durée maximale de 24 heures ;
- choix des pistes, conversions préparées et tickets à usage unique disponibles ; file en mémoire ;
- au moins une qualité parmi `720p-2`, `720p-4`, `1080p-8`, sans doublon.

Une révision absente, une autre identité, un autre serveur ou une capacité manquante ferme l’accès. Le contrôle de révision compare la configuration et la réponse du service de confiance ; ce n’est pas une attestation cryptographique du digest de l’image.

## Connexion indépendante

`DownloadarrStart` ouvre le parcours PIN du fork. Seule une URL `https://app.plex.tv/auth` contenant les paramètres attendus dans son fragment est renvoyée au navigateur. Le `flowId` opaque reste côté API (`json:"-"`). Le fork ne renvoie pas sa date d’expiration PIN : Concierge utilise donc une échéance conservatrice de cinq minutes à partir du **début** de son appel, conforme à la durée enregistrée par le fork.

`DownloadarrAuthenticate` transmet le `flowId` en JSON. Seule la réponse HTTP 202 avec `pending:true` signifie « en attente ». HTTP 400 signifie parcours expiré/invalide ; HTTP 401 et 403 restent des refus d’authentification ou d’accès. Le bearer obtenu est une chaîne aléatoire de 64 caractères hexadécimaux, distincte d’un token Plex et de la session Hub. Les handlers le gardent en mémoire, lié au propriétaire et à sa session Hub ; il ne figure dans aucun DTO, URL, journal ou cookie.

Le client n’utilise pas le cookie jar HTTP partagé, ne suit aucune redirection et n’emprunte aucun compte administrateur. Les handlers revalident le contrat avant les opérations et après les lectures, puis la session locale avant d’exposer les résultats. Déconnexion, dissociation, changement de compte et échéance de session invalident aussi les flux ouverts.

## Options, préparation et file

`DownloadInput` contient `ratingKey`, `quality` et éventuellement `partKey`, `season`, `audio`, `subtitle`. Les options sont relues depuis les métadonnées personnelles accessibles au fork. Les valeurs transmises sont celles proposées : `plex`, `none` pour les sous-titres, `stream:<id>` pour un fichier individuel, `match:<description encodée>` pour une saison. Une saison n’accepte que les correspondances communes et non ambiguës. Le client borne les identifiants de sélection à 1 506 caractères et les listes à 256 choix.

La création requiert la confirmation du contenu, de la qualité et des pistes dans les handlers. Les profils produisent du MP4 H.264/AAC ; une saison produit un ZIP. Un choix explicite de pistes peut modifier les préférences de lecture de **ce compte Plex** : l’interface doit l’expliquer avant confirmation. Le fork refuse un remplacement silencieux par l’original ou par une autre piste.

Les snapshots exposent les états `preparing`, `ready`, `sending`, `error` et les phases `deciding`, `waiting`, `processing`, `finalizing`, `ready`. La progression reste `null` lorsqu’elle n’est pas mesurée ; zéro est conservé comme zéro. Seuls `ready`/`sending` peuvent annoncer la phase finale avec 100 % et tous les fichiers prêts. Les dates en millisecondes sont converties en UTC. Les titres et labels sont bornés ; les diagnostics du fournisseur sont remplacés par un message fixe.

La file accepte au plus deux tâches pour un propriétaire et huit globalement, erreurs conservées comprises. Une saison est bornée à 100 épisodes. La préparation expire après 24 heures ; un fichier prêt est gardé au plus six heures à partir de sa première disponibilité, sous réserve de sa présence réelle dans Plex. `sending` peut dépasser l’échéance pendant un transfert en cours. La liste n’est ni un historique persistant ni la preuve d’une sauvegarde sur le disque de l’utilisateur.

## Limites réseau et transfert

Chaque appel JSON dispose de six secondes et accepte au plus 2 Mio. Les réponses inattendues, redirections, JSON invalides, identifiants incohérents et capacités incompatibles sont refusés. `DownloadarrError` ne contient que le statut, un code fixe et éventuellement un `Retry-After` entier compris entre 1 et 300 secondes ; le corps d’erreur distant n’est jamais repris.

`DownloadarrTicket` demande un ticket à usage unique valable 60 secondes. `DownloadarrFile` le place uniquement dans le corps d’un POST, sans bearer ni cookie. Le fork retrouve et revalide lui-même la session liée au ticket avant les octets. L’ouverture de la réponse est bornée à 15 secondes, DNS/TLS compris, par annulation du contexte. Dès les en-têtes reçus, ce minuteur est arrêté ; aucun timeout HTTP global court n’interrompt un fichier valide. Le contexte du handler continue de borner la session et `Body.Close` annule la requête source. Les seuls types acceptés sont `video/mp4` et `application/zip` en HTTP 200 ; le handler copie uniquement des en-têtes de fichier vérifiés.

Aucun appel de mutation n’est automatiquement rejoué. Après une coupure ou un timeout de création, une conversion peut avoir démarré sans réponse observée : actualiser la liste avant de demander une nouvelle préparation. Après redémarrage de Downloadarr, les handles en mémoire disparaissent ; une absence n’entraîne pas de recréation automatique. Un transfert interrompu peut être explicitement retenté avec un **nouveau** ticket tant que le fichier prêt existe. La reprise HTTP Range n’est pas proposée.

## Vérification

`go test ./internal/upstream -count=1` couvre le parcours personnel complet avec serveur simulé, les identités/révisions/capacités/échéances, la séparation 202/400, les options et limites, les états et la progression nullable, les doublons de file, les refus d’accès, les réponses surdimensionnées, la confidentialité des erreurs, l’absence de cookies/redirections/rejeu et la durée du flux indépendante du timeout JSON. Les handlers et l’interface possèdent leurs propres tests d’intégration.

Aucune conversion réelle ni activation sur le NAS n’a été effectuée pendant cette implémentation. La révision installée, le droit Plex de téléchargement, la préparation réelle des codecs/pistes et le transfert navigateur restent à valider sur le déploiement choisi avant d’annoncer C24/H15 entièrement recettés.
