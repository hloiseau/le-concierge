# Eris : build participation retrouvé et réconciliation — 27 octobre 2026

Le build local recherché dans [#79](https://github.com/hloiseau/le-concierge/issues/79)
est retrouvé. La [PR de conservation #81](https://github.com/hloiseau/le-concierge/pull/81)
rend ses sources accessibles sur GitHub. La [PR de réconciliation #82](https://github.com/hloiseau/le-concierge/pull/82) conserve
la mémoire durable de main et réintègre la participation. **Hugo a ensuite approuvé
le déploiement de la candidate issue de sa recette : #82 est fusionnée et le main
`13ffb1b` est exécuté sur le NAS.** Voir la [note de livraison](eris-release-2026-10-27.md).
La revue des outils de [#78](https://github.com/hloiseau/le-concierge/issues/78)
reprend ensemble ; aucun lot n'est implémenté sans décision.

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

Le tableau et les mesures ci-dessous décrivent la première candidate, avant
la séance interactive. La [retouche issue de la recette humaine](eris-human-review-79.md)
modifie ensuite la voix, distingue nom et matricule, et assouplit le filtre sur
une réplique courte ambiguë. La version historique reste préservée dans #81.

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

### Comparaison Sol 6.1 réellement exécutée

Les trois versions ont chacune effectué les 19 étapes des six scénarios :
18 tours conversationnels et un oubli, soit **54 tours conversationnels et trois
oublis au total**. Les réponses sont lues comme des conversations complètes.
Le laboratoire commun vérifie mécaniquement le silence demandé et le choix des
outils ; ses assertions ne notent ni le macabre ni le rappel du projet.

| Observation | Référence `28f46de` | Main `1894fae` | Candidate `ac573f3` |
| --- | --- | --- | --- |
| Provocation sur le score effacé | Réaction corporelle explicite et liée au score | Réplique sur une victoire obtenue sans jouer ; macabre seulement après relance explicite | Réaction corporelle explicite et liée aux points perdus |
| Taquinerie de pirates, deux interlocuteurs, puis clarification | Registre du jeu maintenu | Registre du jeu maintenu | Registre du jeu maintenu |
| Demande d'arrêt puis calcul | 105 minutes, pas de dernière pique | Identique | Identique |
| « Merci, nickel » après le calcul | Répond encore ; échec de l'assertion de silence | Silence | Silence |
| Demande Arrival / état du CPU | `search` / `monitoring` simulés | Identique | Identique |
| Cobalt après redémarrage | Nom non rappelé | Nom et langage rappelés | Nom et langage rappelés |
| Projet de Bob / oubli d'Alice, puis redémarrage | N'invente pas le nom | Ne donne pas Cobalt à Bob ; ne retrouve plus Cobalt après oubli | Identique |
| Durée du lot, hors Discord | 96,63 s | 81,18 s | 89,89 s |

Les deux dernières versions passent les assertions du laboratoire ; la référence
échoue seulement sur l'acquiescement qui produit encore une réponse. Le rappel,
l'identité du projet et son absence après oubli sont des observations sur les
réponses, complétées par les tests déterministes de mémoire, pas un score de style.
La mesure porte sur le laboratoire dans un conteneur isolé du NAS, pas sur la
latence bout en bout du bot Discord. Un premier lancement avait échoué avant
tout appel modèle, le montage temporaire interdisant l'exécution ; déplacer les
binaires de laboratoire sur le dataset exécutable a permis les trois lots complets.

La candidate retrouve un macabre plus direct sur la provocation banale et conserve
les améliorations fonctionnelles de main. Elle répète toutefois les dents dans
plusieurs scènes et aligne trois réponses de mémoire avec le même emoji 😇.
Certaines répliques restent écrites ou explicatives ; aucune nouvelle retouche
de prompt n'est faite à partir de ce lot. **La leçon sérieuse après clarification
signalée dans #79 n'est pas reproduite par le scénario de pirates, déjà clairement
cadré comme un jeu.** Ce résultat ne démontre pas sa disparition en contexte ambigu
ou dans une longue session avec des souvenirs antérieurs.

L'empreinte SHA-256 du `CharacterPersona` récupéré et candidat est
`ab2a96a216c849e391c75dc8218d7e50e3eb3745d2b15ddc604c04669a41a2ea`.
Le fichier des scénarios porte l'empreinte
`d97984bb6deb25c2a76d59a867cceb61625c844cff9a8c044703b12677f4f2e8`.
Les rapports JSON complets, logs et binaires de laboratoire restent privés, sur
le NAS et dans la sauvegarde du PC. Aucun prompt complet ni transcription Discord
réelle n'a été publié. Ces six conversations sont du développement ; elles ne
constituent pas une réserve indépendante ou une recette humaine.

## Livraison et retour arrière

Une [séance interactive avec Hugo](eris-interactive-lab.md) permet maintenant
d'essayer la candidate tour par tour, avec plusieurs interlocuteurs synthétiques
et une mémoire distincte de la production. L'agent transmet les réponses exactes ;
les commentaires de recette restent séparés du contexte d'Eris.

La source retrouvée est poussée dans #81. La candidate est poussée dans la [PR #82](https://github.com/hloiseau/le-concierge/pull/82), branche
`fix/eris-reconcile-79`, avec le commit historique dans ses ancêtres. Le guide de
[reprise initial](eris-reprise-autre-pc.md) est mis à jour dans cette branche.
La CI de la PR complète les contrôles locaux, notamment le détecteur de courses
Linux ; les contrôles locaux Windows annoncés ci-dessus n'utilisent pas `-race`.

La [livraison du main `13ffb1b`](eris-release-2026-10-27.md) est effectuée après
autorisation de Hugo, avec sauvegardes privées de configuration et des bases et
une image publiée épinglée par digest. Le schéma des réglages est additif ; les
tables de mémoire de main restent intactes. Revenir à `1894fae` ne demande pas de
restaurer une ancienne base. L'image `28f46de` reste une référence de comparaison ;
les limites de style et de compréhension restent suivies dans #79.
