# Eris sur Discord

Eris utilise des [outils natifs et des réponses libres](eris-natural-requests.md). Le modèle résout les demandes avec le catalogue et le contexte ; il pose sa propre question si une ambiguïté reste réelle. Les anciens menus, confirmations et réponses conversationnelles préfabriquées ont été retirés.

## Discussion et mémoire

`@Eris Salut` ouvre un échange public. Une mention seule reçoit aussi une réponse du modèle. Le bouton **Répondre** de Discord et les [relances vérifiées](eris-v2.md) permettent de continuer. `/eris` ou `/eris message:…` répond en privé. Les messages sont limités à 500 caractères ; deux générations peuvent avancer en parallèle et les relances attendent le tour actif.

Le [profil v2](eris-v2.md) ajoute la personnalité, les noms vérifiés, les références publiques et les [souvenirs sourcés](eris-01-memory.md). Les échanges privés sont séparés par membre ; les échanges publics restent dans leur serveur et leur salon. Les résultats personnels des outils ne sont pas enregistrés comme souvenirs durables de discussion.

`/eris oublier:true` efface les données du membre sur ce serveur, sans supprimer les messages Discord publiés. L'effacement précède la confirmation rédigée par le modèle et fonctionne même si celui-ci est indisponible. `/eris-salon` reste réservé aux administrateurs : il active, désactive ou lit la participation spontanée, puis le modèle explique le résultat vérifié.

Le [guide multi-serveurs](eris-multiple-servers.md) décrit les serveurs autorisés uniquement à discuter. Aucun outil externe n'y est annoncé ou exécuté. Les aides et formulaires des commandes sont des éléments d'interface ; les réponses du personnage sont générées.

## Outils

Lier Discord dans **Mon compte** du Hub après connexion Plex permet de demander un média, vérifier sa présence dans Plex, obtenir une recommandation, consulter ses demandes, suivre une disponibilité ou modifier ses réglages privés. « Demande Madagascar » identifie et demande directement le film ; « tous les films Madagascar » utilise la collection officielle. Aucun numéro ou bouton n'est imposé au membre.

Les mêmes appels natifs servent au [web](eris-web.md), aux [fiches cinéma](eris-cinema.md), au diagnostic, aux anime/manga, aux cadeaux et aux créations Discord. Une simple question ou recommandation n'autorise pas une écriture. Le modèle ne dispose d'aucun outil de suppression de média ou d'annulation d'une demande envoyée.

L'API valide chaque action et sa preuve Discord. Une commande signée ou un message relu auprès de Discord détermine l'auteur, le serveur, le salon et la portée privée ; le modèle ne choisit pas cette identité. Les droits personnels sont revalidés, y compris après une lecture et avant une écriture. Les accès Plex/Seerr, quotas, consentements DM et protections contre les doublons restent ceux du Hub.

Une demande créée ne prouve pas sa disponibilité ni son arrivée future dans Plex. Les résultats incertains sont indiqués au modèle sans réessai aveugle. Les filtres de visionnage, cadeaux et réglages restent privés. Les titres, états et saisons des demandes peuvent être consultés dans un salon, sans données d'autres comptes ni liens Plex personnels.

## Configuration

Dans le conteneur bot, configurer explicitement `ERIS_API_URL`, `ERIS_MODEL`, `ERIS_PROTOCOL` (`chat-completions` ou `responses`) et le fichier secret `ERIS_API_KEY_FILE` monté en lecture seule. L'URL contient le préfixe API, par exemple `/v1` ; le transport ajoute `/chat/completions` ou `/responses`. Le moteur doit accepter les fonctions natives. Le profil `ERIS_PROFILE=v2` et `ERIS_MEMORY_PATH` activent la mémoire décrite ci-dessus. Aucun fournisseur de secours n'est choisi automatiquement.

`DISCORD_BOT_NAME=Eris` synchronise le nom du bot et son surnom au démarrage ; `disabled` désactive cette synchronisation. `DISCORD_MESSAGE_CONTENT=true`, avec l'intent correspondant dans Discord, permet les relances et le contexte ambiant. La participation spontanée reste activée salon par salon. Les permissions nécessaires sont **Voir le salon**, **Envoyer des messages**, **Lire l'historique** et l'envoi dans les fils utilisés.

Sans moteur disponible, le transport affiche un code technique, sans réponse préfabriquée du personnage. Les commandes média existantes et `/aide` restent disponibles indépendamment de la génération.

## Validation

`go test ./...` et `go vet ./...` couvrent les contrats, identités, accès, mémoire, doublons et résultats. Les fonctions natives sont vérifiées en Chat Completions et Responses. Le [laboratoire interactif](eris-interactive-lab.md) et les tests facultatifs avec le modèle réel utilisent des actions simulées ; ils ne remplacent pas une recette volontaire dans Discord. Les anciens bilans datés restent des archives des versions précédentes.
