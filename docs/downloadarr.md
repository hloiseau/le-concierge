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

L'API `/auth/me` ne prouvait que l'identifiant local Downloadarr, pas l'identité Plex numérique et le serveur communs au Hub. Le [contrat ciblé](downloadarr-integration-issue.md) décrit `GET /api/integrations/concierge` personnel avec version, révision de build, ID Plex, serveur et expiration réelle, plus les capacités permises. La création de l'issue amont demandée par #24 a été tentée : GitHub renvoie `410 Issues has been disabled in this repository`. L'implémentation du commit `09e53fb`, validée par 67 tests backend, est publiée dans la [PR brouillon Downloadarr #1](https://github.com/hloiseau/LibraryDownloadarr/pull/1), depuis `feat/concierge-personal-contract` vers la branche auditée. La PR porte le contrat amont et ajoute une CI de tests et construction d'image sans publication. Sa fusion et la vérification de l'image réellement déployée restent à faire.

La correction amont ajoute ce contrat, vérifie le serveur et l'accès avec les seuls credentials personnels, revalide session/configuration/politique après la lecture, et retire les titres des jobs lorsque les droits Plex/bibliothèque ont été révoqués. La révision est injectée au build et dans le label OCI ; les builds locaux sans révision l'annoncent comme `null`.

Le raccordement Concierge/Hub est implémenté, désactivé par défaut. Il nécessite une image contenant le contrat de la PR amont et une révision explicitement configurée. Aucun service réel n'a été modifié ni aucune conversion lancée pendant ce développement. Le lien public vers l'application Downloadarr reste disponible dans les services.

## Activation et parcours personnel

Configurer ensemble `DOWNLOADARR_URL` (base HTTPS sans `/api`, sauf développement local) et `DOWNLOADARR_REVISION` (les 40 caractères hexadécimaux de la révision Git de l'image installée). Vérifier le label OCI `org.opencontainers.image.revision` de cette image. La révision annoncée par le contrat doit être strictement identique ; une ancienne image ou une autre révision bloque les opérations. Ces variables appartiennent uniquement au backend. Les exemples TrueNAS les laissent vides.

La page Hub `/telechargements` ouvre un PIN Plex distinct pour Downloadarr. Le Concierge compare l'identité Plex numérique et le serveur avec ceux de la session Hub. Le bearer, le handle de connexion et les tickets de fichier restent en mémoire serveur ; seuls l'URL de connexion, l'expiration et les résultats nettoyés arrivent au navigateur. L'association appartient à la session Hub courante et est supprimée à la déconnexion, au retrait, au changement d'identité, à l'expiration ou au redémarrage. Retirer l'association n'efface pas les préparations distantes et ne déconnecte pas les autres navigateurs Downloadarr.

Depuis un film de la bibliothèque ou un lien Plex précis, le membre choisit la qualité et les pistes réellement proposées. La confirmation précède la préparation ; le choix de piste peut modifier les préférences audio/sous-titres de ce compte Plex. Les saisons utilisent les sélections sémantiques prises en charge par Downloadarr, pas un identifiant de piste supposé commun à tous les épisodes. Le moteur amont décide de la réutilisation, de la file et de ses limites. Les étapes, nombres de fichiers prêts et pourcentages viennent de lui ; une progression inconnue reste inconnue.

Le fichier prêt est demandé par un formulaire POST de même origine. Le Concierge obtient un ticket à usage unique puis diffuse le MP4 ou ZIP ; le Hub ne reçoit jamais le ticket et ne charge pas le fichier entier en mémoire. Le proxy conserve la contre-pression, arrête la source quand le navigateur se déconnecte et ne suit aucun redirect. Le flux s'arrête si la session expire ou si l'association est retirée. Les requêtes partielles `Range` ne sont pas prises en charge. Une erreur de transfert permet de réessayer le fichier prêt, sans relancer automatiquement la conversion. La file amont est en mémoire : après redémarrage ou expiration, les jobs disparus ne sont pas recréés automatiquement.

## Recette restante sur l'installation

Après revue et installation de l'image compatible : vérifier deux comptes aux droits distincts, le refus d'une mauvaise révision ou d'un autre serveur, une préparation autorisée, les pistes d'une saison, un transfert interrompu puis retenté et une révocation pendant le transfert. Les tests automatisés utilisent des services simulés ; ils ne confirment ni la version actuellement installée ni les performances du NAS.

Tests amont : compilation TypeScript et tests Node avec sources Plex simulées, contrôles d'identité/session/politique, révocation pendant la vérification, listes/statuts privés, reuse et tickets. Les tests existants de conversion sont simulés. La vérification matérielle/logicielle de l'image déployée et de vrais transferts reste à faire sous contrôle de l'opérateur.
