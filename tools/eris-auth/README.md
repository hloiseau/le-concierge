# Connexion personnelle d'Eris à ChatGPT

Ce service accompagne le profil `v2` du bot sur le NAS. Il utilise le code OAuth du devkit officiel OpenAI, figé au commit `0a36fefeb913055c8c7a1a29b63d82b96b2e841a`. Les notices du devkit sont conservées dans l'image. Sa licence non commerciale convient à cette expérimentation personnelle ; une distribution commerciale demanderait une autre vérification.

Construire avec `docker build -t eris-auth:local tools/eris-auth`. Le conteneur s'exécute sous UID/GID 65532, avec racine en lecture seule, sans capacité Linux ni port publié. Monter un dossier persistant privé en écriture sur `/state` (propriétaire 65532, mode 0700, sans ACL additionnelle).

`node /app/keeper.mjs login` ouvre un parcours PKCE avec contrôle de l'état, du nonce, de l'émetteur, de l'audience et de la signature de l'identité par le SDK. Le rappel écoute uniquement sur `127.0.0.1:14551`. Pour une connexion sur le NAS, exécuter temporairement ce conteneur avec le réseau hôte et faire parvenir le retour du navigateur à cette adresse locale. Ne pas publier ce port sur le LAN ou Internet. Le service et la connexion interactive utilisent le même verrou ; arrêter le service avant une reconnexion.

Le mode `serve` renouvelle l'autorisation avant expiration, respecte `earliest_refresh_at` et sauvegarde les rotations avant leur vérification d'identité. Une rotation non vérifiée n'est jamais remise au bot. Les fichiers sont remplacés atomiquement avec mode 0600. Le dossier `/state` contient les identifiants et doit rester privé, y compris dans les sauvegardes : les fichiers sont protégés par les permissions du système, sans chiffrement applicatif. Aucune session Codex de développement n'est copiée.

Monter **uniquement `/state/tokens` en lecture seule** dans le bot, sur `/run/eris-auth`. Eris relit `access_token` à chaque requête. Les identifiants de renouvellement, l'identité et l'inscription de l'application restent hors du conteneur du bot. Une révocation impose une nouvelle connexion ; aucun basculement vers une clé API payante n'est prévu.

Le mode `health` vérifie localement que le jeton publié correspond à une connexion encore valide. Les tests ne contactent pas OpenAI : `node --test tools/eris-auth/keeper.test.mjs`. L'éligibilité du compte et l'inférence Luna doivent être validées par un appel réel avant de modifier le bot en service.

Documentation officielle : [connexion](https://developers.openai.com/siwc/token-sharing-open-source/sign-in), [sessions et renouvellement](https://developers.openai.com/siwc/token-sharing-open-source/profiles-and-sessions), [machines personnelles](https://developers.openai.com/siwc/token-sharing-open-source/self-hosted-vms).
