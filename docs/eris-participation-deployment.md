# Eris : participation en salon — 26 octobre 2026

Relevé historique. Cette image est conservée sur le NAS. Après la récupération
du 27 octobre, l'API et le bot ont été [mis à jour vers le main `13ffb1b`](eris-release-2026-10-27.md),
avec la participation et la mémoire réconciliées. Le déploiement passé décrit
ci-dessous n'autorise pas à restaurer une ancienne base.

Déploiement autorisé par Hugo sur `media-hub`, vérifié le 26 octobre 2026. Les corrections sont intégrées à la version réellement active `89a54e0`, afin de conserver la personnalité, les auteurs Discord, les images et le suivi des livres ajoutés depuis l'autre PC.

- Code : `28f46de166fe4266a90e8ef24c43b24ca542752a`, branche `fix/eris-participation`.
- API et bot : `eris-concierge:participation-28f46de`.
- Image : `sha256:2518757c742059fb1bcee11a963e71e85adabb85be379ed064b0f51ab275c3e1`.
- Modèle conservé : `gpt-6.1-sol`, profil v2, protocole Responses.
- Hub, authentification, moteur local et clé d'autorisation Plex conservés.
- Schéma principal SQLite v9 conservé ; ajout de la table des réglages de salons dans la base de personnage.

La participation spontanée est activée dans **général** et **absolute-cinéma**, les deux salons choisis. Les administrateurs la gèrent désormais avec `/eris-salon participation:activer`, `desactiver` ou `etat`, dans le salon concerné. La commande admin est enregistrée auprès de Discord ; le bot est reconnecté à la Gateway. Tous les conteneurs de l'application sont sains.

Les demandes du membre sont consultables publiquement ; les filtres de visionnage restent privés. Les relances sans mention tolèrent cinq messages intercalés pendant deux minutes. Les interventions spontanées ont un contexte court, un seul appel de décision/réponse, un plafond de cinq minutes par salon et aucun outil média. Une intervention ignorée n'est pas relancée.

## Vérifications

`go test ./...` et `go vet ./...` réussissent sur le code intégré. Le détecteur de courses Windows n'a pas pu démarrer, faute de CGO. Le contrôle synthétique avec le modèle réellement configuré réussit : silence sur un échange banal, identification de Premier Contact à partir d'indices, reconnaissance d'une relance avec message intercalé, routage de Billions vers l'outil d'état. Aucun message Discord de test ni demande média réelle n'a été envoyé par ces contrôles. La pertinence des interventions reste à observer en usage réel.

Les binaires API et bot ont été compilés pour Linux amd64 et leurs empreintes vérifiées avant construction de l'image. Ils remplacent uniquement ces deux exécutables dans l'image de la version `89a54e0`. Le code exact est archivé sur le NAS dans le dossier de construction `eris-participation-28f46de`.

## Retour arrière

Configuration et sauvegardes SQLite cohérentes, privées, préfixées par :

`/mnt/Main/tank/concierge/ops-backups/before-eris-participation-20261026T181727Z`

Fichiers : `.json` pour la configuration, `.db` pour la base principale et `-character.db` pour la base de personnage. Ces fichiers ne doivent pas être affichés ou committés. Le script prévoyait un retour de configuration automatique en cas d'échec ; aucun retour arrière n'a été nécessaire. Un premier contrôle préalable attendait encore le schéma v8 et a interrompu la procédure avant toute bascule ; le schéma v9 réel et sa compatibilité ont été vérifiés avant relance.

Restaurer la configuration via `app.update` rétablit l'image `89a54e0`, sans restaurer les bases ni effacer les données récentes. La branche d'origine et ses modifications locales restent intactes ; la branche de déploiement se trouve dans `.cache/eris-participation-release`.
