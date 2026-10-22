# Eris sur Discord

Le bot conserve son compte, ses identifiants, ses commandes, les liaisons Plex/Discord et ses données. L’image applicative reste `ghcr.io/hloiseau/le-concierge` (version de production épinglée par SHA) et la base conserve son chemin actuel. Il n’y a pas de migration à effectuer.

## État installé

Au 22 octobre 2026, le bot/API `20f96b6` et le runtime `5dd08ded` sont déployés. Voir le [bilan daté](status-2026-10-22.md) pour la recette, les mesures réelles et le [travail restant](https://github.com/hloiseau/le-concierge/issues/62). La personnalité et le classement des intentions restent imparfaits ; la recette brute est à 25/32, et non 32/32.

## Nom Discord

`DISCORD_BOT_NAME=Eris` synchronise le nom d’utilisateur du bot et son surnom sur `DISCORD_GUILD_ID` au démarrage. La synchronisation vérifie d’abord le compte associé au token, lit les noms existants et ne les modifie que si nécessaire. Le bot doit avoir la permission **Changer le pseudo** pour son surnom. Un refus Discord ou une limite de changement de nom ne bloque pas les commandes ; consulter le journal puis réessayer plus tard avec :

```sh
/app/bot --sync-identity
```

`DISCORD_BOT_NAME=disabled` désactive cette synchronisation. Le nom de l’application affiché dans OAuth2 se règle séparément dans Discord Developer Portal → General Information. Le code ne modifie pas l’avatar.

## Discuter avec Eris

- `@Eris Salut` : mentionner le compte Eris et recevoir une réponse dans le même salon ou fil. Le bouton **Répondre** de Discord permet ensuite de continuer sans réécrire la mention.
- `/eris` : afficher comment lui parler.
- `/eris message:Salut Eris` : démarrer ou poursuivre une conversation.
- `/eris oublier:true` : effacer la conversation sans appeler le modèle.
- `/eris message:On recommence oublier:true` : effacer puis envoyer un nouveau message.

Les réponses à `/eris` sont éphémères, visibles uniquement par leur auteur. Les mentions et réponses à Eris sont publiques dans le salon concerné, sans notification de masse. Une réponse doit référencer un message d'Eris dans le même salon ; écrire son nom seul, mentionner un rôle ou répondre à quelqu'un d'autre ne déclenche pas le chat. Les messages des bots, webhooks, autres serveurs et messages privés sont ignorés. Il n’est pas nécessaire de lier Plex pour discuter.

Quand le moteur et le token Discord sont configurés, le bot ouvre une connexion Gateway avec `GUILD_MESSAGES`. Pour recevoir aussi le texte des réponses dont la notification est désactivée, activer **Message Content Intent** dans l'application Discord, puis `DISCORD_MESSAGE_CONTENT=true` dans le bot. Le réglage est désactivé par défaut ; sans lui, [Discord transmet seulement certains contenus, dont les mentions du bot](https://docs.discord.com/developers/events/gateway#message-content-intent). Même activé, les messages non adressés à Eris sont écartés avant tout appel au modèle ou à l'API média. Accorder **Voir le salon**, **Envoyer des messages**, **Lire l’historique des messages** et, pour les fils, **Envoyer des messages dans les fils**. La connexion se rétablit après une coupure ; un échec ne bloque pas les commandes HTTP.

La mémoire contient au plus deux échanges précédents, expire après 30 minutes d’inactivité et disparaît au redémarrage. Le chat privé est séparé des conversations publiques, elles-mêmes séparées par membre et par salon ou fil. `/eris oublier:true` efface toutes ces mémoires pour le membre sur ce serveur, sans supprimer les messages déjà publiés dans Discord. Le bot conserve au plus 256 conversations et accepte deux traitements de chat simultanés, séparés des traitements média. Une conversation doit attendre dix secondes entre deux générations ordinaires. Les messages sont limités à 500 caractères. Aucun historique n’est écrit dans SQLite ni dans les journaux.

Eris se présente comme Eris, le bot lui-même. Elle répond brièvement, avec ses goûts, du franc-parler et un humour pince-sans-rire. Une provocation dirigée contre elle peut recevoir une pique ; une confidence sérieuse appelle de l'écoute, et elle doit arrêter de chambrer quand on le demande. Exprimer un goût, comme « mon film préféré est Hot Fuzz », appelle une réponse de discussion ; une demande suivante comme « tu peux le demander dans Seerr ? » reprend le titre de la mémoire courte.

## Chercher et demander des médias

Lier Discord dans **Mon compte** du Hub après une connexion Plex permet de demander naturellement :

- `@Eris Tu as Arrival sur Plex ?` : titres lisibles, synopsis, genre et durée.
- `@Eris Un thriller de moins de deux heures pour ce soir` : jusqu’à trois titres vérifiés dans le catalogue accessible au membre.
- `@Eris Des films proches de Hot Fuzz ?` : recommandations Seerr croisées avec le catalogue Plex du membre ; si nécessaire, complément avec au moins deux genres communs vérifiés, indiqués dans la réponse.
- `@Eris Du même réalisateur que Hot Fuzz à demander dans Seerr ?` : recherche du réalisateur dans les crédits, puis de sa filmographie. Les titres disponibles, bloqués ou en cours de demande sont exclus ; les boutons publics conservent une confirmation séparée avant tout envoi.
- `@Eris Quoi de neuf ?` : derniers ajouts Plex.
- `/eris message:Un film de science-fiction que je n’ai pas vu` : filtre les contenus non marqués vus sur ce compte Plex ; cela ne prouve pas qu’ils n’ont jamais été vus ailleurs.
- `/eris message:Ma demande de Dune est prête ?` : demandes du compte Seerr associé à l’identité Plex, avec leur état.
- `@Eris Demande Hot Fuzz` : choisir le titre et son année dans le salon, puis **Confirmer la demande** ou **Annuler**. Pour une série, préciser les saisons, par exemple « demande la saison 2 de Severance ». `/eris message:Demande Hot Fuzz` garde le même parcours en réponse éphémère.

Une demande dans le salon cherche directement dans Seerr et affiche les choix, la confirmation et le résultat dans ce salon. Seul son auteur peut utiliser les boutons, une fois et pendant dix minutes. Chaque clic public reçoit une nouvelle réponse : le clic d'un autre membre ne peut pas remplacer le message initial. Les boutons d'une demande éphémère ne peuvent pas servir dans un parcours public. Le résultat rejoint uniquement la mémoire de la conversation concernée. Les listes de demandes personnelles et les filtres de visionnage restent accessibles via `/eris`, sans être publiés dans le salon. Les recherches publiques n’exposent ni historique, ni demandes d'autres membres, ni liens personnels Plex. Les droits Plex du membre sont utilisés pour vérifier la lecture ; l’état global Seerr ne prouve pas sa disponibilité personnelle.

Le modèle produit une intention JSON limitée aux outils autorisés. Le bot demande le mode JSON contraint au moteur local, à température 0,45 pour varier les formulations. Le code valide les filtres, appelle l’API puis affiche les résultats sans les faire réécrire par le modèle. Aucun jeton, URL interne ou identité technique n’est fourni au modèle. Le texte des résultats rejoint seulement la mémoire courte de la conversation concernée, pour comprendre « le deuxième » ou une préférence corrigée. Une sortie invalide demande une précision plutôt que d’exécuter une action.

Une absence dans Plex n'empêche pas une recherche dans le catalogue Seerr. Pour préparer une demande, le titre doit être présent dans le message ou la mémoire de cette conversation ; sinon Eris demande de le préciser. Les exemples de sa consigne, les conversations d'autres membres et la mémoire expirée ne peuvent pas fournir le titre. Un titre traduit ou reformulé au point de ne plus correspondre au texte de la conversation peut aussi nécessiter une précision.

`POST /internal/eris` exige le secret bot/API et une preuve Discord. Pour `/eris` et les boutons, l’API revérifie l’interaction signée. Pour un message, elle relit le message et son salon auprès de Discord et vérifie auteur humain, serveur, fraîcheur et mention explicite ou référence à un message d'Eris dans le même salon. L’association Discord/Plex et l’accès courant sont revérifiés avant publication. Les appels sont bornés à 20 secondes et limités par compte.

La préparation d’une demande est en lecture seule. Les choix expirent après dix minutes et au redémarrage, restent liés au compte et sont consommés avant tout envoi. Seul le bouton de confirmation déclenche le POST Seerr, sous l’identité Seerr du membre (`X-Api-User`, vérifiée via `/auth/me`), sans contourner droits, quotas ou approbations. Qualité standard ; aucune sélection de serveur, dossier ou profil par le modèle. Un double clic ne renvoie pas la demande. Après une erreur ambiguë, Eris demande de vérifier Seerr avant de réessayer. Un simple « oui » dans le chat ne vaut pas confirmation.

Limites : recherche de trois titres au maximum par réponse ; recommandations sur un catalogue complet de 5 000 titres maximum, sinon refus de conclure ; recherche personnelle par titre bornée à 1 000 demandes. Les saisons doivent être explicites, les épisodes individuels et demandes 4K ne sont pas proposés par ce parcours. Les résultats factuels sont rendus en français ; certaines recommandations formulées en anglais restent mal reconnues et demandent une précision. Les commandes média existantes restent disponibles.

## Connecter le moteur local

Renseigner **dans le conteneur bot** :

```yaml
DISCORD_BOT_NAME: Eris
ERIS_API_URL: http://eris-runtime:8090/v1
ERIS_MODEL: eris-qwen35-9b-v1
ERIS_API_KEY_FILE: /run/secrets/eris_api_key
```

Monter le fichier secret en lecture seule dans le bot et dans le moteur. Générer sa valeur avec `openssl rand -hex 32`. Ne pas conserver également `ERIS_API_KEY` si `_FILE` est renseigné. L’URL contient `/v1` ; le bot ajoute `/chat/completions`. HTTP convient au réseau Docker privé ; utiliser HTTPS si le moteur est sur une autre machine.

Le service d’inférence et ses instructions d’installation sont dans le dépôt privé d’Eris, dossier `server/`. Il charge **Qwen3.5 9B en GGUF Q4_K_M**, avec une révision et une empreinte épinglées. La personnalité d’Eris est fournie par le bot ; les poids Qwen ne sont pas réentraînés. Les deux candidats LFM ont échoué à la recette Discord. Le navigateur Eris conserve son moteur actuel, et les poids et souvenirs privés ne sont pas recopiés dans ce dépôt public.

Les deux conteneurs doivent partager un réseau Docker pour résoudre `eris-runtime`. Le Compose du moteur accepte le nom du réseau existant via `ERIS_NETWORK` ; aucun port d’inférence n’est publié. Le service dispose de quatre CPU et 6 Go de RAM. Le profil Arc A310 du dépôt privé ajoute Vulkan, huit couches GPU et des lots de 128 tokens ; environ 1,85 Go de mémoire vidéo sont utilisés, le reste du modèle reste sur CPU. Le profil CPU demeure disponible. Le bot limite chaque appel à 50 secondes et n’effectue aucun repli vers un fournisseur externe. Le moteur refuse les générations incomplètes qui dépassent ses 160 tokens ou sa limite souple de 42 secondes.

L’URL vide désactive seulement la génération. `/eris` explique alors que le moteur n’est pas connecté, `/eris oublier:true` fonctionne et toutes les commandes média restent disponibles. Ne renseigner l’URL qu’une fois le moteur prêt ; un échec de génération ne remplace pas une réponse par un résultat inventé.

## Vérifier la mise en service

1. Démarrer le moteur avec le fichier GGUF vérifié et vérifier son état de santé.
2. Vérifier l’identité, les commandes, la mémoire et le français avec les scénarios du guide privé sur le profil CPU ou GPU retenu ; pour le GPU, mesurer aussi le partage avec Plex.
3. Recréer le bot avec les paramètres ci-dessus et l’image de la branche validée, puis vérifier son nom et `/aide`.
4. Tester deux membres : leur historique doit rester séparé ; tester l’effacement et les commandes média pendant une génération.

Les tests automatisés couvrent le protocole HTTP, les signatures, les réponses privées, l’isolation des conversations, l’effacement, les limites, les erreurs et l’identité Discord. Ils ne remplacent pas la lecture des réponses du modèle réel, ni la recette finale dans Discord.

Le [terrain de test conversationnel](../evals/eris/README.md) compare la consigne actuelle à une candidate plus spontanée et mordante sur des scénarios synthétiques séparés. Il contient un corpus de références, un runner Codex/HTTP et les résultats mesurés. La candidate et les essais par abonnement restent expérimentaux : aucune bascule du bot ni modification de la personnalité installée n'est effectuée par ce banc d'essai.
