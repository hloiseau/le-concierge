# Brancher Le Concierge à Discord

Une application Discord dédiée sert à la fois à la connexion du Hub et aux trois commandes du bot. Le bot reçoit des interactions HTTP : aucun Gateway ni privileged intent n'est nécessaire pour cette première version.

## 1. Créer l'application et récupérer les valeurs

Ouvrir le [Discord Developer Portal](https://discord.com/developers/applications), **New Application**, nommer l'application **Le Concierge**.

| Où | Valeur | Emplacement YAML |
| --- | --- | --- |
| General Information | Application ID | `DISCORD_CLIENT_ID` |
| General Information | Public Key | `DISCORD_PUBLIC_KEY` |
| OAuth2 | Client Secret | `api.environment.DISCORD_CLIENT_SECRET` |
| Discord → Paramètres utilisateur → Avancés → Mode développeur, puis clic droit sur le serveur → Copier l'identifiant | Server/Guild ID | `DISCORD_GUILD_ID` |
| Shell du NAS : `openssl rand -hex 32` | Secret interne aléatoire | `BOT_API_SECRET` |

Dans [truenas-discord.yaml](../deploy/truenas-discord.yaml), le bloc `x-discord` partage les valeurs entre API et bot. Le **Client Secret OAuth2** et le **token du bot** sont deux secrets différents. Le token du bot n'est pas nécessaire dans le YAML.

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

## 5. Enregistrer les trois commandes

Dans **Bot → Reset Token**, récupérer le token dans ton gestionnaire de mots de passe. Dans le **Shell du NAS**, lancer le script fourni, depuis le dossier où tu l'as copié :

```bash
sudo bash truenas-admin.sh register
```

Coller le token dans la saisie masquée. Le script l'injecte seulement dans le processus d'enregistrement, sans l'écrire dans le YAML ni l'ajouter à la ligne de commande. Ne pas le coller dans un salon Discord.

Cette opération remplace les commandes de **cette application dans ce serveur** par `/aide`, `/mes-demandes` et `/nouveautes` : utiliser l'application dédiée créée à l'étape 1. On peut relancer la commande sans créer de doublons.

## 6. Tester

Taper `/aide` dans un salon autorisé. La réponse doit être visible uniquement par toi. Ensuite ouvrir le Hub, se connecter avec Discord, lier Plex : le compte Seerr est associé automatiquement avec cette identité. Si nécessaire, se connecter une première fois à Seerr avec le même Plex puis actualiser l’association dans le Hub. Tester `/mes-demandes` et `/nouveautes`.

Le bot peut apparaître **hors ligne** dans la liste des membres : il n'ouvre pas de connexion Gateway pour afficher une présence. Ce sont les commandes qui vérifient son fonctionnement. Les réponses personnelles sont éphémères dans le salon ; les notifications proactives par DM ne sont pas encore implémentées.

Si les commandes n'apparaissent pas : vérifier le serveur configuré, le résultat de `register`, les droits d'intégration, puis recharger Discord. Si elles invitent à se reconnecter, ouvrir le Hub : les autorisations expirent au plus tard après 12 h et sont perdues au redémarrage.

En cas de `Discord reply rejected` avec statut 404 sur une ancienne image, mettre à jour **API et bot**. `/aide` répond directement sans webhook. Pour les commandes personnelles, le bot attend désormais la confirmation du callback initial avant de modifier le message ; les erreurs incluent le code Discord (`discord_code`) sans les tokens. Un 404 isolé ne permet pas de distinguer un webhook inconnu d’un message absent : ce code précis permet de poursuivre le diagnostic.

Référence officielle : [Getting Started Discord](https://docs.discord.com/developers/quick-start/getting-started), [Receiving and Responding](https://docs.discord.com/developers/interactions/receiving-and-responding).
