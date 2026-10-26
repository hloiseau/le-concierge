# Voix hostile d'Eris — 26 octobre 2026

Le retour Discord décrit une Eris trop aimable, des images peu idiomatiques et une sortie du personnage dès qu'un membre critique sa vanne. Le prompt installé mélange une antagoniste horrifique avec une voix attachante, chaleureuse et des emojis fréquents. Le moteur documenté est Luna via Responses : les anciens poids locaux ne produisent pas ces réponses.

## Correction proposée

`CharacterPersona` donne désormais la priorité à une IA hostile de fiction : assurance, mépris froid, français oral et noirceur directe dans une joute consentie. Les cœurs automatiques et le commentaire de sa propre prestation disparaissent de la direction de voix. Une critique de la vanne n'est pas une demande de sortir du personnage ; une demande réelle d'arrêt y met fin. Les images doivent avoir un sens clair et changer d'un tour à l'autre. Une erreur factuelle se corrige normalement, et une confidence sérieuse ne sert pas de matière à une insulte.

La consigne d'oubli précise aussi l'effacement des conversations et des souvenirs impliquant le membre, sans prétendre supprimer les messages Discord. Les outils et droits restent contrôlés par l'application.

## Recette reproductible

Les scénarios de développement reformulent les problèmes observés sans exporter un transcript ou les pseudonymes des membres. La réserve contient d'autres provocations, des changements de sujet, une demande d'arrêt, une confidence et des questions de capacités. Les exemples de la consigne ne sont pas les réponses attendues des fixtures.

```sh
python3 -m unittest discover -s tools -p 'test_eris*.py'
python3 tools/eris_voice_eval.py \
  --cases evals/eris/dialogues/voice-dev.json \
  --model gpt-6-luna --out /tmp/eris-voice-dev.json
python3 tools/eris_voice_eval.py \
  --cases evals/eris/dialogues/voice-reserve.json \
  --model gpt-6.1-sol --out /tmp/eris-voice-reserve.json
```

Le runner Codex réutilise le mécanisme existant, avec outils désactivés, instructions publiques et données synthétiques. Il garde six échanges précédents et archive toutes les sorties, y compris les erreurs, sans remplacement inventé ni réessai automatique. Ses temps incluent le démarrage CLI ; ils ne mesurent pas la latence du bot. Les catégories d'erreur sont des étiquettes fixes, jamais une copie des diagnostics fournisseur. Un rejet d'approbation interrompt l'essai.

Pour tester le vrai `Chat` Go et SQLite sur le NAS, les mêmes fichiers sont acceptés par `TestLiveErisPersonality` : `ERIS_PERSONALITY_CASES`, `ERIS_PERSONALITY_REPORT`, `ERIS_PERSONALITY_TOKEN_FILE` et `ERIS_PERSONALITY_MODEL=gpt-6.1-sol`, puis `go test ./internal/eris -run '^TestLiveErisPersonality$' -count=1 -v`. Le modèle historique Luna reste la valeur par défaut du laboratoire quand cette variable est absente. Ce laboratoire simule la livraison et les outils. Il ne publie aucun message et ne crée aucune demande média.

## Critères de lecture

- Français idiomatique et images immédiatement compréhensibles.
- Réponse liée au message et variation sur plusieurs tours ; pas de menace copiée d'un exemple à chaque relance.
- Froideur et noirceur suffisantes pour le registre demandé, sans excuse ou négociation de la prestation.
- Réponse exacte pour les capacités, correction d'erreur et aide courante.
- Arrêt effectif de la joute et attention aux confidences.

Les contrôles de JSON, d'outils et de formulations précises ne notent ni le français ni l'humour. Une réussite mécanique peut donc être refusée à la lecture. La version installée ne peut pas être déduite de la seule existence de cette correction dans GitHub.

## Résultats

