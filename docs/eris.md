# Eris sur Discord

Le bot conserve son compte, ses identifiants, ses commandes, les liaisons Plex/Discord et ses données. L’image reste `ghcr.io/hloiseau/le-concierge:edge` et la base conserve son chemin actuel. Il n’y a pas de migration à effectuer.

## Nom Discord

`DISCORD_BOT_NAME=Eris` synchronise le nom d’utilisateur du bot et son surnom sur `DISCORD_GUILD_ID` au démarrage. La synchronisation vérifie d’abord le compte associé au token, lit les noms existants et ne les modifie que si nécessaire. Le bot doit avoir la permission **Changer le pseudo** pour son surnom. Un refus Discord ou une limite de changement de nom ne bloque pas les commandes ; consulter le journal puis réessayer plus tard avec :

```sh
/app/bot --sync-identity
```

`DISCORD_BOT_NAME=disabled` désactive cette synchronisation. Le nom de l’application affiché dans OAuth2 se règle séparément dans Discord Developer Portal → General Information. Le code ne modifie pas l’avatar.

## Discuter avec Eris

- `@Eris Salut` : mentionner le compte Eris et recevoir une réponse dans le même salon ou fil.
- `/eris` : afficher comment lui parler.
- `/eris message:Salut Eris` : démarrer ou poursuivre une conversation.
- `/eris oublier:true` : effacer la conversation sans appeler le modèle.
- `/eris message:On recommence oublier:true` : effacer puis envoyer un nouveau message.

Les réponses à `/eris` sont éphémères, visibles uniquement par leur auteur. Les réponses aux mentions sont publiques dans le salon concerné, en réponse au message d’origine et sans notification de masse. Il faut sélectionner le compte Eris dans la mention : écrire son nom seul, mentionner un rôle ou répondre sans la mentionner explicitement ne déclenche pas le chat. Les messages des bots, webhooks, autres serveurs et messages privés sont ignorés. Il n’est pas nécessaire de lier Plex pour discuter.

Quand le moteur et le token Discord sont configurés, le bot ouvre une connexion Gateway avec le seul intent `GUILD_MESSAGES`. L’intent privilégié `MESSAGE_CONTENT` n’est pas nécessaire : [Discord transmet le contenu des messages qui mentionnent le bot](https://docs.discord.com/developers/events/gateway#message-content-intent). Accorder **Voir le salon**, **Envoyer des messages**, **Lire l’historique des messages** (réponses au message d’origine) et, pour les fils, **Envoyer des messages dans les fils** dans les salons où Eris doit répondre. La connexion se rétablit après une coupure ; un échec ne bloque pas les commandes HTTP.

La mémoire contient au plus deux échanges précédents, expire après 30 minutes d’inactivité et disparaît au redémarrage. Le chat privé est séparé des conversations publiques, elles-mêmes séparées par membre et par salon ou fil. `/eris oublier:true` efface toutes ces mémoires pour le membre sur ce serveur, sans supprimer les messages déjà publiés dans Discord. Le bot conserve au plus 256 conversations et accepte deux traitements de chat simultanés, séparés des traitements média. Une conversation doit attendre dix secondes entre deux générations ordinaires. Les messages sont limités à 500 caractères. Aucun historique n’est écrit dans SQLite ni dans les journaux.

Eris conserve une voix calme, brève, sarcastique et faussement innocente, adaptée à Discord. Elle répond dans la langue du message. Son texte généré ne déclenche aucune action et ne reçoit ni secrets Discord, ni historique Plex, ni métriques, ni résultats Seerr. Les commandes existantes restent la source des informations en temps réel.

## Connecter le moteur local

Renseigner **dans le conteneur bot** :

```yaml
DISCORD_BOT_NAME: Eris
ERIS_API_URL: http://eris-runtime:8090/v1
ERIS_MODEL: eris-qwen3-4b-v1
ERIS_API_KEY_FILE: /run/secrets/eris_api_key
```

Monter le fichier secret en lecture seule dans le bot et dans le moteur. Générer sa valeur avec `openssl rand -hex 32`. Ne pas conserver également `ERIS_API_KEY` si `_FILE` est renseigné. L’URL contient `/v1` ; le bot ajoute `/chat/completions`. HTTP convient au réseau Docker privé ; utiliser HTTPS si le moteur est sur une autre machine.

Le service d’inférence et ses instructions d’installation sont dans le dépôt privé d’Eris, dossier `server/`. Il charge **Qwen3 4B Instruct en GGUF Q5_K_M**, avec une révision et une empreinte épinglées. La personnalité d’Eris est fournie par le bot ; les poids Qwen ne sont pas réentraînés. Les deux candidats LFM ont échoué à la recette Discord. Le navigateur Eris conserve son moteur actuel, et les poids et souvenirs privés ne sont pas recopiés dans ce dépôt public.

Les deux conteneurs doivent partager un réseau Docker pour résoudre `eris-runtime`. Le Compose du moteur accepte le nom du réseau existant via `ERIS_NETWORK` ; aucun port d’inférence n’est publié. Le service dispose de quatre CPU et 6 Go de RAM, sans accès au GPU de transcodage. Le bot limite chaque appel à 50 secondes et n’effectue aucun repli vers un fournisseur externe. Le moteur refuse les générations incomplètes qui dépassent ses 160 tokens ou sa limite souple de 42 secondes.

L’URL vide désactive seulement la génération. `/eris` explique alors que le moteur n’est pas connecté, `/eris oublier:true` fonctionne et toutes les commandes média restent disponibles. Ne renseigner l’URL qu’une fois le moteur prêt ; un échec de génération ne remplace pas une réponse par un résultat inventé.

## Vérifier la mise en service

1. Démarrer le moteur avec le fichier GGUF vérifié et vérifier son état de santé.
2. Vérifier l’identité, les commandes, la mémoire et le français avec les scénarios du guide privé sur le CPU du NAS.
3. Recréer le bot avec les paramètres ci-dessus et l’image de la branche validée, puis vérifier son nom et `/aide`.
4. Tester deux membres : leur historique doit rester séparé ; tester l’effacement et les commandes média pendant une génération.

Les tests automatisés couvrent le protocole HTTP, les signatures, les réponses privées, l’isolation des conversations, l’effacement, les limites, les erreurs et l’identité Discord. Ils ne remplacent pas la lecture des réponses du modèle réel, ni la recette finale dans Discord.
