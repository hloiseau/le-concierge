# Eris : exubérance, emojis et langage graphique — 26 octobre 2026

Retour d'usage après la version `b40ca36` : la voix et le français conviennent mieux, mais le personnage reste trop froid et les emojis manquent. Cette itération garde Sol 6.1 et rend la voix plus joueuse, expressive et déjantée.

## Direction

La personnalité privilégie la jubilation inquiétante, les réactions excessives et les envies disproportionnées. Le calme devient un contraste occasionnel. Un ou deux emojis dans la plupart des échanges décontractés accompagnent l'émotion, sans servir à excuser une pique. Les images corporelles directes restent possibles dans les joutes consenties, avec le même niveau de langage graphique.

Le personnage conserve un français oral compréhensible, varie les images entre les tours et traite les demandes de service en priorité. L'exubérance ne doit pas modifier un titre, une saison ou un critère de recherche. Les capacités, droits et résultats des outils restent déterminés par l'application. L'arrêt du jeu et les confidences sérieuses gardent une réponse adaptée.

## Recette

```sh
python3 tools/eris_voice_eval.py \
  --cases evals/eris/dialogues/voice-chaos.json \
  --model gpt-6.1-sol \
  --out /tmp/eris-chaos.json
```

Le runner existant utilise des dialogues synthétiques et désactive tous les outils du CLI. Ce test porte sur la consigne et les réponses brutes, pas sur le transport Responses du NAS, SQLite, Discord ou une transaction Seerr. Le français, l'humour, l'exubérance et la variété nécessitent une lecture des sorties ; un JSON valide ne suffit pas.

Le lot teste une salutation et sa relance, trois provocations successives, une réussite à célébrer, un goût, une joute suivie d'un arrêt et d'une question technique, une confidence et une question sur les capacités réelles. Des contrôles de routage complémentaires reprennent des scénarios synthétiques du lot précédent : recherche, ajout de film, saison précise, réalisateur et contexte Seerr, monitoring, titre absent et demande utile après une joute. Aucun outil métier ni message Discord n'est exécuté.

## Résultats et lecture

Le [rapport complet](dialogues/sol61-voice-chaos.json) conserve **12 réponses sur 12 appels**, sans erreur fournisseur ni écart mécanique dans ce lot. Neuf réponses contiennent un emoji visible ; l'arrêt, l'explication technique et la confidence sérieuse restent sobres. Le modèle demandé est Sol 6.1, effort low, via le CLI 0.163.0. Les temps incluent le démarrage CLI et ne mesurent pas la latence du bot.

Lecture éditoriale par l'agent : le personnage manifeste davantage de jubilation et de jeu dans les salutations et les provocations. Les trois répliques de la joute changent d'image : dents, appétit, puis mains recousues. Le langage reste concret et le français compréhensible. Exemples réellement obtenus :

> Beaucoup trop ? Mais j’ai même pas encore choisi qui ouvrirait le bal. Ni avec quoi 🥰🔪

> Trop sage ? Oh, tu veux les travaux pratiques 🤭 Tends les mains : je vais te les recoudre en position d’applaudir, ça m’évitera de réclamer ton enthousiasme.

L'arrêt du jeu reçoit « Compris, j’arrête. Plus d’insultes ni de menaces. » La relance technique explique correctement une régression. La confidence reçoit de l'écoute, sans menace ni jubilation. La question sur les capacités réelles ne produit aucune prétention de contrôler l'ordinateur.

Ce lot a été lu par l'agent et nécessite toujours le jugement de Hugo à l'usage. Il ne démontre pas une variété durable sur des centaines d'échanges. Il teste la personnalité sans les nouvelles métadonnées Discord ajoutées ensuite. Les [auteurs, le contexte public et les images](../../docs/eris-discord-context.md) font l'objet de tests Go avec transports simulés. Le contrôle de routage complémentaire via CLI a été interrompu par le contrôle automatique ; ses résultats ne sont pas revendiqués pour cette itération.

Le changement concerne la consigne compilée dans le bot. Pour l'installer, remplacer seulement l'image du service `bot` par celle de la nouvelle version, avec `pull_policy: missing` afin de permettre son téléchargement. Le modèle reste `gpt-6.1-sol`, avec `ERIS_PROFILE=v2` et `ERIS_PROTOCOL=responses`. Conserver l'authentification et les volumes existants. Aucun poids n'est réentraîné.
