# Eris : build participation retrouvé et réconciliation — 27 octobre 2026

Le build local recherché dans [#79](https://github.com/hloiseau/le-concierge/issues/79)
est retrouvé. La [PR de conservation #81](https://github.com/hloiseau/le-concierge/pull/81)
rend ses sources accessibles sur GitHub. La branche `fix/eris-reconcile-79` conserve
la mémoire durable de main et réintègre la participation. **La candidate n'est ni
fusionnée ni déployée. Le ton reste soumis à la recette de Hugo.** Les outils de
[#78](https://github.com/hloiseau/le-concierge/issues/78) restent suspendus.

## Provenance vérifiée

| Élément | Preuve |
| --- | --- |
| Code du build apprécié | `28f46de166fe4266a90e8ef24c43b24ca542752a`, parent `89a54e0ce213d23bf3847ace61cc45cb37a8ff42` |
| Note historique | `33b2df13de1227e89a6665aa6b6cf3d0e36a1c10`, publiée dans #81 |
| Archive source NAS | SHA-256 `beb32cedfa6eadffc4026957696c3922fbd2d6c41cff7683cedd4d1b7267388f` ; les 370 fichiers correspondent exactement à `git archive 28f46de`, sans fichier ajouté, absent ou modifié |
| Ancienne image locale | `eris-concierge:participation-28f46de`, ID `sha256:2518757c742059fb1bcee11a963e71e85adabb85be379ed064b0f51ab275c3e1` ; aucun RepoDigest publié |
| Binaire API dans l'image | SHA-256 `59d143f86b5b56a6aa95cc4512e10c99fd4806a4767fce2cc856b5b79bf8762f` |
| Binaire bot dans l'image | SHA-256 `ddcad74102cbadc7d09e48ae6b758053428b5e7d73aec647daf7bd35f27c9ee9` |

Les empreintes des binaires ont été calculées dans les couches de l'image exportée,
dans le dossier de construction du NAS et dans le dossier local de publication :
elles concordent. Le Dockerfile remplace uniquement API et bot dans l'image du
parent `89a54e0`. Le label de révision n'est donc pas utilisé comme preuve unique.
Le worktree historiquement nommé `eris-participation-release` a depuis avancé vers
des corrections Downloadarr/Maintainerr ; son nom seul ne désigne pas le build.

## Sauvegardes et état réellement exécuté

Avant toute mutation Git, une copie indépendante privée du checkout Windows a
préservé l'historique, les sources, les fichiers non suivis et les fichiers locaux.
La copie a terminé sans échec ni discordance (46 286 fichiers, environ 9,66 Gio).
Seuls les jonctions et le cache de modèles téléchargés contenant des liens
inaccessibles ont été exclus. Le reçu privé enregistre ces exclusions. Une
sauvegarde privée de l'image historique existe aussi sur le NAS (78 629 376 octets).
Aucun de ces dossiers privés ni les rapports bruts n'entre dans Git.

API et bot étaient sains et utilisaient tous deux
`ghcr.io/hloiseau/le-concierge:sha-1894fae72274981516692a0b746d97e80fa350f2`,
image réellement exécutée `sha256:9e9fd3f021d0664947dc87f6e2e83883d3939941813ee6f435a3f9b2bccdda7e`.
Le bot est en profil `v2`, protocole `responses`, modèle `gpt-6.1-sol`, avec
Message Content activé. Le runtime et l'authentification sont sains. Les variables
complètes, identifiants Discord, clés et données des membres n'ont pas été publiés.

Le checkout `feat/eris-test-ground` reste sur `06bfffb`, avec ses changements
initiaux. Ses anciennes expériences de ton ne sont pas confondues avec le build
validé ; elles sont sauvegardées en privé et ne sont pas importées dans la candidate.
Les récupérations, comparaisons et tests sont effectués dans d'autres worktrees.

## Comparaison et choix de réconciliation

| Sujet | Build `28f46de` | Main `1894fae` | Candidate |
| --- | --- | --- | --- |
| Voix | Prompt de joute horrifique expressive de #73 | Réécriture privilégiant davantage calme et brièveté, nouveaux exemples | `CharacterPersona` restauré exactement depuis `28f46de`, sans nouvelle retouche éditoriale |
| Identité | Personnage Eris | Canon permanent ERIS-01 | Canon de main conservé, également transmis aux interventions spontanées |
| Contexte | Identités vérifiées, images, contexte public borné ; 12 messages de session | 40 échanges courts, budget, résumé progressif et contexte ambiant RAM | Fenêtres et résumés de main conservés ; relances implicites récupérées |
| Mémoire | Carnet des lubies et réglages des salons | Notes/épisodes SQLite sourcés, corrections, oubli transitif, livraison confirmée | Toutes les tables et garanties de main conservées ; table des réglages ajoutée sans destruction |
| Participation | `/eris-salon`, opt-in par salon, plafond de cinq minutes, aucun outil, aucune relance ignorée | Fonction et commande absentes | Fonction et commande récupérées ; l'état silencieux du salon bloque aussi une intervention spontanée |
| Relances | Nom Eris ou réponse implicite, deux minutes et cinq messages intercalés | Mentions/réponses Discord et silence des clôtures | Relances récupérées ; « nickel »/réactions ne déclenchent pas de génération ; mention ou appel par le nom permet la reprise |
| Demandes publiques | Titres, saisons et états des demandes du membre consultables en salon | Listes personnelles renvoyées vers le privé | Politique récupérée avec relecture Discord côté API et identité du membre courant ; filtres de visionnage et données de compte restent privés |
| Outils et modèle | Sol 6.1 ; outils existants | Sol 6.1 sur le NAS | Aucun nouvel outil, aucune modification de modèle ni d'authentification |

Le diff est une preuve de changements de code et de consignes. Il n'isole pas à
lui seul la cause de chaque réplique décevante : modèle, contexte et souvenirs
influent aussi sur la génération. La restauration exacte constitue une candidate
comparable, pas une déclaration que la régression de ton est corrigée.

## Vérifications et recette

- `go test ./...` : 2 284 tests réussis après réconciliation des flux et ajout des
  cas de silence/migration ; `go vet ./...` et `git diff --check` réussis.
- Tests d'intégration du Gateway : vraie relance avec messages intercalés,
  acquiescement sans inférence, résultat `silent` sans envoi Discord et annulation
  d'une réponse en cours lorsqu'un message plus récent arrive.
- Test de migration d'une base ayant déjà la mémoire durable : ajout des réglages,
  épisode conservé après réouverture, oubli du membre sans suppression du choix
  administrateur. Les tests existants couvrent les notes, résumés, oubli transitif,
  cloisonnement, outils et livraison.
- [Laboratoire commun](../internal/eris/reconciliation_live_test.go) et
  [conversations synthétiques](../evals/eris/dialogues/reconciliation-79.json) :
  même code de laboratoire sur la référence, main et la candidate, même Sol 6.1,
  transport Responses, bases temporaires, livraisons et outils simulés. Chaque
  réponse générée alimente le tour suivant. Aucun message Discord réel n'est envoyé.

Les résultats de la comparaison modèle sont consignés après exécution dans cette
section. Les fichiers bruts contenant prompts complets restent privés. Le verdict
éditorial porte sur les échanges complets, le naturel, la compréhension et le
macabre ; le JSON valide et les mots-clés ne suffisent pas.

## Livraison et retour arrière

La source retrouvée est poussée dans #81. La candidate suit la branche
`fix/eris-reconcile-79`, avec le commit historique dans ses ancêtres. Le guide de
[reprise initial](eris-reprise-autre-pc.md) est mis à jour dans cette branche.
La CI de la PR complète les contrôles locaux, notamment le détecteur de courses
Linux ; les contrôles locaux Windows annoncés ci-dessus n'utilisent pas `-race`.

Aucun déploiement de la candidate n'a eu lieu. Avant une bascule partagée, conserver
une sauvegarde cohérente de la configuration et des bases et utiliser une image
publiée associée au SHA vérifié. Le schéma des réglages est additif ; les tables de
mémoire de main restent intactes. Revenir à `1894fae` ne demande pas de restaurer
une ancienne base. L'image `28f46de` reste une référence de comparaison et ne doit
pas remplacer implicitement les améliorations de mémoire. La recette humaine du
ton reste nécessaire avant de conclure #79.
