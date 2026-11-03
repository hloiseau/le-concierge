# Eris sur un deuxième serveur, uniquement pour discuter

La même application Discord et le même processus bot peuvent servir le serveur
principal et des serveurs supplémentaires. Le moteur et le token restent identiques.

Le serveur principal reste celui de `DISCORD_GUILD_ID`. Les serveurs supplémentaires
sont autorisés explicitement par `DISCORD_CHAT_GUILD_IDS`, dans le service **bot** :

```yaml
DISCORD_GUILD_ID: 'ID_DU_SERVEUR_PRINCIPAL'
DISCORD_CHAT_GUILD_IDS: 'ID_DU_DEUXIEME_SERVEUR'
```

Pour plusieurs serveurs supplémentaires, séparer les IDs par des virgules, sans
répéter le serveur principal (dix serveurs supplémentaires maximum). Une valeur
vide conserve le fonctionnement sur le seul serveur principal.

## Installation

1. Dans Discord, activer **Paramètres utilisateur → Avancés → Mode développeur**,
   puis clic droit sur le deuxième serveur → **Copier l'identifiant du serveur**.
2. Dans le Developer Portal, utiliser le lien d'installation de l'application
   existante, avec **Guild Install**, `bot` et `applications.commands`. Choisir
   le deuxième serveur depuis un compte ayant **Gérer le serveur**.
3. Autoriser Eris à voir les salons voulus, envoyer des messages et lire leur
   historique ; ajouter l'envoi dans les fils si nécessaire. Les images et
   aperçus utilisent les permissions habituelles du bot.
4. Déployer une image qui contient cette évolution, ajouter la variable au
   service **bot** dans le YAML TrueNAS existant, puis recréer ce service.
   Ne pas remplacer l'ID du serveur principal, changer le token ou lancer un
   deuxième bot avec le même token.
5. Le démarrage enregistre les commandes dans chaque serveur configuré.
   En cas d'échec, vérifier que l'application est invitée, puis redémarrer le
   bot ou lancer `/app/bot --register` dans son conteneur.

Sur un serveur supplémentaire, les seules commandes publiées sont `/eris`,
`/eris-salon` et une `/aide` spécifique à la discussion. Les commandes média
et tous les boutons d'actions sont refusés, même s'ils étaient encore affichés
dans Discord. Le modèle conserve la personnalité, le contexte Discord, le silence
et la mémoire configurés, mais aucun appel aux outils du Hub ou à une action
externe n'est autorisé. Cela inclut les recherches web du Hub.

La participation spontanée reste désactivée par défaut dans chaque salon.
`/eris-salon participation:activer` nécessite un administrateur, le profil v2,
la mémoire persistante et Message Content Intent, comme sur le premier serveur.
Une mention `@Eris Salut` n'exige pas d'activer cette participation.

Les conversations et souvenirs sont séparés par serveur, puis par salon pour
les échanges publics ou par membre pour `/eris`. L'oubli est limité au serveur
où la commande est exécutée. L'API média, les rappels, les rôles d'intérêts et
la liaison de compte conservent le serveur principal.

Retirer un ID de la variable puis redémarrer refuse immédiatement les messages
et interactions de ce serveur ; les anciennes commandes peuvent rester affichées
dans Discord. Retirer également l'application du serveur pour les supprimer.

## Vérification après déploiement

- Sur le deuxième serveur : `@Eris Salut`, une réponse à son message, puis
  `/eris message:Salut` (réponse privée).
- `/aide` ne propose aucune commande média ni lien de compte.
- Une demande de film dans Seerr ne déclenche aucune action.
- Le même membre ne retrouve pas les souvenirs de son autre serveur.
- `/eris oublier:true` efface ses souvenirs ici sans supprimer ceux du premier.
- Sur le premier serveur, vérifier la discussion et une commande média habituelle.

Les tests automatisés utilisent des services simulés et ne constituent pas un
test Discord réel ni une validation de la version installée sur le NAS.
