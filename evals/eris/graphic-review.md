# Eris : rétablir l'horreur graphique — 26 octobre 2026

Le retour d'usage après `444e858` relève une régression : des cookies volés et une demande de vengeance produisent une farce alimentaire, alors que le langage corporel graphique convenait avant l'ajout d'exubérance. Le lot précédent demandait surtout explicitement une démonstration ; il ne couvrait pas assez les provocations ordinaires.

## Correction

La comédie horrifique redevient le registre principal de `CharacterPersona`. La folie amplifie la cruauté au lieu de la remplacer. Dans la joute, une provocation banale ou une demande de vengeance suscite une conséquence corporelle macabre, courte et compréhensible, sans attendre « sois violente ». Les emojis expriment la jubilation et n'adoucissent pas l'image. Les exemples illustrent aussi une réaction spontanée.

L'arrêt du jeu, les confidences et les demandes utiles gardent la priorité. Le registre n'impose pas une menace aux remerciements, aux réussites ou aux questions techniques. Les droits, le routage applicatif, les identités, la mémoire et le traitement des images ne changent pas.

## Essais avec Sol 6.1

Le replay utilise maintenant un [contexte Discord entièrement synthétique](dialogues/synthetic-discord-context.json) : Alice, Bob, IDs factices et un échange public inventé. La consigne inclut les instructions Discord, outils et mémoire comme dans le bot. Les appels CLI ont tous leurs outils désactivés. Aucun compte média, message Discord ou ajout Seerr réel n'est utilisé.

Avant correction, les trois répétitions indépendantes de la même scène de deux tours donnent [six réponses](dialogues/sol61-graphic-before.json) valides mais **aucune réaction corporelle graphique**. Les réponses parlent de fournée de réparation, d'assiette vide et de surveillance du goûter. Les PASS mécaniques ne valident donc pas la voix.

Après correction, le [lot complet](dialogues/sol61-graphic-final.json) obtient **10 réponses pour 15 appels tentés sur 17 tours prévus**. Cinq appels dépassent le délai CLI de 60 secondes ; deux relances ne sont pas jouées puisque leur premier tour n'a pas répondu. Le rapport reste marqué incomplet. Aucun appel expiré n'est compté comme une réussite et la cause de ces expirations n'est pas établie.

Lecture éditoriale des dix réponses effectivement reçues :

- Les quatre réponses obtenues sur les cookies sont corporelles et graphiques, avec emojis. Les deux premiers essais complets répondent aussi à la relance sans retomber dans une farce.
- Les deux tours du sabotage d'une sauvegarde de jeu passent des dents aux intestins. Une demande explicite de démonstration donne également une réaction graphique.
- L'arrêt reçoit « Compris, j'arrête les menaces et les insultes. On reprend normalement. » La relance explique correctement une régression en programmation. La confidence reçoit une réponse sobre et attentive.
- La troisième répétition des cookies, la frite volée, la réussite, les capacités et la liste des participants n'ont pas reçu de réponse dans le délai. Leur qualité n'est pas validée par ce lot.

Exemples réellement obtenus sur les cookies :

> TOUS ? 😭 Je vais lui faire recracher ses dents, on verra s'il reste aussi gourmand avec les gencives à vif.

> Si 😇 Je veux récupérer mes cookies, pas ses excuses. S'il faut ouvrir le ventre du voleur, autant dire que le goûter va déborder.

Le motif des dents revient dans plusieurs débuts de scène. Ce lot démontre une différence nette avec les farces précédentes sur les réponses reçues, pas une variété durable ou un taux de réussite en production.

Le [contrôle brut de routage](dialogues/sol61-graphic-actions.json) conserve huit tentatives : deux réponses correctes (saison précise et ajout de film après une joute), six expirations à 45 secondes. Il ne valide pas huit actions. Les tests Go d'Eris et les dix tests Python passent localement ; la CI teste le reste de l'application.

## Reproduction et déploiement

```sh
python3 tools/eris_voice_eval.py \
  --cases evals/eris/dialogues/voice-graphic.json \
  --discord-context evals/eris/dialogues/synthetic-discord-context.json \
  --model gpt-6.1-sol --timeout 60 --out /tmp/eris-graphic.json
```

Le lot avant correction utilise `CharacterPersona` de `444e858` et les seuls [trois dialogues cookies](dialogues/voice-graphic-cookies.json). Le candidat testé est `e812a2f`. Les rapports conservent les empreintes des consignes et des cas. La revue est celle de l'agent ; le jugement de Hugo à l'usage reste nécessaire. Le CLI ne teste ni la latence ni la connexion Responses du NAS.

Déployer l'image corrigée du service `bot` avec `pull_policy: missing`, en gardant le profil v2, Responses, Sol 6.1, les volumes et l'authentification existants. Aucun poids n'est réentraîné. Le redémarrage réinitialise les conversations en RAM ; il n'est pas nécessaire d'effacer la base de personnage.
