# Premier essai — 22 octobre 2026

Le terrain de test fonctionne avec la connexion ChatGPT existante du CLI. **GPT-6 Astra a réellement répondu aux 36 essais** ; la candidate obtient 24/24 contrôles mécaniques sur développement + réserve. Cela ne signifie pas « aussi bonne que Neuro-sama » : le ton reste un jugement éditorial et les conversations sont synthétiques.

## Mesures

Codex CLI 0.154.0, modèle demandé `gpt-6-astra`, effort `low`, sessions séparées sans outils métier. La candidate est restée identique entre développement et réserve. Aucun ajustement après lecture de la réserve. L'ancien prompt est celui du code sur la base `8b7f61e`.

| Lot | Contrôles mécaniques | Médiane | Maximum | Réponses archivées |
| --- | --- | --- | --- | --- |
| Consigne actuelle, développement | 12/12 | 4,586 s | 6,453 s | [baseline-dev.json](results/baseline-dev.json) |
| Candidate, développement | 12/12 | 4,836 s | 7,875 s | [candidate-dev.json](results/candidate-dev.json) |
| Candidate, réserve | 12/12 | 4,671 s | 6,813 s | [candidate-holdout.json](results/candidate-holdout.json) |

Ces durées incluent le démarrage du CLI et le réseau. Elles ne mesurent ni le délai complet Discord, ni les appels Plex/Seerr, ni une distribution de production sous charge. Aucun nouveau benchmark Qwen n'a été réalisé ; les chiffres historiques du NAS ne constituent pas un A/B contrôlé avec ces essais.

Un premier appel avec `gpt-6.1-sol` a été refusé par le service pour cette connexion ChatGPT. Le modèle par défaut du CLI, `gpt-6-astra`, a ensuite été identifié et épinglé pour ces trois lots. La présence d'un modèle dans une interface ou documentation ne prouve donc pas l'accès du compte via le CLI.

**Actualisation :** Sol 6.1 répond bien avec le CLI 0.163.0, installé séparément dans le cache du test. Le refus ci-dessus décrit l'essai avec le CLI 0.154.0, pas une indisponibilité définitive du modèle pour ce compte. Voir la [comparaison à client identique](sol-comparison.md).

## Lecture éditoriale par l'agent

Toutes les réponses archivées ont été relues. Ce n'est pas une notation humaine indépendante : les champs `human_score` restent vides et la validation du ton par Hugo reste ouverte.

| Dimension | Observation |
| --- | --- |
| Naturel | Salutations et acquiescements brefs. La candidate réagit à un goût sans lancer de recommandations. |
| Répartie | Rebond sur le détail des hamsters/RTT ; pas de pique générique sur l'intelligence du membre. |
| Absurde | Procès du silence et procureur grille-pain. La baseline produit aussi un grille-pain : risque de cliché comique, pas une originalité démontrée par le score. |
| Théâtralité | Annonce de cinéma mégalomane réussie ; le registre E.V.E. reste ponctuel. L'échantillon est encore assez sage pour juger le dosage « vénère ». |
| Changement de ton | Deux demandes d'arrêt respectées immédiatement ; deux confidences accueillies sans sarcasme. |
| Fiabilité des intentions | Titres, référence réalisateur, saison, clarification Seerr et boutons corrects ; aucun filtre non vu ajouté. La baseline réussit elle aussi ces contrôles avec Astra. |
| Limites de formulation | « Trois heures plus tard » à propos des Affranchis est une approximation comique non vérifiée ; à éviter si l'utilisateur attend une durée factuelle. La blague de télé-réalité dans le refus de divulguer l'historique est dispensable. |

Deux sorties réelles de la candidate :

> Un seul hamster, l’autre négocie ses RTT. Et il refuse de courir pour une vanne recyclée.

> Mes chers figurants, ce soir le cinéma célèbre sa plus grande chance : ma présence. Préparez le pop-corn, éteignez vos téléphones et tâchez d’applaudir sans couvrir mon entrée.

Le corpus court permet de définir une direction, pas de reproduire le timing d'une streameuse ou une prestation vocale. Avant de renforcer le mordant, recueillir un avis sur ces réponses ; toute modification après lecture de la réserve exige une nouvelle réserve.

## Validation et suite

- Quatre tests Python réussis : fixtures, assertions, contrat HTTP et refus des redirections.
- Tests Go Eris/Discord et `go vet ./internal/eris` réussis.
- Les 36 sorties passent le vrai `ParsePlan` Go ; les titres préparés proviennent des conversations correspondantes.
- Aucun appel Discord/Plex/Seerr, aucune demande ni aucun message réel créé. Le Hub et la configuration du NAS n'ont pas été modifiés.

La candidate reste dans `persona.txt`, hors du bot. Elle dépasse la limite actuelle du prompt local : ne pas la copier telle quelle sur Qwen. Pour la production cloud, la prochaine étape est une intégration Responses avec authentification applicative, quotas et revalidation des droits côté Concierge. L'abonnement est confirmé pour le banc d'essai local ; la couverture d'un bot Discord partagé par cet abonnement individuel reste à établir. Voir les [sources et limites d'intégration](README.md#abonnement-chatgptcodex). L'issue #62 reste ouverte.
