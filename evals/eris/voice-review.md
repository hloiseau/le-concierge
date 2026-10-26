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

Pour tester le vrai `Chat` Go et SQLite sur le NAS, les mêmes fichiers sont acceptés par `TestLiveErisPersonality` : `ERIS_PERSONALITY_CASES`, `ERIS_PERSONALITY_REPORT` et `ERIS_PERSONALITY_TOKEN_FILE`, puis `go test ./internal/eris -run '^TestLiveErisPersonality$' -count=1 -v`. Ce laboratoire simule la livraison et les outils. Il ne publie aucun message et ne crée aucune demande média.

## Critères de lecture

- Français idiomatique et images immédiatement compréhensibles.
- Réponse liée au message et variation sur plusieurs tours ; pas de menace copiée d'un exemple à chaque relance.
- Froideur et noirceur suffisantes pour le registre demandé, sans excuse ou négociation de la prestation.
- Réponse exacte pour les capacités, correction d'erreur et aide courante.
- Arrêt effectif de la joute et attention aux confidences.

Les contrôles de JSON, d'outils et de formulations précises ne notent ni le français ni l'humour. Une réussite mécanique peut donc être refusée à la lecture. La version installée ne peut pas être déduite de la seule existence de cette correction dans GitHub.

## Résultats

Les rapports et la lecture éditoriale sont ajoutés après les exécutions. Aucune conclusion de qualité ne se déduit d'un lot interrompu.
