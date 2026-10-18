# Choisir ses salons Discord

Le panneau public **Choisir mes salons** ouvre un menu visible uniquement par le membre qui clique. `/salons` ouvre le même menu depuis tout autre salon du serveur où la commande est autorisée. Aucune connexion Plex, liaison de compte Hub ou publication Communauté n’est nécessaire.

Les choix cochés reflètent les rôles actuels du membre. Sélectionner un ou plusieurs intérêts remplace sa sélection parmi **ces seuls rôles** ; les autres rôles restent inchangés. Le bouton **Quitter tous ces salons** retire tous les rôles d’intérêts configurés. Les résultats et erreurs restent privés ; le panneau public n’est pas modifié par les choix individuels.

## Préparer les rôles et salons

1. Créer un rôle par intérêt, sans permission de serveur (`permissions: "0"`), non géré par une intégration, et le placer **sous le rôle Le Concierge**. Le bot refuse une liste contenant un rôle qui ne respecte plus ces conditions.
2. Préparer les salons thématiques et leurs permissions : masquer **Voir le salon** à `@everyone`, puis l’autoriser au rôle d’intérêt correspondant, avec les droits de lecture et d’écriture souhaités. Le menu attribue les rôles ; il ne crée pas les salons et ne modifie pas leurs permissions.
3. Garder **Gérer les rôles** (`Manage Roles`) sur Le Concierge. **Gérer les salons** (`Manage Channels`) sert uniquement à l’installation si le bot prépare les salons ; ce droit peut ensuite être retiré. Le bot n’a pas besoin d’Administrateur. Dans le salon texte du panneau, lui permettre de voir le salon, lire l’historique, envoyer des messages et intégrer des liens.

Un administrateur Discord voit toujours les salons, même après avoir quitté tous les intérêts. D’autres rôles ou autorisations individuelles peuvent aussi donner accès : vérifier la visibilité avec un membre ordinaire. Voir les [permissions et la hiérarchie Discord](https://docs.discord.com/developers/topics/permissions).

## Configuration facultative du bot

Renseigner les deux variables dans `bot.environment` du [YAML TrueNAS](../deploy/truenas-discord.yaml), ou dans l’environnement du processus bot. Les identifiants ci-dessous sont fictifs : les remplacer par ceux du serveur configuré dans `DISCORD_GUILD_ID`.

```dotenv
DISCORD_INTERESTS_CHANNEL_ID=100000000000000001
DISCORD_INTERESTS_JSON='[{"key":"films","label":"Films et séries","roleId":"200000000000000001","channelId":"300000000000000001"},{"key":"jeux","label":"Jeux vidéo","roleId":"200000000000000002","channelId":"300000000000000002"}]'
```

`DISCORD_INTERESTS_CHANNEL_ID` désigne le **salon texte public du panneau**. Chaque entrée associe une clé stable, un libellé, le rôle à attribuer et son salon thématique. Le bot utilise son `DISCORD_BOT_TOKEN` existant ; les deux variables d’intérêts ne sont pas nécessaires dans le service API.

La configuration accepte de 1 à 25 entrées, en JSON UTF-8 strict de 64 Kio maximum, avec uniquement `key`, `label`, `roleId` et `channelId`. Les clés sont uniques et suivent `[a-z][a-z0-9-]{0,31}`. Les libellés contiennent de 1 à 80 caractères : lettres, chiffres, espaces et ponctuation simple ; mentions, Markdown, emojis et caractères de contrôle sont refusés. Les identifiants de rôles et salons sont tous distincts, et différents de l’identifiant du serveur et du salon du panneau.

Laisser **les deux variables vides** désactive la fonction. Une configuration partielle ou invalide empêche le démarrage et indique l’erreur à corriger. Désactiver la fonction ne retire pas les rôles déjà attribués et ne supprime pas le panneau.

## Publier une fois, puis modifier le panneau existant

Redémarrer le bot après configuration : `/salons` est alors ajouté automatiquement au catalogue de commandes. Le démarrage **ne publie aucun panneau**. Pour le créer explicitement avec la configuration du conteneur :

```bash
docker exec le-concierge-bot /app/bot --publish-interests
```

La commande affiche l’identifiant du message créé. Le conserver pour mettre à jour ce même panneau :

```bash
docker exec le-concierge-bot /app/bot --publish-interests --message-id 400000000000000001
```

Sans `--message-id`, chaque exécution crée un nouveau message. Avec cet argument, le bot vérifie que le message lui appartient et se trouve dans le salon configuré. Une publication non confirmée n’est pas retentée automatiquement : vérifier le salon avant de relancer. `--register` et `--publish-interests` sont deux opérations séparées.

En local, les mêmes options sont disponibles avec `./bin/bot` après chargement de son environnement. Après modification de la configuration, redémarrer le bot, mettre à jour le panneau et rouvrir `/salons` : les anciens sélecteurs sont invalidés.

## Vérifier le parcours

Avec un membre ordinaire, ouvrir le panneau, choisir un intérêt, puis relancer `/salons` dans un autre salon : le choix doit rester coché et le salon correspondant accessible. Tester ensuite **Quitter tous ces salons**. Ces opérations ne demandent pas de connexion au Hub.

Si Discord refuse une écriture ou si les rôles ne peuvent plus être vérifiés, le bot ne confirme pas un succès complet. Il présente l’état relu lorsqu’il est disponible ; sinon les anciens contrôles sont retirés. En cas de limitation Discord, attendre puis relancer `/salons`. Vérifier en priorité le token, **Gérer les rôles**, la position du bot et les permissions des rôles d’intérêts.
