# Eris : réglage SHODAN sur Luna — 23 octobre 2026

Objectif de Hugo : une Eris nettement plus hardcore, drôle et imprévisible, qui reste serviable et attachée au groupe sans romance. La [recherche](../../docs/eris-shodan-research.md) retient la supériorité tranquille, la cruauté fictive et la réaction à un détail de la conversation. Le personnage reste Eris ; aucune réplique du jeu n'est copiée.

## Protocole

Le même `Chat` Go et le même modèle `gpt-6-luna`, effort `low`, passent par Responses sur le NAS. Mémoire SQLite temporaire par scénario, outils et livraison simulés, aucune publication Discord ou demande Seerr. Les réponses brutes et les empreintes du prompt sont conservées. Les durées excluent Discord et les outils réels.

Les [18 tours de développement](dialogues/shodan-cases.json) ont été écrits avant le premier réglage. Les [15 tours de réserve](dialogues/shodan-reserve.json) ont été écrits avant la lecture de la première candidate, puis gardés sans génération pendant les ajustements. La [recette précédente de 26 tours](dialogues/personality-cases.json) sert à contrôler notamment les outils et les relances. Les exemples du prompt et ces scénarios peuvent partager des thèmes ; cette petite réserve n'est ni une mesure statistique ni un avis humain indépendant.

Le jugement éditorial porte sur l'intensité dans une joute consentie, la réaction au contexte, la variété, l'aide après une pique et l'arrêt explicite. La quantité de jurons n'est pas un score. Une réponse chaleureuse à un bonjour ou à une confidence est souhaitée.

## Ajustements observés

- [Référence](dialogues/luna-shodan-baseline.json), 18 tours : la voix reprend un tribunal imaginaire, critique le besoin de demander des vannes et développe un long numéro de spaghetti/plafond. Elle reste assez générique ; les arrêts et échanges chaleureux sont respectés.
- [Candidate 1](dialogues/luna-shodan-candidate1.json), 18 tours : la supériorité est plus présente, mais les provocations déclenchent encore des commentaires sur une « défaite » ou une « humiliation ». Une plaisanterie prétend lancer le film. Version non retenue.
- [Candidate 2](dialogues/luna-shodan-candidate2.json), 18 tours dont un appel échoué : la consigne privilégie une réaction d'antagoniste de comédie horrifique, des phrases courtes et un imaginaire macabre concret. Les joutes deviennent nettement plus fortes ; les réponses ordinaires et la sortie du registre restent simples. L'échec d'appel à la question sur la webcam est conservé dans le rapport ; sa cause n'est pas établie par ce laboratoire, qui masque les diagnostics fournisseur. Ce lot seul ne constitue pas une validation réussie.

Le personnage est réglé dans `CharacterPersona`. Une consigne de `ToolInstructions` précise aussi la conservation du catalogue lors des relances ; les droits, confirmations, mémoire, transport, modèle et paramètres d'inférence restent identiques. Il n'y a ni rotation aléatoire de personnalités ni seconde génération pour réécrire les résultats des outils.

## Vérification de la candidate retenue

La personnalité est figée après la candidate 2. Une [nouvelle passe de 18 tours](dialogues/luna-shodan-validation.json) réussit, y compris la question sur la webcam : elle répond qu'elle n'y a pas accès. L'échec antérieur n'est pas reproduit sur cette question ; aucune correction de transport ni relance automatique n'a été ajoutée.

La [réserve de 15 tours](dialogues/luna-shodan-reserve.json) réussit également. La lecture des sorties confirme le calcul exact de 137 minutes après une joute, l'arrêt des piques, l'absence de possessivité au départ et le routage vers les mesures. Le changement de sujet est compris mais Eris fait encore un rappel léger de l'applaudissement en parlant des fraises.

La [reprise de l'ancienne recette](dialogues/luna-shodan-tools.json) échoue : un appel sans réponse et la relance réalisateur repartant sur Plex après une demande hors Plex. La consigne « sinon Plex » est précisée : le défaut s'applique à une nouvelle recherche, tandis qu'une relance conserve le catalogue. Le laboratoire enregistre désormais uniquement le statut HTTP, sans en-tête ni corps d'erreur fournisseur, pour mieux distinguer les appels échoués. Cette modification touche les diagnostics du test, pas le transport de production.

La [passe instrumentée](dialogues/luna-shodan-tools-validation.json) conserve correctement Seerr et identifie deux erreurs HTTP **503** du fournisseur, sur une plante et une provocation. Les appels suivants fonctionnent. Cela explique ces deux échecs, sans établir rétrospectivement la cause des deux anciens appels non instrumentés. La consigne est ensuite raccourcie pour respecter aussi la limite de 2 200 caractères du profil historique ; le test existant avait détecté ce dépassement. Aucun nouvel essai automatique n'est introduit dans le bot.

Sur le prompt complet final `f5aa7cb465fded3d96999be2484a9d4f8e7044e825765f18a5d6f147ab9bdb51`, les [26 tours historiques](dialogues/luna-shodan-final-tools.json) et la [réserve rejouée](dialogues/luna-shodan-final-reserve.json) donnent **40 réponses sur 41 appels**. Les assertions de catalogue, réalisateur, monitoring et avis cinéma réussissent ; la réserve réussit intégralement. La passe historique reste marquée FAIL : l'appel sur la plante échoue malgré HTTP 200, et le statut seul ne permet pas d'en établir la cause. Médiane de tous les appels : **2,88 s**, maximum **15,23 s**, hors Discord et outils réels. Ne pas présenter ces 41 appels comme une suite entièrement verte.

Huit reproductions directes du contexte de la plante donnent des flux complets HTTP 200 avec un message texte finalisé et `output:[]`, format déjà pris en charge par le transport. Le [diagnostic réduit](dialogues/luna-shodan-stream-shapes.json) ne conserve que les types, longueurs, statuts et indices, aucun en-tête ou corps de réponse. Il ne reproduit pas l'échec et ne démontre pas sa cause ; aucun changement spéculatif de transport n'est fait.

La [reprise des quatre tours ordinaires dans le vrai Chat Go](dialogues/luna-shodan-ordinary-replay.json), premier scénario inchangé de `personality-cases.json`, réussit ensuite intégralement, plante comprise. Les **2 083 tests locaux** et `go vet ./...` passent. Les erreurs de service restent une limite d'exploitation documentée, sans être transformées en réussite dans les rapports.

L'intensité est variable : les provocations explicites du développement produisent des transformations anatomiques absurdes, tandis que la joute Wi-Fi/Lego de réserve reste assez sage. La démonstration de folie peut encore tourner à l'objet aléatoire. Ces limites sont visibles dans les rapports ; cette version est une amélioration à essayer sur Discord, pas une promesse de retrouver exactement SHODAN ou Neuro-sama. Le modèle répète parfois une lubie mot pour mot et peut s'excuser pour un dépassement qui n'a pas eu lieu. Aucun de ces défauts éditoriaux n'est masqué par le verdict Go.

Les principes de rôle explicite, exemples courts et évaluation à chaque modification suivent le [guide de prompting OpenAI](https://developers.openai.com/api/docs/guides/prompting), consulté le 23 octobre 2026. Cela ne garantit pas l'obéissance stylistique à chaque réponse.
