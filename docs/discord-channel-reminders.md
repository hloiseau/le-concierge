# Rappels dans le salon notifications

Le Concierge mentionne les **demandeurs originaux de Seerr** dans un salon texte Discord quand Maintainerr prévoit le retrait du film ou de la série. Les membres n'ont pas besoin de saisir un identifiant TMDB, de créer un suivi, de se connecter au Hub ni d'activer les messages privés pour ce rappel collectif.

Dans l'environnement du service **API**, renseigner :

```dotenv
DISCORD_GUILD_ID=identifiant_du_serveur
DISCORD_NOTIFICATION_CHANNEL_ID=identifiant_du_salon_notifications
DISCORD_BOT_TOKEN_FILE=/run/secrets/discord_bot_token
```

Le fichier doit être monté en lecture seule et contenir le token du bot, pas le secret OAuth. `DISCORD_BOT_TOKEN` est l'alternative ; ne pas définir les deux. Le bot doit voir le salon et pouvoir y envoyer des messages. Le transport vérifie l'identifiant du serveur et le type du salon avant chaque envoi. Aucun message de test n'est envoyé au démarrage.

Si aucun token du bot n'est fourni à l'API, les annonces du salon et les DM restent désactivés avec un avertissement dans les logs ; le Hub peut démarrer. Ajouter ensuite le token dans l'environnement du service **API**, puis redémarrer pour activer les fonctions demandées. Un token fourni uniquement au conteneur bot n'est pas accessible à l'API. Les valeurs invalides et les fichiers de secrets illisibles restent des erreurs explicites.

Sur les versions antérieures à ce correctif, le log `DISCORD_DMS_ENABLED requires a boolean and a bot token when enabled` arrête l'API. Pour rétablir provisoirement le Hub, remettre `DISCORD_DMS_ENABLED: 'false'` et `DISCORD_NOTIFICATION_CHANNEL_ID: ''` dans le service API, puis redéployer. Cela désactive les rappels Discord sans modifier la base. Renseigner le token avant de réactiver les deux réglages.

Configurer aussi `PLEX_URL`, `PLEX_SERVER_ID`, `MAINTAINERR_URL`, `SEERR_URL` et `SEERR_API_KEY` (lecture des demandes et des paramètres utilisateurs). Pour les retraits de saisons et d'épisodes, `TAUTULLI_URL` et `TAUTULLI_API_KEY` permettent de retrouver la série et le numéro de saison depuis les métadonnées Plex. Sans Tautulli, seuls les retraits de films et séries entières sont couverts.

L'identité Discord vient des paramètres de notification du demandeur dans Seerr (`discordIds`, ou l'ancien `discordId`). Si ces paramètres sont vides, Le Concierge utilise une association Plex–Discord déjà vérifiée dans le Hub. Un simple pseudo Seerr ne permet pas de déduire un compte Discord : sans association, aucun utilisateur n'est mentionné pour cette demande. Les noms et titres provenant des services ne peuvent pas injecter `@everyone`, un rôle ou une autre mention.

Les rappels portent sur les demandes en attente, acceptées ou terminées dont la clé Plex et la qualité HD/4K correspondent au contenu menacé. L'identifiant du serveur Plex de Seerr doit correspondre au serveur configuré dans Le Concierge. Les demandes refusées/échouées, règles inactives, exclusions et dates incertaines sont ignorées. Une saison ne concerne que les personnes qui ont demandé cette saison. Le message donne la date **estimée** de retrait, sans révéler un historique de visionnage personnel. Un demandeur supprimé ou une métadonnée périmée n'empêche pas de traiter les autres contenus valides.

Deux jalons sont utilisés : à moins de sept jours, puis à moins de deux jours du retrait. Envoi entre 8 h et 22 h, heure de Paris, dix contenus au maximum par jour dans le salon. Plusieurs demandeurs d'un même contenu sont regroupés dans le même message. Une clé durable par contenu, règle, échéance et jalon empêche les doublons après redémarrage ; les erreurs temporaires sont réessayées cinq fois au maximum avec le même nonce Discord. Une panne de source n'autorise jamais l'envoi à partir d'un ancien titre en base.

Les messages privés restent séparés : `DISCORD_DMS_ENABLED=true`, liaison Discord et choix explicite dans **Mon compte**. Cocher les messages privés active aussi les rappels personnels dans Le Hub ; le salon collectif ne dépend pas de ce choix. Une case grisée indique soit un bot DM non configuré, soit une association Discord manquante.

Contrats utilisés : [paramètres utilisateurs Seerr](https://github.com/seerr-team/seerr/blob/develop/server/routes/user/usersettings.ts), [métadonnées Tautulli 2.18.2](https://github.com/Tautulli/Tautulli/blob/v2.18.2/plexpy/pmsconnect.py). SQLite passe au schéma 6 pour conserver uniquement les clés de déduplication et les états de livraison du salon, sans token ni historique de visionnage.
