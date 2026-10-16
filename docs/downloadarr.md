# Audit Downloadarr pour Concierge #24 / Hub #15

Audit du 16 octobre 2026. Fork propriétaire : [hloiseau/LibraryDownloadarr](https://github.com/hloiseau/LibraryDownloadarr), branche [`codex/download-quality-a6fa824141dc`](https://github.com/hloiseau/LibraryDownloadarr/tree/codex/download-quality-a6fa824141dc), révision auditée `729af956aaa106380011d686ceed38d1350cf78f`. Le clone utilisateur `C:\Users\hugol\Downloadarr` est sur `main` ; il n'a pas été modifié. La correction amont est préparée dans un checkout isolé sous `.cache/downloadarr-integration`.

L'image réellement déployée n'est **pas confirmée** : aucune session administrateur du NAS n'est disponible, `/api/health` ne renvoie qu'un état et une date, et le tag mutable `quality-preview` ne prouve pas la révision de l'instance. Aucun conteneur, réglage NAS, téléchargement ou transcodage n'a été lancé pour cet audit.

## Capacités constatées dans le fork

| Domaine | Contrat existant et limites |
|---|---|
| Connexion | PIN Plex propre à Downloadarr, handle aléatoire expirant, session bearer personnelle de 24 h par défaut ; identité Plex stable stockée sous `plex:<id>` ; aucun partage de cookie avec Le Hub |
| Droits | Métadonnées lues avec le token personnel, serveur configuré exact, restrictions Plex `allowSync`, politique propre à Downloadarr par utilisateur/bibliothèque/qualité |
| Qualité | `720p-2`, `720p-4`, `1080p-8` ; cible MP4 H.264/AAC vérifiée, refus de repli silencieux vers l'original |
| Pistes | `/api/downloads/options` donne audio/sous-titres disponibles ; piste exacte pour un fichier, correspondance non ambiguë commune à tous les épisodes d'une saison ; `plex` / sous-titres `none` ; sélection explicite pouvant changer les pistes choisies du compte Plex |
| Préparation | `POST /api/downloads`, `{ratingKey,quality,partKey?,season?,audio?,subtitle?}` ; le Hub devra demander confirmation avant toute mutation |
| Suivi | `GET /api/downloads` et `/:id` propres au propriétaire ; states preparing/ready/sending/error ; stages deciding/waiting/processing/finalizing/ready ; progress nullable, jamais à reconstruire à partir d'un simple stade |
| Limites | Deux jobs par utilisateur, huit globalement ; TTL préparation 24 h, cache prêt 6 h, expiration PMS possible avant ; saisons au plus 100 épisodes |
| Doublons | Déduplication des créations simultanées et réemploi par propriétaire/serveur/média/qualité/pistes, avec revalidation de source et droits ; les changements de pistes ne doivent pas modifier une conversion encore active |
| Fichiers | Ticket POST à usage unique de 60 s, lié au job/propriétaire/session ; aucun bearer/token Plex dans l'URL ; MP4 ou ZIP de saison ; vérifications avant octets |
| Reprise | Une interruption de transfert peut réutiliser une conversion encore prête ; pas de contrat HTTP Range. Jobs en mémoire : un redémarrage perd leurs handles ; ne jamais relancer automatiquement une conversion devenue introuvable |
| Historique | `/api/media/download-history` borné et personnel ; rows anciennes `recorded` et requêtes `requested` ne prouvent pas un transfert complet |

Sources de code : [auth](https://github.com/hloiseau/LibraryDownloadarr/blob/729af956aaa106380011d686ceed38d1350cf78f/backend/src/routes/auth.ts), [routes de conversion](https://github.com/hloiseau/LibraryDownloadarr/blob/729af956aaa106380011d686ceed38d1350cf78f/backend/src/routes/downloads.ts), [moteur](https://github.com/hloiseau/LibraryDownloadarr/blob/729af956aaa106380011d686ceed38d1350cf78f/backend/src/services/downloadService.ts), [sélections](https://github.com/hloiseau/LibraryDownloadarr/blob/729af956aaa106380011d686ceed38d1350cf78f/backend/src/services/downloadStreams.ts), [politiques](https://github.com/hloiseau/LibraryDownloadarr/blob/729af956aaa106380011d686ceed38d1350cf78f/backend/src/services/downloadPolicy.ts).

## Prérequis amont traité

L'API `/auth/me` ne prouvait que l'identifiant local Downloadarr, pas l'identité Plex numérique et le serveur communs au Hub. Le [contrat ciblé](downloadarr-integration-issue.md) décrit `GET /api/integrations/concierge` personnel avec version, révision de build, ID Plex, serveur et expiration réelle, plus les capacités permises. La création de l'issue amont demandée par #24 a été tentée : GitHub renvoie `410 Issues has been disabled in this repository`. Une implémentation est prête dans le commit local `09e53fb` de la branche isolée `feat/concierge-personal-contract`, avec 67 tests backend réussis. Sa publication et l'ouverture d'une PR attendent l'autorisation de publier dans ce dépôt supplémentaire ; aucune PR amont n'existe encore. Le contrat reste conservé ici avec référence à #24.

La correction amont ajoute ce contrat, vérifie le serveur et l'accès avec les seuls credentials personnels, revalide session/configuration/politique après la lecture, et retire les titres des jobs lorsque les droits Plex/bibliothèque ont été révoqués. La révision est injectée au build et dans le label OCI ; les builds locaux sans révision l'annoncent comme `null`.

Le client Concierge n'est pas activé avant revue/déploiement de ce contrat et confirmation de la révision installée. Le Hub conserve son lien personnel vers l'application Downloadarr dans les services. Il ne propose ni faux choix de conversion ni progression simulée. La future liaison démarrera un PIN Downloadarr distinct, comparera les IDs Plex/serveur avec le grant actuel, gardera le bearer uniquement en mémoire par propriétaire/session, puis utilisera les routes existantes ; elle ne recréera pas le moteur.

Tests amont : compilation TypeScript et tests Node avec sources Plex simulées, contrôles d'identité/session/politique, révocation pendant la vérification, listes/statuts privés, reuse et tickets. Les tests existants de conversion sont simulés. La vérification matérielle/logicielle de l'image déployée et de vrais transferts reste à faire sous contrôle de l'opérateur.
