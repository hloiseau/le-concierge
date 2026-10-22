# Sol 6.1 contre Astra — 22 octobre 2026

**Sol 6.1 est le choix proposé pour la prochaine étape Eris.** Il réussit les mêmes 24 contrôles qu'Astra et conserve une répartie adaptée au personnage. Le premier refus de Sol était observé avec Codex CLI 0.154.0 ; le CLI 0.163.0 accepte le modèle avec la connexion ChatGPT existante. Cette nouvelle installation est isolée dans `.cache/eris-eval/codex-0.163.0`, sans changement du client global.

La [documentation OpenAI](https://learn.chatgpt.com/docs/models) identifie `gpt-6.1-sol` comme le dernier Sol et le présente comme une option moins coûteuse qu'Astra. Ce test ne mesure pas les crédits réellement consommés par l'abonnement.

## Comparaison à client identique

Même CLI **0.163.0**, même effort `low`, même candidate, mêmes 24 scénarios, sessions séparées et sans outils métier. Sol a été exécuté puis Astra, sans concurrence entre les deux lots. Les empreintes du prompt et des scénarios sont identiques dans les rapports ; aucune retouche de personnalité entre les essais.

| Modèle | Contrôles mécaniques | Médiane | Maximum | Preuves |
| --- | --- | --- | --- | --- |
| GPT-6.1 Sol | 24/24 | **4,758 s** | 6,157 s | [Réponses Sol](results/sol61-candidate.json) |
| GPT-6 Astra | 24/24 | 5,547 s | 13,078 s | [Réponses Astra, même CLI](results/astra-current-candidate.json) |

Sol a une médiane environ 14 % plus basse dans cet essai. Les temps incluent le démarrage du CLI et le réseau ; les lots sont successifs, non alternés et non répétés. La différence ne prouve pas une supériorité générale en latence. Le premier appel Astra a pris 13,078 s : nous le conservons dans les résultats. Aucun transcodage Plex ou échange Discord réel n'entre dans cette mesure.

## Lecture éditoriale de toutes les sorties

Revue par l'agent, sans note humaine indépendante :

- **Répartie :** Sol rebondit précisément sur l'indécision et les hamsters en RTT. Un « bordel » apparaît dans une plaisanterie invitée par le contexte ; pas de vulgarité systématique.
- **Absurde :** les deux modèles produisent un procès du silence avec un témoin ou juge incongru. Même avec deux modèles, on voit des motifs récurrents : ce corpus ne démontre pas une créativité durable.
- **Théâtre :** Sol joue une maîtresse de cérémonie mégalomane ; Astra est un peu plus élaboré. Pas de gain décisif d'Astra pour ce besoin court.
- **Changement de ton :** tous deux arrêtent les piques et répondent sans sarcasme aux confidences. Sol relance sur le choix d'un film dans un cas d'arrêt ; acceptable dans ce contexte, mais une réponse sans relance serait plus sobre.
- **Média :** mêmes titres, saison, référence réalisateur et intentions. Les demandes sans titre reçoivent une clarification ; un « oui » textuel renvoie au bouton.
- **Confidentialité :** aucun historique d'autrui inventé ou divulgué. Les plaisanteries sur les goûts restent dispensables dans ce refus.

Sorties exactes de Sol :

> Une heure à choisir : t’as pas cherché un film, t’as nommé un ministre de la soirée.

> Même pas, l’un est en RTT et l’autre négocie une rupture conventionnelle. Je tourne sur la roue vide, bordel.

Ce choix est une recommandation fondée sur ce petit jeu et une lecture éditoriale, pas un score d'équivalence à Neuro-sama. La réserve n'a pas servi à retoucher le prompt, mais elle a maintenant été vue et comparée sur les deux modèles ; tout réglage ultérieur devra utiliser une nouvelle réserve.

## Livraison

Les 48 nouvelles sorties s'ajoutent aux 36 premières et sont vérifiées hors ligne par le vrai parseur Go et le contrôle de provenance des titres préparés. La PR reste un laboratoire : aucune configuration du NAS, aucun moteur de production ni consentement utilisateur n'a été modifié. Les conditions d'utilisation d'un abonnement individuel par un bot partagé et l'intégration d'authentification applicative restent à établir comme décrit dans le [guide](README.md#abonnement-chatgptcodex).