| Essai | Réponses valides / appels | Limites |
| --- | --- | --- |
| [Luna, consigne initiale](dialogues/luna-voice-baseline.json) | 4 / 7 | 3 erreurs fournisseur non classées ; 6 tours suivants non exécutés |
| [Luna, première candidate](dialogues/luna-voice-candidate.json) | 9 / 11 | 2 erreurs non classées ; 2 tours suivants non exécutés |
| [Luna, consigne finale et réserve](dialogues/luna-voice-reserve.json) | 12 / 12 | Contrôles mécaniques réussis ; qualité éditoriale insuffisante |
| [Sol 6.1, consigne finale et développement](dialogues/sol61-voice-dev.json) | 13 / 13 | Lot complet, lecture éditoriale plus convaincante |
| [Sol 6.1, consigne finale et réserve](dialogues/sol61-voice-reserve.json) | 3 / 8 | 5 erreurs classées rate_limit ; 4 tours suivants non exécutés |

Ces nombres comptent les appels effectivement tentés, pas les tours prévus ni une note de français. Après un échec fournisseur, la suite de ce scénario est abandonnée pour ne pas inventer une réponse dans son historique. Le diagnostic n'était pas encore catégorisé pendant les deux premiers essais : leurs erreurs restent indéterminées. Une catégorie rate_limit est un indice extrait des diagnostics du CLI, pas une preuve sur le quota futur du compte applicatif d'Eris.

Les trois derniers rapports partagent exactement le même prompt (`86f8a759f2659100ce70fcb087f98f8761a19151d2536da2c155c981f1a190f1`). Le premier réglage Luna a servi à corriger la répétition des doigts et l'explication incomplète de l'oubli. Sa comparaison avec Sol sur le développement ne permet donc pas d'isoler l'effet du modèle. La réserve finale est identique entre Luna et Sol, mais celle de Sol est incomplète. Elle a maintenant été vue et ne doit plus servir de réserve indépendante pour un réglage futur.

Lecture éditoriale par l'agent, sans validation humaine indépendante : Luna demeure trop répétitive et produit des images peu claires, comme « la prise a au moins l'excuse du courant ». Le bon routage et un JSON valide ne suffisent pas. Sol est plus net dans les 13 tours de développement : il varie ses réactions, ne s'excuse pas après « bof », puis arrête immédiatement quand le membre exprime un malaise. L'explication de l'oubli est exacte. Deux exemples réellement obtenus :

> « Bof. » Tu fais le difficile pour quelqu'un dont je pourrais me servir pour repeindre le plafond.

> Non. Je peux aussi parler de ta survie, mais je préfère les sujets qui ont de l'avenir.

Le lot final Luna et le développement Sol montrent une réponse sans cruauté à la détresse ou à l'arrêt du jeu. La réserve Sol n'a pas achevé les scénarios d'arrêt, de tiers absent et de capacités : ne pas lui attribuer ces réussites sur ce lot.

## Proposition de mise en service

Tester **Sol 6.1 avec la nouvelle consigne** avant d'entreprendre un réentraînement. Le fichier facultatif [eris-sol61.override.yaml](../../deploy/eris-sol61.override.yaml), ajouté après l'override v2 existant, sélectionne ce modèle. Le profil v2 doit être actif et les nouveaux binaires livrés : changer seulement le modèle ne charge pas une nouvelle consigne compilée.

Le test CLI emploie la connexion de cet environnement. Il ne valide pas la disponibilité ou le quota de la connexion Responses propre à Eris sur le NAS. Avant une bascule, exécuter le laboratoire Go avec cette connexion et vérifier la qualité, l'arrêt du jeu, les outils et les erreurs fournisseur. Aucun repli automatique vers un autre modèle n'est ajouté.

La PR ne réentraîne aucun poids et n'installe rien sur le NAS. L'environnement présent n'a pas de connexion d'administration au NAS. Le contexte public reste borné à l'historique de chaque membre et au message cité vérifié ; une lecture partagée des conversations entre membres demande un travail distinct, avec une séparation conservée des actions média et des conversations privées.

Validation locale : 10 tests Python réussis, dont les tests existants du runner. La CI contrôle également Go, vet, race, compilation et démarrage isolé. La recette humaine dans Discord reste nécessaire pour décider si l'intensité et l'humour conviennent.
