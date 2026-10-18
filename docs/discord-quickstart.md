# Brancher Le Concierge à Discord

Une application Discord dédiée sert à la liaison facultative du compte Discord et aux commandes du bot. La connexion au Hub se fait avec Plex. Le bot reçoit des interactions HTTP : aucun Gateway ni privileged intent n'est nécessaire.

## 1. Créer l'application et récupérer les valeurs

Ouvrir le [Discord Developer Portal](https://discord.com/developers/applications), **New Application**, nommer l'application **Le Concierge**.

| Où | Valeur | Emplacement YAML |
| --- | --- | --- |
| General Information | Application ID | `DISCORD_CLIENT_ID` |
| General Information | Public Key | `DISCORD_PUBLIC_KEY` |
| OAuth2 | Client Secret | `api.environment.DISCORD_CLIENT_SECRET` |
| Discord → Paramètres utilisateur → Avancés → Mode développeur, puis clic droit sur le serveur → Copier l'identifiant | Server/Guild ID | `DISCORD_GUILD_ID` |
| Shell du NAS : `openssl rand -hex 32` | Secret interne aléatoire | `BOT_API_SECRET` |

Dans [truenas-discord.yaml](../deploy/truenas-discord.yaml), le bloc `x-discord` partage les valeurs entre API et bot. Le **Client Secret OAuth2** et le **token du bot** sont deux secrets différents. Le processus bot utilise `DISCORD_BOT_TOKEN` ou un fichier secret `DISCORD_BOT_TOKEN_FILE` pour mettre à jour automatiquement les commandes à chaque démarrage. Le processus API utilise aussi ce token pour les notifications Discord ; voir le [guide des notifications](discord-reminders.md).

Dans **OAuth2 → Redirects**, ajouter exactement :

```text
https://hub.hugoloiseau.fr/api/v1/auth/discord/callback
```

Adapter si tu choisis un autre domaine. Le Hub demande lui-même les portées `identify` et `guilds.members.read` lors de la connexion utilisateur ; elles ne sont pas à ajouter au lien d'installation du bot.

## 2. Déployer le YAML complet et Caddy

Suivre le [guide TrueNAS](truenas-quickstart.md), en utilisant le YAML complet. Si le Hub est déjà installé, **éditer la même application** pour remplacer son YAML ; conserver le dataset et les valeurs existantes.

Vérifier que le Hub répond en HTTPS et que Caddy relaie `/interactions` vers le port LAN `18081` du bot.

## 3. Déclarer l'endpoint d'interactions

Dans **General Information → Interactions Endpoint URL**, saisir :

```text
https://concierge.hugoloiseau.fr/interactions
```

Enregistrer. Discord envoie un PING signé : la sauvegarde doit réussir. Si elle échoue, contrôler DNS/HTTPS, IP/port Caddy, bot sain et Public Key correspondant à la bonne application. Un navigateur qui ouvre cette URL en GET peut recevoir 404 : Discord utilise POST.

## 4. Installer dans ton serveur

Dans **Installation**, activer **Guild Install**. Le Concierge ne gère pas l'installation personnelle pour cette version. Dans **Default Install Settings → Guild Install**, sélectionner les scopes **`applications.commands`** et **`bot`**. Ne pas donner la permission Administrateur ; les réponses actuelles passent par les interactions.

Copier **Install Link**, l'ouvrir, choisir **Ajouter à un serveur**, puis ton serveur. Le compte qui installe doit pouvoir gérer ce serveur. Dans les paramètres d'intégration du serveur, autoriser les commandes aux rôles/salons voulus ; les membres doivent pouvoir utiliser les commandes d'application dans le salon.

## 5. Enregistrer les commandes

Dans **Bot → Reset Token**, récupérer le token dans ton gestionnaire de mots de passe, puis configurer `DISCORD_BOT_TOKEN` ou `DISCORD_BOT_TOKEN_FILE` dans le service **bot**. Au démarrage, il enregistre automatiquement le catalogue de sa version dans le serveur configuré. Les mises à jour d’image ajoutent donc les nouvelles commandes sans commande manuelle supplémentaire.

Les logs indiquent `Discord guild commands updated` si l’enregistrement réussit. Une panne Discord ou un token refusé produit `Discord command registration failed` sans arrêter les interactions existantes. Après correction, redémarrer le bot ou relancer l’enregistrement explicite. Sans token configuré, le bot signale `Discord command registration skipped` et garde les commandes déjà installées.

Pour un enregistrement ponctuel, sans conserver le token dans le service, le **Shell du NAS** peut toujours lancer le script fourni depuis le dossier où il a été copié :

```bash
sudo bash truenas-admin.sh register
```

Coller le token dans la saisie masquée. Le script l'injecte seulement dans le processus d'enregistrement, sans l'écrire dans le YAML ni l'ajouter à la ligne de commande. Ne pas le coller dans un salon Discord.

L’enregistrement automatique ou explicite remplace les commandes de **cette application dans ce serveur** par `/aide`, `/mes-demandes`, `/a-voir`, `/nouveautes`, `/surprise`, `/recap`, `/livres`, `/demander-livre` et `/diagnostic` : utiliser l'application dédiée créée à l'étape 1. Le répéter ne crée pas de doublons. Avec le mode ponctuel sans token dans le service, relancer le script après une mise à jour des commandes.

`/mes-demandes` reprend les demandes Seerr de la personne connectée et renvoie vers leurs notifications dans le Hub. Il n’y a rien à recopier. `/a-voir` reprend **Avant leur départ**, avec vos états de visionnage et les dates de retrait estimées ; cette commande reste privée. Les anciens noms `/mes-suivis` et `/suivre` ne sont plus publiés dans le catalogue de commandes ; les interactions déjà enregistrées restent reconnues pendant la mise à jour.

`/diagnostic` est réservé aux administrateurs Discord ; son accès exige aussi une présence dans `ADMIN_DISCORD_IDS` côté API. Le bot lui-même n’a pas besoin de la permission Administrateur. Les clés Radarr/Sonarr facultatives et les limites de cette commande sont détaillées dans le [guide de diagnostic](diagnostics.md).

## 6. Tester

Taper `/aide` dans un salon autorisé. La réponse doit être visible uniquement par toi. Ensuite ouvrir le Hub, se connecter avec Plex puis ouvrir **Mon compte → Lier Discord** : le compte Seerr est associé automatiquement avec cette identité. Si nécessaire, se connecter une première fois à Seerr avec le même Plex puis actualiser l’association dans le Hub. Tester `/mes-demandes` et `/nouveautes`.

Le bot peut apparaître **hors ligne** dans la liste des membres : il n'ouvre pas de connexion Gateway pour afficher une présence. Ce sont les commandes qui vérifient son fonctionnement. Les réponses personnelles sont éphémères dans le salon. Les messages privés des [suivis](subscriptions.md) et des [rappels](removal-reminders.md) exigent une configuration et un consentement explicites.

Pour partager un résultat, tester `/nouveautes public:true`, `/surprise public:true` ou `/recap public:true`. Le résultat paraît dans le salon sans lien Plex personnel, mention ni bouton ; la confirmation reste privée. Sans `public:true`, rien n’est partagé. Les erreurs restent privées, tout comme les commandes personnelles et administrateur. Voir [le partage des résultats](discord-discovery.md#partager-dans-le-salon).

Avec plus de dix demandes Seerr, `/mes-demandes` propose **Suivant**, puis **Précédent** pour revenir en arrière. Les pages remplacent le même message, toujours visible uniquement par toi. Tester aussi la dernière page et une nouvelle commande après déconnexion du Hub : elle doit inviter à se reconnecter. Si une page devient vide après suppression de demandes dans Seerr, le bouton précédent permet de revenir. En cas de panne, les boutons sont retirés ; relancer `/mes-demandes` après rétablissement.

`/nouveautes` affiche maintenant les titres, dates et liens vers Plex, avec **Suivant** puis **Début** pour revenir à la première page. Les épisodes d’une même série sont regroupés à l’intérieur de chaque page : six entrées Plex source peuvent donc donner moins de six cartes. La suite utilise le curseur réel de Plex et s’arrête à l’offset maximal 10 000 ; cette sélection n’est pas un inventaire exhaustif. **Début** revient au début, sans prétendre reconstituer une page précédente de taille variable. Tester une page courte, la dernière page et une perte d’accès pendant la navigation. Une panne retire les résultats et les boutons ; relancer la commande après rétablissement.

Mettre à jour **API et bot** avec la même version. Avec le token configuré dans le bot, son démarrage met à jour les commandes. Le [guide de déploiement](deployment.md#sauvegarde-et-retour-arrière) précise le retour arrière.

Si les commandes n'apparaissent pas : vérifier le serveur configuré, le token et le résultat de l’enregistrement dans les logs du bot, les droits d'intégration, puis recharger Discord. Si elles invitent à se reconnecter, ouvrir le Hub : les autorisations expirent au plus tard après 12 h et sont perdues au redémarrage.

En cas de `Discord reply rejected` avec statut 404 sur une ancienne image, mettre à jour **API et bot**. `/aide` répond directement sans webhook. Pour les commandes personnelles, le bot attend désormais la confirmation du callback initial avant de modifier le message ; les erreurs incluent le code Discord (`discord_code`) sans les tokens. Un 404 isolé ne permet pas de distinguer un webhook inconnu d’un message absent : ce code précis permet de poursuivre le diagnostic.

Référence officielle : [Getting Started Discord](https://docs.discord.com/developers/quick-start/getting-started), [Receiving and Responding](https://docs.discord.com/developers/interactions/receiving-and-responding).
