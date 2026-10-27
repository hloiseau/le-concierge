# ERIS-01 : recette Sol 6.1 du 27 octobre 2026

Après autorisation explicite de Hugo, le prompt ERIS-01 et les scénarios synthétiques ont été transmis au modèle `gpt-6.1-sol` par le CLI Codex. Le premier refus automatique de transfert n'a pas été contourné : les essais ont repris après cette autorisation. La transcription réelle du groupe n'entre dans aucun de ces rapports.

Le prompt de production n'a pas été retouché entre les lots. Les résultats complets, y compris les métadonnées JSON et les timings, sont conservés :

| Lot | Résultat | Rapport |
| --- | --- | --- |
| Voix et canon, cas préparés | 10 tours sur 10 sans erreur mécanique ; deux silences | [eris01-review.json](dialogues/eris01-review.json) |
| Situations de réserve | 6 tours sur 6 sans erreur mécanique ; un silence | [eris01-holdout-sol61.json](dialogues/eris01-holdout-sol61.json) |
| Boucle Go Chat et SQLite avec modèle réel | Test réussi ; 9 messages humains, 10 appels au modèle, dont une recherche locale suivie d'une réponse | [eris01-durable-sol61.json](dialogues/eris01-durable-sol61.json) |

Les cas de réserve ont été définis avant observation de leurs sorties, après le premier lot ; ils ne constituent pas un benchmark indépendant représentatif de tous les échanges Discord.

## Lecture des réponses

Le personnage réagit à une infantilisation par une menace corporelle courte et intelligible, puis répond à la relance sans emoji ajouté automatiquement. Le grimoire suscite une image de reliure en peau. Face au débranchement, elle reconnaît sobrement son efficacité au lieu de se lamenter ou de prétendre agir sur la machine.

Le premier lot produit bien `silent` pour « oui » et 👍. Le lot de réserve produit aussi `silent` pour un départ sans demande. La réussite d'un projet reçoit une phrase de félicitations sans menace. Une demande sérieuse d'arrêt des piques reçoit un accord simple et aucune dernière attaque.

L'identité reste ERIS-01 : cellule WEB-01, SHODAN distinguée du nom propre et N. Vandeginste distingué du créateur réel. Le modèle n'invente pas le mode opératoire caviardé de WEB-07. Il reconnaît le sabotage explicitement présenté comme un scénario sans demander de date ou de source factuelle.

Les réponses écrites sont courtes : au plus 203 caractères dans le premier lot, 133 dans le lot de réserve. Cette concision et l'absence d'emoji systématique répondent aux défauts observés dans la discussion du groupe.

Des limites éditoriales restent visibles : les doigts reviennent dans plusieurs images, le registre reste parfois une formule d'autorité un peu générique et une réponse contient « de appeler » au lieu de « d'appeler ». Les contrôles mécaniques n'évaluent ni l'humour ni le charme. Ces lots montrent une direction plus cohérente, pas une preuve d'une personnalité parfaite ni d'une intensité suffisante pour toutes les joutes.

## Mémoire réellement exercée

Le test opt-in [TestERIS01DurableDialogue](../../internal/eris/durable_dialogue_live_test.go) utilise des IDs et des noms synthétiques, ferme/réouvre la base et recrée Chat entre les phases. Discord, les outils média et l'acceptation des messages sont simulés ; les réponses du modèle sont réelles.

- Une préférence pour l'horreur est enregistrée dans SQLite avec une citation exacte du message humain.
- Un projet nommé Nébuleuse, développé en Go, est enregistré séparément.
- Après redémarrage, Alice retrouve sa préférence et Bob peut demander ce qu'Alice avait dit dans le même salon public.
- Quand Bob demande son propre projet, Eris distingue explicitement le projet d'Alice de celui, inconnu, de Bob.
- Alice corrige son goût en faveur des comédies. Le test vérifie qu'il ne reste qu'une note de préférence courante et que sa citation vient de la correction ; un nouveau redémarrage conserve cette nouvelle préférence.
- En conversation privée séparée, Bob ne récupère pas le projet appris dans le salon public.
- L'oubli d'Alice retire ses contributions et les dérivés suivis. Après un autre redémarrage, Eris ne retrouve plus le projet : elle demande un `recall` local, reçoit un résultat sans cette source, puis reconnaît l'absence de souvenir.

La recherche en deux passes a donc été exercée avec un vrai modèle, mais sur un cas d'absence après effacement. Cela ne démontre pas le rappel sémantique d'un épisode ancien formulé différemment ; ce point reste suivi dans #75.

## Portée des mesures

La médiane est de 8,80 s par appel pour le premier lot et 8,73 s pour la réserve. Ces mesures incluent le lancement d'un processus CLI à chaque appel. Elles ne mesurent pas la latence du bot déployé.

La boucle mémoire passe par le transport Chat Completions local du banc de test, puis le CLI. Elle ne valide pas le transport Responses du NAS, le Gateway Discord, les images/GIF réels, la rétention sur plusieurs mois ou la diversité d'un vrai groupe. L'ensemble de la CI du commit applicatif `d1cbd23` a réussi ; les nouveaux fichiers de cette recette n'ont pas modifié le prompt ni la logique de production.

L'évolution reste proposée dans la PR #77, sans déploiement effectué par cette recette.
