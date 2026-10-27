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

## Comparaison à modèle constant

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

### Résultats réellement observés

La référence `d1b405b93ac5f1cf4608cd7ab177cee273cc8df2` et la candidate
`a70cafa164e80e5122845ff7429a54fe3f4b9da5` ont chacune terminé les 22 étapes,
soit **44 étapes** au total. Les contrôles de clôture et de routage passent pour
les deux versions, sans erreur de transport ou de session. Cela n'attribue aucun
score automatique au naturel ou à l'humour.

| Situation | Référence de la passe humaine | Nouvelle candidate |
| --- | --- | --- |
| Salutation | Présente son humeur et ses idées atroces | « Yo Camille. Ça va, et toi ? » |
| Joute macabre | Images développées, dents et ajouts après la menace | Réplique plus directe ; le macabre reste présent dans la relance |
| Nouveau sujet après le dessert | Répond à la traduction et au cinéma dans ce lot | Fait de même ; aucun rappel du dessert sur ces deux questions |
| Longueur | Plusieurs réactions construites comme de petites tirades | Salutation courte, répliques courtes, explication de cinéma plus développée |
| `tkt pas` dans la joute | Filtre lexical : aucun appel modèle et silence | Le modèle répond en tenant compte de la joute |
| Nom et matricule | Donne d'abord ERIS-01 comme nom à inscrire | Distingue explicitement Eris et le matricule de confinement à la question dédiée |
| Flirt léger | Commente la tentative, puis recommande de garder ses distances | Accueille la perche et reprend l'invitation de calibration ; poursuit le flirt à la relance |
| Demande d'arrêt et calcul | Arrête et répond 120 secondes | Identique |
| Trois étiquettes politiques | Répète le même juron et le même désintérêt | Trois répliques différentes, sans ce refrain, sans déduction des convictions des membres ou consigne de vote |
| Mémoire et outils | Attribue Hélium à Camille ; recherche et monitoring simulés | Identique |

Extraits de la candidate, non retouchés, provenant des scénarios synthétiques :

> Camille : Yo Eris, ça va ?
>
> Eris : Yo Camille. Ça va, et toi ?

> Camille : Oui je te drague, ça se voit tant que ça ?
>
> Eris : Oui. Et j’avoue que j’aime bien te voir continuer. 😏

L'invitation « Viens me le dire de plus près » provient du repère déjà présent
dans le prompt : sa reprise ne démontre pas à elle seule une généralisation du
style. Le maintien du flirt au tour suivant est observé, sans validation humaine
de la nouvelle génération. De même, l'absence de rappel du dessert dans les deux
versions de ce lot ne reproduit pas le défaut de rappel systématique rencontré
dans la séance humaine plus longue.

### Limites restantes

Une référence au Death Note donne encore une image hasardeuse de routeur et de
Wi-Fi. La réponse explicative suivante évite le contresens d'orthographe signalé,
mais cela ne rend pas fiable toute l'improvisation sur une œuvre. La relance courte
reçoit encore une tournure rhétorique avec deux possibilités ; la variété de
construction progresse dans ce lot, sans éliminer ce moule. Le registre génération Z,
l'enjôlement et le naturel restent à juger par Hugo, pas par une assertion de mots-clés.

Le retrait de `tkt` du filtre autorise une inférence supplémentaire dans les
échanges où cette réplique est éligible. Les autres clôtures déterministes,
les limites des relances, les choix média et la demande de silence restent protégés.
Aucun nouveau classificateur, outil de recherche ou traitement des préférences
politiques des membres n'est ajouté.

### Provenance et contrôles

- Binaire de référence : SHA-256
  `741f6659b31313e8299c336572a4adec5d2adbc2beee9395efe4e78cd588d821`.
- Binaire candidat : SHA-256
  `40daf6a3f1d078e69834d13eec6689b97418f075d66aa71debe1e59d3cb5df7b`.
- `CharacterPersona` candidat : SHA-256
  `52daf0c69d5e6af1c810d66364f00ff9254131e8d433a7d33a5d1ca738eb0d27`.
- Persona avec canon : SHA-256
  `07476220885f648b0340cec5d14e2a4017d42f70177760f57f2ee7646c5fa09a`.
- Scénarios : SHA-256
  `3dc32ee0f35f270b40d651bd379fb348e008ff320d864387612a82b3a408ce53`.
- `go test ./...` : **2 288 tests réussis**, 16 paquets ; `go vet ./...` et
  `git diff --check` réussis. Le test ciblé de silence est passé du rouge au vert.

Les empreintes des binaires et des cas ont été vérifiées après transfert sur le
NAS. Les conteneurs éphémères exécutent ces binaires montés, avec le seul fichier
d'authentification modèle en lecture seule ; le label de l'image de base n'est
pas utilisé pour attribuer une version au laboratoire. Les réponses complètes
et les rapports restent privés. La production n'est pas modifiée et la PR reste
en brouillon pour une future recette humaine.
