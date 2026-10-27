# Eris : livraison sur le NAS — 27 octobre 2026

Déploiement demandé par Hugo après lecture de la nouvelle candidate issue de la
recette humaine. La [PR #82](https://github.com/hloiseau/le-concierge/pull/82)
est fusionnée dans `main`, avec les commits historiques récupérés dans ses
ancêtres. La [PR #83](https://github.com/hloiseau/le-concierge/pull/83) conserve
le script d'exploitation et cette note, sans données privées.

## Version réellement déployée

| Élément | Référence vérifiée |
| --- | --- |
| Sources applicatives sur main | `13ffb1bed93042a9c1cbaced8de28828b59862c1` |
| Candidate de voix incluse | `a70cafa164e80e5122845ff7429a54fe3f4b9da5` |
| CI de main avant bascule | [tests, image publiée et déploiement isolé réussis](https://github.com/hloiseau/le-concierge/actions/runs/40574460040) |
| Tag publié utilisé pour le téléchargement | `ghcr.io/hloiseau/le-concierge:sha-13ffb1bed93042a9c1cbaced8de28828b59862c1` |
| Référence épinglée dans API et bot | `ghcr.io/hloiseau/le-concierge@sha256:f92961ae4f92cd223ba2c65b38d78cf5a06580b034fd89e4c7e82fa9b36b68dd` |
| ID d'image réellement exécuté par les deux services | `sha256:68f67f92e799f89815c927a7d94b7669c9da0234a7111a5492a97cef60d7095a` |
| Profil, transport et modèle conservés | `v2` / `responses` / `gpt-6.1-sol` |
| Version remplacée | `1894fae72274981516692a0b746d97e80fa350f2` |

La publication CI lie l'image au SHA de main et à son label de révision. Le digest
publié est ensuite épinglé dans la configuration et les IDs exécutés sont relus
après la bascule. Le runtime, l'authentification et le Hub conservent leurs images.
Les commits d'exploitation/documentation suivants ne changent pas les binaires
API/bot de cette livraison ; ils ne sont pas annoncés comme une nouvelle version
déployée.

## Sauvegardes et contrôles observés

Le script [deploy_eris_nas.py](../tools/deploy_eris_nas.py) est commité et poussé
avant utilisation dans la PR #83 (`2a1d652ebb19ba596df8cc6eff1822eddd0579d4`).
Son empreinte exécutée est
`189189c463314cc6ffb0907410977ea80fed191a85bf897b566d4c2373f94b69`.
Le précontrôle vérifie l'ancienne version effective et les bases ; la configuration
est relue avant modification pour refuser une dérive pendant la préparation.

Une sauvegarde privée indépendante de la configuration et de chacune des deux
bases SQLite existe sous le nom de lot
`before-eris-reconciled-20261027T200944Z`. Le chemin rempli et les fichiers restent
sur le NAS dans le dossier d'exploitation privé. Les bases sont sauvegardées via
SQLite backup, y compris les transactions validées dans WAL, puis contrôlées.
Fichiers privés en mode 600, dossier en mode 700 et ACL nettoyées. Aucune base n'a
été restaurée ou supprimée pendant cette mise à jour.

Après `app.update`, les observations suivantes réussissent :

- API, bot, Hub, runtime et authentification sains ; absence d'OOM.
- API et bot exécutent exactement l'image et la révision indiquées ci-dessus.
- Contrôles `/app/api --healthcheck` et `/app/bot --healthcheck` réussis.
- Gateway Discord reconnectée et mise à jour des commandes confirmée par les logs
  du nouveau bot ; `/eris-salon` relu auprès de Discord avec permission administrateur
  et choix `activer`, `desactiver`, `etat`.
- Base principale en schéma **v9** avant et après ; bases principale et personnage
  intègres. Tables de mémoire durable, résumés et réglages conservées ; les **deux
  salons** de participation sont toujours activés.
- Hub accessible, accès Hub → API fonctionnel et routes privées sans session à
  **401**.

Les trois tests du script couvrent la modification des seules images API/bot,
le refus de références mutables et la sauvegarde SQLite avec WAL sans écrasement.
La CI applicative comporte les tests avec `-race`. Les [44 étapes comparatives
Sol](eris-human-review-79.md) précèdent la bascule ; aucun message Discord de test,
demande Seerr ou action média réelle n'a été envoyé pour ces contrôles.

## Retour arrière

Le script prévoit le retour automatique à la configuration précédente si les
contrôles de la mise à jour échouent. Ce chemin n'a pas été nécessaire.
Pour un retour manuel, relire la sauvegarde privée de configuration et appliquer
les anciennes images API/bot, puis vérifier leur santé et la connexion Discord.
Le schéma principal reste v9 et les tables de personnage sont additives ; revenir
à `1894fae` n'implique pas de restaurer une ancienne base ni de perdre les souvenirs
ou demandes récents. La restauration des bases est une opération distincte.

## Suite

Hugo a approuvé la mise en production de cette candidate. Les limites de style
et de références observées restent décrites dans le [bilan de recette](eris-human-review-79.md)
et peuvent être observées en usage réel. La [revue des outils #78](https://github.com/hloiseau/le-concierge/issues/78)
reprend avec Hugo ; elle ne vaut pas autorisation de tout implémenter.
