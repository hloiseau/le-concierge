# Eris : refonte de la voix déployée — 27 octobre 2026

Déploiement demandé par Hugo pour tester dans Discord. La refonte du [contexte et de la voix](../evals/eris/context-refactor.md) est active sur l'application TrueNAS `media-hub`.

- Code API et bot : `d4826afd79cea142186e1f450c2b8ed4a88dcdfe`, branche `fix/eris-voice-context`.
- Image locale : `eris-concierge:voice-d4826af`.
- Image ID : `sha256:55caa3a06ca7664c8756d2606e81749d8a3d9e126cd460fd60df9f507e340903`.
- Base conservée : image GHCR immuable `sha256:f92961ae4f92cd223ba2c65b38d78cf5a06580b034fd89e4c7e82fa9b36b68dd`, code `13ffb1bed93042a9c1cbaced8de28828b59862c1`.
- Modèle : `gpt-6.1-sol`, profil v2, protocole Responses, effort `low`.

Les deux exécutables Linux amd64 sont compilés depuis le commit propre, transférés avec leurs empreintes SHA-256, puis copiés dans l'image de base. La configuration héritée de l'image est vérifiée, de même que ses couches de base. Le manifeste et l'archive du code exact restent sur le NAS dans `/mnt/Main/tank/concierge/eris-voice-d4826af`.

La modification Compose porte sur les images API/bot et leur `pull_policy: never`, nécessaire à l'image locale. Les autres services, variables d'environnement, volumes et réglages sont conservés. Les mémoires et historiques existants restent disponibles.

## Vérifications après bascule

Les cinq services de l'application sont sains. API et bot utilisent la nouvelle image ; les images du Hub, du moteur local et de l'authentification sont identiques à celles d'avant la mise à jour. Les contrôles internes `--healthcheck` passent. Les événements de démarrage confirment la mise à jour des commandes de guilde et la connexion à la Gateway Discord ; seuls ces indicateurs ont été retournés, sans contenu de journaux.

Les deux bases SQLite passent `PRAGMA quick_check`. La configuration relue correspond exactement à la modification prévue. Aucun message Discord de test ni action média n'a été envoyé par l'agent. Hugo réalise la recette du ton directement dans Discord.

## Sauvegarde et retour arrière

Sauvegarde privée cohérente, créée avant la bascule :

`/mnt/Main/tank/concierge/ops-backups/before-eris-voice-20261027T223317Z`

Elle contient `config.json`, `main.db` et `character.db`. Le dossier est en mode 700 et les fichiers en 600, ACL héritées retirées. Ces fichiers contiennent des données privées et restent sur le NAS.

Restaurer `config.json` avec `app.update` rétablit la configuration et les images précédentes tout en conservant les données récentes. Le retour automatique prévu en cas d'échec n'a pas été nécessaire.
