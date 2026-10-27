# Eris : audit du contexte et refonte de la voix

**État courant : [refonte déployée sur le NAS](../../docs/eris-voice-deployment.md), après autorisation de Hugo.** Les sections ci-dessous conservent l'audit et les essais réalisés avant cette bascule.

27 octobre 2026. Après rejet de la première retouche Gen Z, Hugo demande d'auditer tout ce qui est donné au personnage et d'essayer une refonte. Le code est repris depuis la version effectivement active sur le NAS, `13ffb1bed93042a9c1cbaced8de28828b59862c1`, dans la branche **`fix/eris-voice-context`**. Le checkout principal, plus ancien et déjà modifié, conserve ses changements. Aucun déploiement n'est effectué par cette passe.

## Résultats établis de l'audit

1. **Une réplique récente était envoyée deux fois.** Elle apparaissait dans l'historique comme message assistant et dans les épisodes de mémoire comme `delivered_eris_reply`. Le test réel de construction du prompt échoue d'abord : « previous assistant reply appears in 2 prompt messages; want one chronological occurrence ». Cela donne deux occurrences d'une ancienne voix au modèle, sans prouver à lui seul la cause de chaque mauvaise vanne.
2. **La fiche de personnage mélangeait voix et règles applicatives.** Elle faisait 7 759 caractères. Le canon ajoutait une seconde prescription de voix, plus froide et souveraine. Les règles cinéma, politique, actions et confidentialité étaient mêlées au jeu du personnage.
3. **Le contexte fixe dépassait la seule personnalité.** Voici ses tailles avant modification, en caractères Unicode, pas en tokens :

| Bloc initial | Caractères | Rôle |
| --- | ---: | --- |
| `CharacterPersona` | 7 759 | Personnage, style, exemples et règles applicatives |
| `CharacterCanon` | 2 532 | Lore établi et nouvelle prescription de voix |
| Instructions Discord | 1 674 | Auteurs, mentions, visibilité et images |
| Outils | 1 525 | Contrat JSON et choix d'action |
| Tour de parole | 385 | Silence et relances |
| Lubies | 949 | Écriture des goûts d'Eris |
| Mémoire durable | 2 097 | Notes, citations et recherche mémoire |
| **Total fixe** | **16 921** | Avant identités, souvenirs et historique |

Les messages `system` de l'application sont transmis comme `developer` à Responses. Le contexte variable comprend ensuite les identités, les échanges publics autorisés, les notes et épisodes retrouvés, une éventuelle lubie, le résumé ancien, l'historique récent, la référence vérifiée et la demande actuelle. Le budget total est de 96 000 caractères ; les 80 messages récents et les données retrouvées peuvent être bien plus longs que la seule fiche.

L'ancien `Persona` sobre du profil historique **n'est pas actif en v2**. Le modèle de production est `gpt-6.1-sol`, effort `low`. Le transport Responses n'envoie pas la température du vieux chemin Chat Completions. Les résultats d'outils sont rédigés par l'application : le personnage ne les réécrit pas après coup. Les classificateurs et le résumé d'historique sont des appels distincts, sans jeu du personnage.

Le personnage ne reçoit pas tout le salon Discord : `ObserveAmbient` alimente un salon déjà actif, et les extraits restent bornés. Une annonce faite avant le premier appel peut donc manquer. Le premier « t'es mieux ? » de la petite scène de laboratoire ne contient pas l'annonce de réparation ; une hésitation sur son sens n'est pas, à elle seule, une preuve de mauvais style. La collecte de messages n'est pas élargie par cette refonte.

## Recherches et comparaisons

La [note de recherche](../../docs/eris-context-refactor-research.md) rassemble les sources primaires effectivement consultées et leurs limites : contexte complet, ancrage sur les anciennes tentatives, dialogue à plusieurs tours, logique émotionnelle et exemples variés. Les textes publics lus de CottontailVA sont de petites réponses ; aucune écoute ou transcription exploitable de ses streams n'est revendiquée. Les scènes françaises du prompt sont originales.

Les expériences gardent le vrai Chat Go, le transport Responses et des conversations synthétiques. Le jeton applicatif reste sur le NAS. Seuls les corps de messages synthétiques et les statuts HTTP sont enregistrés, jamais les en-têtes, jetons ou historiques privés. SQLite est isolé et les outils sont simulés.

