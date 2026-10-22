# Eris v2 sur le NAS — 22 octobre 2026

Déploiement demandé par Hugo dans l'application TrueNAS existante `media-hub`. Le bot est sain et sa connexion Discord a été confirmée après démarrage. Aucune commande ou publication Discord de test n'a été envoyée par l'agent.

## Version active

- Bot : `eris-bot:luna-7228468f4704`, code `7228468f4704cd30f0269030a0cdb213dae790e9`, image `sha256:2a081f01eaa0130449f277f30e5c9bb66abbe22079c48ce6a80e9f550eb0b928`.
- Authentification : `eris-auth:siwc-0a36fefe`, image `sha256:2776132d9310f1ebbc06a2cab2c113f704303a5524ab5895a32c476777a7eace`, service issu du commit `696b3d2`.
- Paramètres : `ERIS_PROFILE=v2`, `ERIS_PROTOCOL=responses`, `ERIS_MODEL=gpt-6-luna`, `ERIS_API_URL=https://api.openai.com/v1`.
- Mémoire durable : `/mnt/Main/tank/concierge/eris-character/character.db`, montée sur `/app/eris-data/character.db`.
- Connexion privée : `/mnt/Main/tank/concierge-secrets/eris-siwc`, accessible uniquement à son propriétaire et aux administrateurs. Seul son sous-dossier `tokens` est monté en lecture seule dans le bot.

Cette image de test remplace uniquement le binaire du bot dans l'image publiée précédente. Le binaire Linux compilé sous Windows est copié avec `--chown=65532:65532 --chmod=0555`. La branche reste une PR de travail : ce déploiement ne constitue pas une fusion dans `main`.

Les versions de l'API, du Hub et du moteur local n'ont pas changé. TrueNAS recrée toutefois les conteneurs du groupe pendant une mise à jour. Le moteur local est conservé pour permettre un retour arrière ; le bot v2 utilise désormais Luna.

## Vérifications

L'appel réel utilise le code Go d'Eris, son parseur Responses et une base SQLite de test sur le NAS. Deux demandes synthétiques ont réussi : une salutation en français (2,13 secondes pour cet appel) puis le choix de `search` pour « Alien sur Plex ». Le résultat catalogue de ce deuxième test est simulé ; aucun appel Plex/Seerr ni envoi Discord n'a eu lieu. Ce temps est une mesure unique, pas une garantie de latence.

Le flux réel du forfait fournit parfois un `response.completed` avec `output:[]` ; le texte finalisé vient alors de `response.output_item.done`. La correction est couverte par des tests d'interruption, de doublon, d'élément manquant, de refus et d'outil inattendu. Les 645 tests ciblés Go passent. Les six tests d'authentification passent localement, en CI et dans l'image Linux sur le NAS. Le renouvellement périodique n'a pas encore atteint son échéance en conditions réelles.

Tous les conteneurs de l'application sont sains, le bot est connecté à Discord, la base de personnage existe et la clé d'autorisation Plex est inchangée. Le serveur de rappel OAuth était temporaire et son port local a été fermé après connexion.

La première tentative de bascule a rencontré un binaire sans droit d'exécution ; le retour automatique à l'ancienne configuration a réussi. La seconde tentative a réussi après correction et vérification de l'exécutable.

## Retour arrière

Configuration précédente sauvegardée sur le NAS, avec mode 0600 dans un dossier privé :

`/mnt/Main/tank/concierge/ops-backups/before-eris-luna-20261022T213657Z.json`

Pour la restaurer, transmettre son contenu à `app.update('media-hub', {'custom_compose_config': configuration})` avec le client TrueNAS, puis vérifier les contrôles de santé et la reconnexion Discord. La sauvegarde contient des secrets : ne pas l'afficher, la joindre à une issue ou la copier dans Git. La mémoire et la connexion ChatGPT restent sur disque lors du retour arrière ; aucune suppression automatique de ces données n'est prévue.

## Recette humaine restante

Mentionner Eris pour discuter, répondre à l'un de ses messages avec un autre membre, revenir sur une lubie puis lui demander de la laisser tomber. `/eris oublier:true` permet de tester l'oubli. Juger surtout le naturel, la variété et l'intensité du personnage : la validation technique ne remplace pas ce retour.
