# Rappels dans le salon notifications

Le Concierge mentionne les **demandeurs originaux de Seerr qui ont encore le contenu à voir** quand Maintainerr prévoit son retrait. Les membres n'ont pas besoin de créer un suivi ni d'activer les messages privés. Une autorisation Plex personnelle est nécessaire pour vérifier le visionnage de chaque demandeur. Avec la [conservation chiffrée](persistent-plex-authorization.md), elle survit à l’expiration de la session web et au redémarrage de l’API. La déconnexion explicite, la suppression du compte ou une révocation Plex suspend ses mentions jusqu’à une nouvelle connexion.

Dans l'environnement du service **API**, renseigner :

```dotenv
DISCORD_GUILD_ID=identifiant_du_serveur
DISCORD_NOTIFICATION_CHANNEL_ID=identifiant_du_salon_notifications
DISCORD_BOT_TOKEN_FILE=/run/secrets/discord_bot_token
```

Le fichier doit être monté en lecture seule et contenir le token du bot, pas le secret OAuth. `DISCORD_BOT_TOKEN` est l'alternative ; ne pas définir les deux. Le bot doit voir le salon et pouvoir y envoyer des messages. Le transport vérifie l'identifiant du serveur et le type du salon avant chaque envoi. Aucun message de test n'est envoyé au démarrage.

Si aucun token du bot n'est fourni à l'API, les annonces du salon et les DM restent désactivés avec un avertissement dans les logs ; le Hub peut démarrer. Ajouter ensuite le token dans l'environnement du service **API**, puis redémarrer pour activer les fonctions demandées. Un token fourni uniquement au conteneur bot n'est pas accessible à l'API. Les valeurs invalides et les fichiers de secrets illisibles restent des erreurs explicites.

Sur les versions antérieures à ce correctif, le log `DISCORD_DMS_ENABLED requires a boolean and a bot token when enabled` arrête l'API. Pour rétablir provisoirement le Hub, remettre `DISCORD_DMS_ENABLED: 'false'` et `DISCORD_NOTIFICATION_CHANNEL_ID: ''` dans le service API, puis redéployer. Cela désactive les rappels Discord sans modifier la base. Renseigner le token avant de réactiver les deux réglages.

Configurer aussi `PLEX_URL`, `PLEX_SERVER_ID`, `MAINTAINERR_URL`, `SEERR_URL`, `SEERR_API_KEY`, `TAUTULLI_URL` et `TAUTULLI_API_KEY`. Plex confirme les contenus non vus avec le jeton personnel du demandeur ; Tautulli permet d'écarter un contenu déjà terminé, même en cas de revisionnage. L'absence d'historique seule ne suffit jamais. Une panne de lecture suspend l'annonce.

Le filtre est individuel : si un demandeur a terminé un film et un autre non, seul le second est mentionné. Une série ou saison reste éligible uniquement si elle contient un épisode non terminé appartenant à une saison demandée et non exclue par Maintainerr. Aucun détail de l'historique n'est publié. Les observations personnelles sont conservées seulement en mémoire pendant le passage, puis relues avant l'envoi.

L'identité Discord vient des paramètres de notification du demandeur dans Seerr (`discordIds`, ou l'ancien `discordId`). Si ces paramètres sont vides, Le Concierge utilise une association Plex–Discord déjà vérifiée dans le Hub. Un simple pseudo Seerr ne permet pas de déduire un compte Discord : sans association, aucun utilisateur n'est mentionné pour cette demande. Les noms et titres provenant des services ne peuvent pas injecter `@everyone`, un rôle ou une autre mention.

Les rappels portent sur les demandes en attente, acceptées ou terminées dont la clé Plex et la qualité HD/4K correspondent au contenu menacé. L'identifiant du serveur Plex de Seerr doit correspondre au serveur configuré dans Le Concierge. Les demandes refusées/échouées, règles inactives, exclusions et dates incertaines sont ignorées. Une saison ne concerne que les personnes qui ont demandé cette saison. Un retrait d'épisode précise la saison et le numéro d'épisode fourni par Tautulli ; si ce numéro manque, le message indique « un épisode (numéro indisponible) », sans annoncer le départ de toute la saison. Les épisodes restent annoncés séparément. Le message donne la date **estimée** de retrait, sans révéler un historique de visionnage personnel. Un demandeur supprimé ou une métadonnée périmée n'empêche pas de traiter les autres contenus valides.

Deux jalons sont utilisés : à moins de sept jours, puis à moins de deux jours du retrait. Envoi entre 8 h et 22 h, heure de Paris, dix contenus au maximum par jour dans le salon. Plusieurs demandeurs d'un même contenu sont regroupés dans le même message. Une clé durable par contenu, règle, échéance et jalon empêche les doublons après redémarrage. Les erreurs temporaires autorisent cinq tentatives au total, avec des reprises après 1, 2, 4 puis 8 minutes et le même nonce Discord ; un délai supérieur imposé par Discord est respecté. Une panne de source n'autorise jamais l'envoi à partir d'un ancien titre en base.

Les messages privés restent séparés : `DISCORD_DMS_ENABLED=true`, liaison Discord puis activation des notifications personnelles et des messages privés dans **Mon compte → Notifications**. Le salon collectif ne dépend pas de ce consentement aux DM. Une case grisée indique soit des notifications personnelles désactivées, soit un bot DM non configuré, soit une association Discord manquante.

Juste avant le POST Discord, après vérification du salon, le service relit Maintainerr, ses règles, les demandes Seerr, les identités et le visionnage des destinataires. Un contenu désormais terminé, une déconnexion, un retrait reporté ou annulé, une exclusion, une règle désactivée ou un changement de demandeur interrompt l’envoi. Le titre, la portée et la liste des mentions doivent encore correspondre au message préparé ; une erreur temporaire de lecture autorise une reprise, jamais l’envoi de l’ancienne observation. Le cycle est borné à 45 secondes et s’arrête au plus tard à 22 h, même s’il a commencé avant. Ces lectures de services distincts ne constituent pas une transaction commune.

Contrats utilisés : [paramètres utilisateurs Seerr](https://github.com/seerr-team/seerr/blob/develop/server/routes/user/usersettings.ts), [métadonnées Tautulli 2.18.2](https://github.com/Tautulli/Tautulli/blob/v2.18.2/plexpy/pmsconnect.py). SQLite passe au schéma 6 pour conserver uniquement les clés de déduplication et les états de livraison du salon, sans token ni historique de visionnage.