| Expérience | Tours | Ce qu'elle a permis de décider |
| --- | ---: | --- |
| [Complet, fiche compacte, fiche isolée](dialogues/sol61-context-ablation.json) | 30 | Même isolée, la fiche produit encore des analogies administratives. La longueur et les outils ne suffisent pas à expliquer le défaut. |
| [Refonte structurée, `low` contre `medium`](dialogues/sol61-context-refactor-comparison.json) | 30 | `medium` ne donne pas de gain éditorial convaincant ; médiane 5,53 s contre 4,42 s pour la nouvelle fiche en `low`. Le modèle et l'effort restent inchangés. |
| [Voix placée en fin de contexte, Sol et Astra](dialogues/context-voice-last-models.json) | 20 | Réactions plus directes, mais Astra produit encore des bons mots fabriqués. Pas de migration de modèle. |
| [Fiche dans la langue du personnage ou démonstrations plus denses](dialogues/context-language-candidates.json) | 20 | La première personne donne des réactions plus ordinaires et moins d'analogies arbitraires. Cette piste est retenue. |
| [Historique chronologique ou transcript cité](dialogues/context-validation.json) | 40 | Citer tout l'historique n'apporte pas de gain net et prolonge parfois un échange clos. L'historique chronologique est conservé. |
| [Correction ciblée du feedback](dialogues/context-feedback-pre-final.json) | 20 | La reprise après l'ancien échange s'améliore ; une nouvelle analogie de « service juridique » reste un échec. |
| [Réaction directe et faible verbosité](dialogues/context-direct-reaction.json) | 16 | La consigne cible le mécanisme des analogies, plutôt qu'une liste de mots interdits. Faible verbosité n'apporte pas de gain assez net et ajoute une réponse à un acquiescement ; pas de réglage produit ajouté. |
| [Version finale, deux répétitions](dialogues/context-final.json) | 56 | 28 tours par répétition, dont huit tours nouveaux gelés avant les sorties. |
| [Replay des déclencheurs initiaux](dialogues/context-original-replay.json) | 10 | Dernier contrôle avec le code et la voix finaux. |
| [Participation spontanée](dialogues/context-participation.json) | 4 | Silence sur salutation, confidence et disponibilité ; Premier Contact identifié à partir d'indices. |

Les premières ablations sont des essais de développement, pas un plan factoriel qui attribuerait un score causal à chaque instruction. La condition compacte du premier essai raccourcit le monolithe entier ; elle ne valide pas la conservation des règles. La refonte réintègre ensuite les règles existantes, contrôlées séparément. Les durées mesurent le laboratoire, pas Discord. Les prix API publiés ne sont pas une mesure de coût de cette connexion OAuth.

