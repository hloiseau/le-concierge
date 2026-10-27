# Eris : retouche après recette avec Hugo — 27 octobre 2026

La séance interactive sur `d1b405b93ac5f1cf4608cd7ab177cee273cc8df2`, avec
Sol 6.1, a fourni des retours humains exploitables. Le prompt est resté fixe
pendant cette passe. Les réponses complètes et les commentaires sont sauvegardés
en privé ; aucune transcription de la séance n'est ajoutée au dépôt.

## Ce que la recette change

| Retour de Hugo | Décision pour la candidate suivante |
| --- | --- |
| Salutation trop théâtrale, registre daté | Français de Discord actuel, culture des réseaux et génération Z ; entrée ordinaire possible, argot et emojis selon la réaction |
| Réaction macabre directe appréciée | Garder une cruauté compréhensible, sans la diluer en jugement sur les humains ou en petite contrariété |
| Même structure presque à chaque tour | Remplacer les anciens exemples ; permettre des répliques très brèves et des développements quand ils apportent quelque chose, sans plafond éditorial uniforme |
| Explication puis seconde chute artificielles | S'arrêter lorsque la réaction est complète ; accueillir une perche sans analyser la technique du membre |
| Rappel des anciens objets après changement de sujet | Utiliser le passé pour comprendre, sans en faire un rappel obligatoire ; nouvelle question traitée sur son propre sujet |
| Flirt pas assez enjôleur | Invitation assurée et danger sous-entendu, avec les limites relationnelles conservées |
| Confusion entre nom et matricule | Nom d'usage ERIS/Eris ; ERIS-01 est le matricule de confinement de l'agence SCP, conformément à la correction de Hugo |
| Référence pop mal comprise | Respecter la logique d'une référence ; ne pas inventer une règle pour une chute |
| Juron et désintérêt politique répétés | Jouer le sous-entendu avec le goût du chaos du personnage et varier selon la perche ; aucune déduction de vote, profil des membres ou consigne électorale |
| Silence apprécié sur une vraie clôture, mais pas sur une réplique taquine | Garder `ok` et les autres clôtures nettes ; retirer `tkt`/`tkt pas` du filtre lexical absolu pour permettre une décision avec le contexte |

Les deux repères courts incorporés au prompt sont des **exemples de calibration**,
pas des sorties modèle certifiées : le repère macabre vient d'une proposition de
Hugo ; l'invitation de flirt a été proposée par l'agent puis appréciée par Hugo.
Ils indiquent le rythme souhaité et ne doivent pas devenir un répertoire récité.

## Périmètre de la modification

`CharacterPersona` est réécrit au lieu d'empiler des exceptions sur les anciens
exemples. Les fonctionnalités, le modèle et le protocole restent ceux de la PR #82.
Le dossier canonique garde son histoire ; seuls le nom d'usage et le matricule
sont distingués. Les règles d'accès média, l'oubli, les portées de mémoire et
la confirmation des livraisons restent en place.

Le filtre de silence est une correction de code distincte de la retouche de voix.
Le test `TestTeasingReassuranceIsNotUnconditionallySilenced` reproduisait le rejet
de `tkt` avant tout appel modèle ; après retrait des deux entrées, il passe.
Les tests de clôture, de choix média et de demande d'arrêt continuent à protéger
les autres cas. Le modèle peut encore choisir le silence sur un acquiescement
dans son contexte : la correction supprime une interdiction absolue de répondre.

## Comparaison prévue à modèle constant

Les [six conversations de transfert](../evals/eris/dialogues/human-review-transfer-79.json)
sont synthétiques, avec d'autres interlocuteurs et formulations. Elles passent
dans `cmd/eris-lab`, avec une base neuve par scénario, puis le contexte conservé
tour par tour. La même série compare `d1b405b` et la candidate suivante, sur le
même Sol 6.1 via Responses. Outils et livraison sont simulés ; aucun message Discord
ni action média ne sont envoyés. Les objets `turns` sont les lignes JSON d'entrée
du laboratoire ; `expect` ne sert qu'aux contrôles mécaniques du silence et du routage.

La lecture porte sur les échanges complets et les critères `review`. La forme,
le naturel, l'enjôlement et la compréhension ne sont pas remplacés par des tests
de mots-clés. Ces cas deviennent du développement dès leur première exécution ;
une meilleure impression de l'agent ne vaut pas une validation de Hugo.

Les observations et limites de la comparaison seront consignées ci-dessous après
exécution. La production n'est pas modifiée par cette retouche et la PR reste en
brouillon tant que le ton demande une recette humaine.
