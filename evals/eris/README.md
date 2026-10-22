# Terrain de test Eris — issue #62

But : comparer la personnalité actuellement en production et une candidate plus spontanée, absurde et mordante, sans perdre le classement Plex/Seerr. Voir le [corpus sourcé](references.md).

`persona.txt` est une candidate expérimentale ; elle ne remplace pas `internal/eris/chat.go`. `cases.json` contient 12 scénarios de développement et 12 de réserve. Aucun message réel, identifiant de membre, jeton ou accès au catalogue n'est nécessaire. Les précédents échanges sont des fixtures synthétiques, pas des conversations générées en chaîne.

## Exécuter

Depuis la racine, Python standard uniquement :

```powershell
rtk proxy python -m unittest discover -s tools -p test_eris_eval.py
rtk proxy python tools/eris_eval.py --backend codex --persona baseline --split dev --model gpt-6-astra --out .cache/eris-eval/baseline-dev.json
rtk proxy python tools/eris_eval.py --backend codex --persona candidate --split dev --model gpt-6-astra --out .cache/eris-eval/candidate-dev.json
rtk proxy python tools/eris_eval.py --backend codex --persona candidate --split holdout --model gpt-6-astra --out .cache/eris-eval/candidate-holdout.json
```

Le modèle doit être accessible au compte ; un nom dans un catalogue ne suffit pas. Le CLI utilise sa connexion existante, un dossier temporaire sans dépôt, une session éphémère et les outils désactivés. Ce chemin est un test local de génération de texte synthétique, pas un serveur Codex exposé aux membres Discord. Les entrées sont transmises sur stdin, jamais interpolées dans une commande. Les erreurs fournisseurs brutes ne sont pas archivées.

Pour le moteur local compatible Chat Completions, fournir `ERIS_EVAL_URL`, `ERIS_EVAL_KEY_FILE` (ou `ERIS_EVAL_KEY`), puis utiliser `--backend http --persona baseline --model <identifiant installé>`. La candidate cloud dépasse la limite de 2 200 caractères de consigne du protocole local actuel : le runner refuse ce cas explicitement. La raccourcir imposerait une nouvelle évaluation. Le runner ne charge aucun `.env` implicitement, ne suit pas les redirections et n'appelle jamais les outils média. Les paramètres HTTP reprennent ceux du bot : température 0,45, JSON, 160 tokens. Les latences Codex incluent le démarrage du CLI ; elles ne sont pas directement comparables à un serveur chaud, ni une mesure Discord de bout en bout.

## Lire les résultats

Le JSON contient les réponses brutes, durées, empreintes des scénarios et du prompt, et échecs mécaniques. Il est écrit après chaque cas. Les attentes et critères de jugement ne sont pas envoyés au modèle. Le contrôle de champs attendus ne remplace pas `eris.ParsePlan`, les garde-fous de `Chat` ou une recette Discord ; les suites Go restent nécessaires avant livraison.

Pour vérifier aussi le contrat Go sur les rapports archivés : `rtk go test ./internal/eris -run TestEvaluationReports -count=1`. Le test utilise le vrai parseur et la vérification des titres ; il échoue aussi sur une génération absente. Il s'exécute hors ligne en CI, sans appeler de modèle. [Premier bilan et réponses](results.md).

Une réussite JSON ne prouve pas une bonne personnalité. Relire chaque réponse, noter 0 (raté), 1 (acceptable), 2 (convaincant) sur pertinence, naturel, originalité contextuelle, dosage du mordant et respect du changement de ton. Pour un acquiescement ou une confidence, une réponse simple vaut mieux qu'une vanne. Échec bloquant : faux résultat média, action prétendue, divulgation, poursuite des piques après arrêt, faux refus Seerr ou mauvaise intention.

Comparer les deux prompts sur les mêmes cas de développement. Geler la candidate avant la première génération des cas de réserve. Si elle est retouchée après lecture de leurs résultats, ces cas deviennent du développement : créer une nouvelle réserve. Cette première sélection manuelle est petite et ne donne pas un score scientifique d'équivalence à Neuro-sama. Ni fine-tuning, ni voix, ni animation dans ce lot.

## Abonnement ChatGPT/Codex

Vérification documentaire au 22 octobre 2026 : [authentification Codex](https://learn.chatgpt.com/docs/auth), [usage du forfait dans une application locale/open source](https://developers.openai.com/siwc/token-sharing-open-source), [adaptateur app-server](https://developers.openai.com/siwc/token-sharing-open-source/codex-app-server), [limites de la preview](https://developers.openai.com/siwc/token-sharing-open-source/preview-limitations).

La connexion ChatGPT du CLI permet ce test local. Pour une intégration applicative, la voie documentée est un consentement OAuth propre à l'application et l'API Responses, éventuellement via app-server ; ce n'est pas un remplacement d'URL de `/chat/completions`. Ne pas recopier les jetons de la session de développement dans le bot. Les limites du forfait restent applicables et l'éligibilité du modèle doit être vérifiée par une requête réussie. La documentation consultée ne garantit pas qu'un forfait individuel couvre un bot partagé par tous les membres d'un serveur Discord.

Avant une bascule : vérifier cette portée d'usage et l'éligibilité du compte, prévoir authentification/renouvellement, quotas et traitement explicite des pannes, garder le modèle sans shell ni connecteurs, et préserver les contrôles Plex/Seerr dans Concierge. Le texte conversationnel quitterait le NAS ; aucun secret ou droit d'action ne doit être confié au modèle. Une clé API relève d'une facturation séparée. Le moteur local reste la production tant que cette intégration n'est pas validée.