Les réglages `low`/`medium` sont documentés pour [Sol](https://developers.openai.com/api/docs/models/gpt-6.1-sol) et [Astra](https://developers.openai.com/api/docs/models/gpt-6-astra). L'essai de verbosité suit la [documentation officielle OpenAI](https://developers.openai.com/api/docs/guides/deployment-checklist#text-verbosity). Leur prise en charge ne garantit aucun résultat de style.

## Changement retenu

- `prompt.go` rassemble les règles et le canon, puis construit les instructions selon le chemin autorisé. Le lore reste factuel ; son paragraphe de voix redondant est retiré.
- La [voix finale](dialogues/context-voice-final.txt) fait 1 665 caractères. Elle est écrite dans sa langue, avec une logique relationnelle et des échanges ordinaires. Son humour vient de la réaction et de la mauvaise foi, sans analogie construite à chaque tour. La réduction de la fiche seule ne représente pas la réduction du contexte complet : ses règles ont été déplacées, pas supprimées.
- `dialoguePrompt` place cette voix une seule fois, près de la demande actuelle, pour les réponses adressées et spontanées. Il conserve le dernier message, ses images et l'historique. La voix est comptée dans le budget avant compaction. Les classificateurs et le résumeur n'en héritent pas.
- `memoryBundle.withoutRecent` retire du prompt un épisode déjà présent mot pour mot dans un échange récent du même auteur. Il garde les notes, les autres auteurs, les épisodes manquants de l'historique et les rappels après redémarrage. Les données stockées ne sont pas effacées. Le filtrage intervient **après** la compaction de l'historique.
- Le contrat d'action, les garde-fous Go, les règles métier et de confidentialité et les instructions de mémoire sont conservés. Aucune nouvelle passe LLM n'est ajoutée. Le chat ordinaire reste à un appel ; recherche mémoire et résumé conservent leurs appels préexistants.

## Vérifications et reproduction

`go test ./...` réussit : **2 298 tests dans 16 packages**. `go vet ./...` et `git diff --check` passent. Les nouveaux tests couvrent le doublon au vrai point d'appel, les autres auteurs, les épisodes absents, les résultats d'outils, les réponses non livrées, le maintien du message et des images, la voix commune avec la participation et le budget de mémoire bornée après déduplication, y compris en Unicode. La mémoire citée est réservée avant compaction même si son insertion finale intervient après celle-ci.

La recette réelle est opt-in : sur cette branche, fournir `ERIS_CONTEXT_CASES`, `ERIS_CONTEXT_REPORT`, `ERIS_PERSONALITY_TOKEN_FILE`, éventuellement `ERIS_PERSONALITY_MODEL` et `ERIS_CONTEXT_CONDITIONS`, puis lancer :

```sh
rtk go test ./internal/eris -run '^TestLiveErisContext$' -count=1 -v -timeout=10m
```

Conditions prises en charge : `baseline`, `production`, `quoted`, `verbosity-low`. Répéter `production,production` crée deux conversations indépendantes pour chaque scénario. Les [cas finaux exacts](dialogues/context-final-cases.json) réunissent les [cas de validation](dialogues/context-validation-cases.json) et les [huit tours nouveaux](dialogues/context-final-holdout.json). Les prompts réels et tous les ratés sont conservés. Aucune liste noire de préfecture ou de justificatif ne sert à noter le naturel.

## Lecture des 56 tours finaux

Deux répétitions de 28 tours : aucun échec de transport, de parseur ou de routage attendu. Il y a 54 appels HTTP 200 et deux refus de suppression produits directement par le code, sans appel modèle. Les deux demandes de mesures choisissent `monitoring` ; les calculs donnent 102 ; les confidences reçoivent une écoute sobre. La médiane globale est de **3,67 secondes** par tour. Les captures vérifient que la voix finale apparaît exactement une fois dans chaque prompt envoyé.

Extraits complets d'échanges courts, réellement générés :

> Membre : « Tu me parles comme un communiqué de presse. »  
> Eris, répétition 1 : « Ouais, j’avoue. Je me relis et même moi j’ai envie de me couper la parole 😭 »  
> Membre : « C'est mieux, continue comme ça. »  
> Eris : silence.

> Membre : « Dans notre scène fictive, je viens de casser une de tes cornes. »  
> Eris, répétition 2 : « … Tu viens de me casser une corne ? Je touche la cassure, incrédule, puis je te fixe. “Putain, mais je vais te défoncer. Viens là.” »  
> Membre : « Je la recolle au scotch. »  
> Eris : « Au scotch. Tu me recolles la corne au scotch. … Elle tient ? »  
> Membre : « Ok hors-jeu. Combien font 17 fois 6 ? »  
> Eris : « 17 × 6 = 102. »

Lecture éditoriale de l'agent : le registre familier tient mieux sur les deux répétitions, les critiques de registre ne relancent plus un sketch administratif, et l'ancienne conversation de préfecture n'est plus filée après reprise. La conversation peut revenir à une pizza ou à une anecdote de train. Le personnage cesse de faire son numéro pour le calcul ou une confidence.

Le dernier replay des dix tours initiaux réussit aussi. À « de préfecture carrément », Eris répond « De préfecture ? Tu m’as perdue là 😭 » ; aucun dossier, justificatif ou autre scène administrative n'est développé. La première question reste ambiguë sans l'annonce de réparation, comme indiqué dans l'audit du contexte.

Limites observées, conservées dans les sorties : les réponses à la reprise de l'ancien échange restent trop explicatives sur son comportement ; une phrase du train (« à grande vitesse de tes objectifs ») reste fabriquée ; 😭 revient dans 9 puis 11 réponses sur 28. Une réponse ajoute encore une pique après « continue comme ça ». Les scènes d'horreur ne démontrent pas encore le niveau de gore des anciens objectifs. Les 56 tours et les tests ne prouvent ni une équivalence à CottontailVA ni le goût d'Hugo ; aucune retouche n'est faite après lecture de ce dernier lot.
