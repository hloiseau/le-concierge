# Reprise Eris sur l'autre PC — 27 octobre 2026

## Résultat de la récupération sur le PC de déploiement

Le build a été retrouvé et préservé le 27 octobre. Les sources sont maintenant
[poussées dans la PR de conservation #81](https://github.com/hloiseau/le-concierge/pull/81).
Le [bilan de réconciliation](eris-recovery-79.md) donne les preuves, les écarts,
les contrôles et les limites de la candidate. Le checkout d'origine reste intact.
API et bot exécutaient alors `1894fae72274981516692a0b746d97e80fa350f2` ;
aucune bascule n'avait été effectuée pendant la récupération initiale.
Depuis la recette humaine et l'autorisation de Hugo, la PR #82 est fusionnée et
le main `13ffb1b` est [déployé sur le NAS](eris-release-2026-10-27.md).

Les sections suivantes conservent le contexte et la procédure de reprise rédigés
avant cette découverte. Leur mention d'une référence alors introuvable est historique.

## Mission immédiate

Retrouver le code de l'image `eris-concierge:participation-28f46de` utilisée avant
la régression, préserver tout travail local, puis comparer cette référence à
`main`. **La correction de personnalité reste suspendue jusqu'à cette comparaison.**

Un bot qui tourne sur le NAS alors que ses sources ne sont pas poussées et
identifiables est une livraison incomplète. Ne recommence pas : récupère et
versionne le travail existant avant de modifier encore le prompt ou de déployer.
« Testé en local » ne remplace ni un commit, ni un push, ni une PR traçable.

Hugo demande une Eris qui conserve son passé ERIS-01 et son registre femme fatale,
détraqué, cruel et inspiré de SHODAN. Il apprécie une réaction macabre nette et
compréhensible, la reconnaissance des interlocuteurs et des vannes. Il refuse la
punchline automatique, les répétitions, les réponses après chaque acquiescement
et les leçons sérieuses qui remplacent la joute. Les demandes utiles et les vrais
arrêts doivent rester compris. Il faut retrouver le comportement apprécié, puis
évaluer les écarts ; ajouter des consignes au hasard ne résout pas la reprise.

## Points de reprise

| Référence | Ce qu'elle établit | Ce qu'elle n'établit pas |
| --- | --- | --- |
| [Issue #79](https://github.com/hloiseau/le-concierge/issues/79) | Enquête prioritaire et écarts relevés. | Le build local exact n'est pas encore récupéré. |
| [Issue #78](https://github.com/hloiseau/le-concierge/issues/78) | Inventaire complet des futurs outils Hub, NAS, services en ligne et voix. Revue prévue cinq par cinq. | Aucun nouveau lot n'est validé pour réalisation ; tout est en attente. |
| [Issue #62](https://github.com/hloiseau/le-concierge/issues/62) | Validation humaine du 26 octobre : Sol 6.1, réponses implicites, participation spontanée bornée et `/eris-salon`. | Ce texte est un relevé historique, pas une inspection actuelle du NAS. |
| [PR #64](https://github.com/hloiseau/le-concierge/pull/64) | PR en brouillon encore ouverte à la dernière vérification, branche `feat/eris-test-ground`, tête `06bfffb1225682f179c0d2dd6bb3a9b5ed059b5d`. | Elle décrit un ancien déploiement ; sa tête n'est pas identifiée comme la source de l'image participation. |
| [PR #77](https://github.com/hloiseau/le-concierge/pull/77) | Mémoire durable, canon ERIS-01, silence, fenêtre élargie et résumé progressif fusionnés. | Sa CI verte ne valide pas le ton dans les conversations réelles ni l'équivalence avec une image locale antérieure. |

Au contrôle du 27 octobre, `main` pointe sur
`1894fae72274981516692a0b746d97e80fa350f2`. Sa CI complète est
[verte](https://github.com/hloiseau/le-concierge/actions/runs/40505925006) et l'image
publiée est `ghcr.io/hloiseau/le-concierge:sha-1894fae72274981516692a0b746d97e80fa350f2`.
Cette référence décrit le code publié, pas une confirmation de l'image actuellement
exécutée sur le NAS. Relire la tête courante avant de reprendre : elle peut avoir avancé.

Le tag ancien a été rapporté par Hugo dans sa configuration. Le préfixe `28f46de`
n'a pas été résolu par GitHub dans ce dépôt. Il peut désigner un commit resté
local ; l'absence de résultat distant ne prouve pas la perte du code. Un autre
ancien checkout de session contient des modifications non commitées de voix et de
contexte, mais pas cette référence ni la commande de participation : il ne doit
pas être pris pour la référence NAS sans preuve.

## Écarts à examiner

- La participation spontanée et `/eris-salon participation:activer|desactiver|etat`
  sont décrites dans #62, mais absentes du `main` contrôlé. La configuration
  historique prévoyait deux salons autorisés, au plus une intervention toutes les
  cinq minutes par salon, un silence possible et aucune relance sans réponse.
- #62 décrit aussi un comportement public des demandes personnelles qui doit être
  comparé à la version locale et aux contrôles actuels. Ne pas importer des règles
  d'accès supposées : retrouver le code et ses preuves.
- #77 a réécrit une partie de la personnalité en privilégiant davantage le calme
  et la brièveté. Le registre et les exemples diffèrent de ceux de l'image locale
  appréciée. Cela peut contribuer à la régression ; ce n'est pas encore une cause isolée.
- Le contexte, les consignes d'outils, le modèle configuré, les messages ambiants,
  les souvenirs et l'historique réellement transmis peuvent aussi modifier les réponses.
  Comparer la requête complète avec des données synthétiques, pas seulement une constante.

Les retours réels signalent une cruauté plus faible ou obscure et une mauvaise
interprétation de taquineries, même après clarification du contexte. Les
transcriptions Discord et leurs membres ne sont pas reproduits ici et ne doivent
pas devenir des fixtures publiques.

## 1. Préserver le PC avant de synchroniser

Lire ce document et les issues depuis GitHub **avant de faire un pull dans un
checkout qui pourrait contenir le seul exemplaire du travail**. Commencer par ces
lectures dans le dépôt local supposé :

```bash
git status --short --branch
git branch -av
git worktree list
git log --all --oneline --decorate -40
git reflog --all --date=iso -40
git show --stat --oneline 28f46de
git diff --stat
git diff --cached --stat
git ls-files --others --exclude-standard
```

Une commande introuvable ou un commit absent est une observation à noter, pas un
prétexte pour réinitialiser le dépôt. Faire une copie privée indépendante du
dossier de travail, avec son historique et les fichiers non suivis, avant toute
récupération. Elle peut contenir des secrets : elle reste hors du dépôt public.
Un bundle Git ne protège pas les modifications non commitées ni les fichiers non suivis.

Après cette sauvegarde, inventorier le travail utile. Si `28f46de` existe, relever
son SHA complet et créer une branche de secours qui le conserve. Capturer les
changements locaux utiles dans une branche distincte, après inspection des
fichiers : ne pas ajouter aveuglément secrets, bases ou rapports bruts. Pousser
cette branche et ouvrir une PR de récupération, éventuellement en brouillon.
Vérifier qu'elle est lisible depuis GitHub avant de poursuivre.

Effectuer les comparaisons et la réconciliation dans un autre worktree ou clone.
Ne pas écraser le checkout retrouvé avec `main`, ni fusionner toute une ancienne
branche sans examiner les écarts. Rechercher aussi les scripts de build,
Dockerfiles, manifests de sources et notes de déploiement qui expliquent le tag.

## 2. Identifier les images du NAS

Avec l'accès au NAS disponible, lire séparément les références de l'API et du bot :

```bash
docker inspect le-concierge-bot --format '{{.Config.Image}} {{.Image}}'
docker inspect le-concierge-api --format '{{.Config.Image}} {{.Image}}'
docker image inspect eris-concierge:participation-28f46de --format '{{.Id}} {{json .RepoDigests}} {{index .Config.Labels "org.opencontainers.image.revision"}}'
```

Conserver l'image participation et la configuration existante. Un label de
révision peut être hérité d'une image de base dont les binaires ont été remplacés :
il ne suffit pas à prouver la source exécutée. Confronter scripts de construction,
SHA des sources et empreintes des binaires ; si nécessaire, conserver une archive
privée de l'image et récupérer les binaires pour comparaison.

Relever uniquement les réglages nécessaires : profil Eris, modèle, protocole,
activation Message Content, paramètres de participation et présence des volumes
mémoire/authentification. Ne pas afficher un `docker inspect` complet ou toutes
les variables d'environnement : ils peuvent contenir des secrets. Les chemins,
identifiants et configurations privés sont conservés dans les notes d'exploitation
privées, avec une synthèse nettoyée dans le dépôt.

La version précédemment validée est décrite comme v2 / Responses / `gpt-6.1-sol`.
Confirmer l'état actuel avant de changer quoi que ce soit. Ne pas relancer une
connexion OAuth, effacer une mémoire ou recréer une clé Plex pour résoudre un
écart de version.

## 3. Réconcilier puis recetter

La branche de correction doit partir d'une source identifiée et relier #79. Elle
doit distinguer les comportements récupérés, les changements conservés de #77 et
les adaptations nécessaires. La nouvelle mémoire à préserver comprend :

- canon permanent ERIS-01 ; notes/épisodes SQLite sourcés, cloisonnés et corrigibles ;
- jusqu'à quarante échanges courts, budget de texte et résumé progressif persistant ;
- oubli transitif, absence d'autorisation média par les souvenirs et suivi des livraisons ;
- silence sur les clôtures de conversation et contexte Discord avec identités vérifiées.

Ces éléments ne doivent pas être retirés implicitement par un retour de fichiers
anciens. Pour toute bascule, sauvegarder la configuration et les bases de façon
cohérente ; vérifier la compatibilité de schéma avant retour arrière. Ne pas
restaurer une vieille base ou supprimer des tables pour faire fonctionner un ancien bot.

Recette à préparer avec des échanges **synthétiques** et plusieurs interlocuteurs :

1. Salutation puis provocation banale : cruauté compréhensible et naturelle, sans
   explication du sous-entendu ni chute forcée.
2. Taquinerie entre participants puis clarification explicite : reconnaître le
   registre, sans transformer une accusation en fait personnel ou en souvenir,
   ni prolonger une leçon après clarification.
3. Demande réelle d'arrêt ou détresse : arrêt du jeu et réponse adaptée.
4. Changement de sujet, acquiescement, départ : pas de répétition ni relance indue.
5. Mentions, réponses implicites, participation bornée et réglage `/eris-salon`.
6. Demande média, choix textuel, monitoring, rappel après redémarrage et oubli.

Comparer la référence récupérée et la candidate avec le même modèle et des
contextes contrôlés. Séparer session neuve et continuité avec mémoire. Une sortie
JSON valide ou un mot macabre ne vaut pas une réussite éditoriale. Les anciens
essais Sol ont surtout établi des contrôles mécaniques ; le retour humain a
montré une limite de cette validation. Les timings CLI ne mesurent pas le NAS.

Les essais précédemment autorisés utilisaient Sol 6.1 et des données synthétiques.
Respecter la portée de cette autorisation ; ne pas transmettre les conversations
Discord réelles. Les nouveaux rapports bruts avec prompts complets sont restés
locaux après refus du contrôle automatique de publication. Publier des bilans
nettoyés et des fixtures synthétiques, pas ces rapports bruts.

## 4. Terminer avec une livraison traçable

Lire [AGENTS.md](../AGENTS.md) et [CONTRIBUTING.md](../CONTRIBUTING.md). Pour le
travail destiné au NAS : commit, push vérifié et PR doivent précéder le déploiement,
sauf instruction explicite contraire de Hugo. Privilégier la publication CI et un
tag immuable plutôt qu'un build local isolé. Documenter les preuves réellement
obtenues ; ne pas promettre que la CI garantit le ton ou l'état de production.

La note de livraison contient, sans secrets : source complète du build, PR et état
de fusion, images/digests API et bot, profil/modèle, contrôles, migrations et retour
arrière. Si une branche de travail est déployée pour essai, la nommer explicitement,
garder la PR à jour et réconcilier ensuite les écarts. Vérifier que les commits
annoncés existent sur GitHub et signaler tout travail encore seulement local.

À l'issue de cette reprise, mettre à jour #79 et ce document avec la référence
retrouvée et l'explication des écarts. #78 reste en attente. Le déploiement actuel
et la récupération du build participation ne sont pas réalisés par ce document.
