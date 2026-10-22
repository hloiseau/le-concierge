# Autorisation Plex et rappels autonomes

Depuis le contrat 0.10, `PLEX_GRANT_KEY` ou `PLEX_GRANT_KEY_FILE` active la conservation chiffrée des autorisations Plex personnelles. Sans clé, le fonctionnement reste temporaire : douze heures au maximum et perte au redémarrage.

Avec une clé, chaque nouvelle connexion Plex enregistre l’autorisation de ce compte. Les sessions existantes ne sont pas migrées : une première reconnexion est nécessaire. Les préférences de notifications et le consentement aux DM ne changent pas.

La fermeture du navigateur, l’expiration des douze heures et le redémarrage du Concierge n’arrêtent plus les rappels. La session web reste temporaire et n’est jamais restaurée. Les commandes personnelles du bot utilisent la même autorisation, toujours avec l’identité Discord liée. Aucun compte administrateur partagé ne remplace le compte personnel.

## Conservation et révocation

SQLite v8 conserve seulement le jeton de compte Plex, l’identité Plex et l’identifiant client du flux PIN, chiffrés par AES-256-GCM avec nonce aléatoire. Le chiffrement authentifie aussi le propriétaire local, son identité Plex, le serveur Plex et l’origine du Hub. Le jeton de ressource du serveur et les résultats de vérification ne sont pas enregistrés.

Avant toute utilisation après redémarrage, le Concierge vérifie auprès de Plex l’identité, l’absence de restriction et le partage du serveur. Cette vérification est renouvelée au plus toutes les cinq minutes. Une panne amont suspend les traitements personnels ; elle n’autorise pas une lecture à partir d’anciennes données. Un refus ou un changement d’identité retire l’autorisation. Les preuves de visionnage et règles de suppression restent vérifiées avant les notifications, y compris les annonces du salon.

« Déconnexion » efface immédiatement l’autorisation locale, en mémoire et en base, puis ferme la session. La suppression du compte local l’efface aussi. Retirer l’accès de l’application dans Plex arrête les traitements à la prochaine vérification ; la dernière vérification positive n’est réutilisée que pendant cinq minutes. Une erreur de stockage pendant la déconnexion est signalée : réessayer, car l’effacement n’est pas confirmé.

Les identifiants Grimmory, cookies Shelfmark et autorisations Downloadarr restent temporaires et ne sont pas enregistrés avec l’autorisation Plex.

## Installation et restauration

1. Sauvegarder SQLite avant la migration v8. Les versions précédentes nécessitent la sauvegarde de leur propre schéma pour un retour arrière.
2. Générer une clé dédiée de **32 octets aléatoires**, encodée en **64 caractères hexadécimaux**, dans un fichier secret hors du dataset SQLite. Ne pas réutiliser un jeton Plex, une clé Tautulli ou le secret API/bot.
3. Monter ce fichier en lecture seule dans le service API, accessible à son UID 65532, puis définir `PLEX_GRANT_KEY_FILE=/run/secrets/plex_grant_key`. Ne pas définir simultanément `PLEX_GRANT_KEY`. Le Hub et le bot n’ont pas besoin de cette clé.
4. Conserver cette clé lors de chaque mise à jour et la sauvegarder séparément de la base, avec un accès restreint. Ne pas l’afficher dans les journaux ou les arguments de commande.
5. Redéployer puis reconnecter chaque compte une fois. Le Hub affiche que les rappels fonctionnent en arrière-plan et explique comment les arrêter.

Le démarrage est refusé si des autorisations enregistrées ne peuvent pas être déchiffrées : clé absente ou différente, données modifiées, changement de serveur ou d’origine. Restaurer la clé, la configuration et la base correspondantes. Une remise à zéro volontaire exige d’arrêter l’API, de sauvegarder puis de vider uniquement `plex_authorizations` avant de changer la clé ; tous les comptes devront se reconnecter. Ne pas remplacer automatiquement la clé à chaque déploiement.

Les tests utilisent des comptes et transports simulés : migration depuis v7, réouverture SQLite, expiration du navigateur, reprise des rappels privés et du salon, contenu terminé, séparation des propriétaires, clé incorrecte, données altérées, révocation et course avec une nouvelle connexion. La reprise sur le NAS a été vérifiée le 20 octobre après reconnexion de deux comptes et redémarrage réel de l’API : autorisations conservées, sessions web invalidées, déconnexion isolée entre comptes. La réception réelle des messages consentis reste à valider ; aucune attente réelle de douze heures ni livraison différée n’a été observée par cette recette. Voir le [bilan daté](status-2026-10-22.md).
