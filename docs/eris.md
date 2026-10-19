# Eris sur Discord

Le bot conserve son compte, ses identifiants, ses commandes, les liaisons Plex/Discord et ses données. L’image reste `ghcr.io/hloiseau/le-concierge:edge` et la base conserve son chemin actuel. Il n’y a pas de migration à effectuer.

## Nom Discord

`DISCORD_BOT_NAME=Eris` synchronise le nom d’utilisateur du bot et son surnom sur `DISCORD_GUILD_ID` au démarrage. La synchronisation vérifie d’abord le compte associé au token, lit les noms existants et ne les modifie que si nécessaire. Le bot doit avoir la permission **Changer le pseudo** pour son surnom. Un refus Discord ou une limite de changement de nom ne bloque pas les commandes ; consulter le journal puis réessayer plus tard avec :

```sh
/app/bot --sync-identity
```

`DISCORD_BOT_NAME=disabled` désactive cette synchronisation. Le nom de l’application affiché dans OAuth2 se règle séparément dans Discord Developer Portal → General Information. Le code ne modifie pas l’avatar.

## Chat privé

- `/eris message:Salut Eris` : démarrer ou poursuivre une conversation.
- `/eris oublier:true` : effacer la conversation sans appeler le modèle.
- `/eris message:On recommence oublier:true` : effacer puis envoyer un nouveau message.

Les réponses sont éphémères, visibles uniquement par leur auteur. Le bot ne lit pas les messages des salons ; aucun Gateway ni intent Message Content n’est nécessaire. L’identité vient de l’interaction Discord signée et du serveur configuré. Il n’est pas nécessaire de lier Plex pour discuter.

La mémoire contient au plus deux échanges précédents par membre et par serveur, expire après 30 minutes d’inactivité et disparaît au redémarrage. Le bot conserve au plus 256 conversations et accepte deux traitements de chat simultanés, séparés des traitements média. Un membre doit attendre dix secondes entre deux générations ordinaires. Aucun historique n’est écrit dans SQLite ni dans les journaux.

Eris conserve une voix calme, brève, sarcastique et faussement innocente, adaptée à Discord. Elle répond dans la langue du message. Son texte généré ne déclenche aucune action et ne reçoit ni secrets Discord, ni historique Plex, ni métriques, ni résultats Seerr. Les commandes existantes restent la source des informations en temps réel.

## Connecter le moteur local

Renseigner **dans le conteneur bot** :

```yaml
DISCORD_BOT_NAME: Eris
ERIS_API_URL: http://eris-runtime:8090/v1
ERIS_MODEL: eris-lfm2.5-v1
ERIS_API_KEY_FILE: /run/secrets/eris_api_key
```

Monter le fichier secret en lecture seule dans le bot et dans le moteur. Générer sa valeur avec `openssl rand -hex 32`. Ne pas conserver également `ERIS_API_KEY` si `_FILE` est renseigné. L’URL contient `/v1` ; le bot ajoute `/chat/completions`. HTTP convient au réseau Docker privé ; utiliser HTTPS si le moteur est sur une autre machine.

Le service d’inférence et ses instructions d’installation sont dans le dépôt privé d’Eris, dossier `server/`. Il charge le **checkpoint fusionné BF16** produit par le pipeline existant ; l’adaptateur LoRA seul et l’export ONNX destiné au navigateur ne conviennent pas à ce service. Le code, les poids, les assets et les souvenirs privés ne sont pas recopiés dans ce dépôt public.

Les deux conteneurs doivent partager un réseau Docker pour résoudre `eris-runtime`. Le Compose du moteur accepte le nom du réseau existant via `ERIS_NETWORK` ; aucun port d’inférence n’est publié. Le service CPU dispose d’un budget initial de quatre cœurs et 8 Go de RAM, à mesurer sur le NAS. Le bot limite chaque appel à 50 secondes et n’effectue aucun repli vers un fournisseur externe.

L’URL vide désactive seulement la génération. `/eris` explique alors que le moteur n’est pas connecté, `/eris oublier:true` fonctionne et toutes les commandes média restent disponibles. Ne renseigner l’URL qu’une fois le moteur prêt ; un échec de génération ne remplace pas une réponse par un résultat inventé.

## Vérifier la mise en service

1. Démarrer le moteur avec le checkpoint entraîné et vérifier son état de santé.
2. Vérifier l’identité et le français avec les prompts du guide privé ; la qualité du modèle anglais spécialisé doit être mesurée avant de l’ouvrir aux membres.
3. Recréer le bot avec les paramètres ci-dessus et l’image de la branche validée, puis vérifier son nom et `/aide`.
4. Tester deux membres : leur historique doit rester séparé ; tester l’effacement et les commandes média pendant une génération.

Les tests automatisés couvrent le protocole HTTP, les signatures, les réponses privées, l’isolation des conversations, l’effacement, les limites, les erreurs et l’identité Discord. Ils n’attestent pas la qualité du checkpoint : les poids entraînés et l’accès au serveur Discord restent nécessaires pour ce dernier contrôle.
