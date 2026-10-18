# Communauté et roadmap publiées

Cette version publie un instantané **explicitement approuvé**, commun aux pages `/communaute` et `/roadmap` du Hub. « Public » signifie ici consultable par les membres connectés au Hub avec Plex, pas accessible sur Internet sans authentification. Les routes HTTP lisent uniquement le fichier publié. L’exporteur peut tourner dans le processus API sur activation explicite, ou séparément via le CLI, par des lectures Discord/GitHub ; la publication entièrement manuelle reste possible.

La synchronisation porte uniquement sur les fils et issues choisis dans un manifeste. Un forum explicitement sélectionné avec `allThreads: true` inclut ses sujets publics actuels et futurs, actifs ou archivés. Elle relit les permissions à chaque passage, mais n’est pas un flux d’événements instantané : les changements deviennent visibles au prochain rafraîchissement, avec un instantané valable cinq minutes au maximum. Les votes et statuts des discussions ont une seule source : Discord. Les liens vers le fil permettent de consulter et voter avec son identité Discord, qui applique ses droits ; le Hub n’invente ni compteur ni vote local. Les statuts de la roadmap suivent les états et labels GitHub explicitement mappés.

## Sélection et configuration

Monter un fichier JSON en lecture seule et définir :

```dotenv
COMMUNITY_FEED_FILE=/config/community/publication.json
DISCORD_GUILD_ID=33333
COMMUNITY_FORUM_IDS=11111
ROADMAP_ISSUE_IDS=example/project#1
```

Les identifiants ci-dessus sont fictifs. Les listes sont séparées par des virgules, avec au plus 100 forums et 100 issues. Un forum absent de la liste n’apparaît pas ; une issue absente de la sélection ne peut pas être publiée. Le fichier entier est un document de publication : ne jamais y exporter une réponse brute d’API, une discussion privée, un auteur, des jetons, une adresse interne ou des données d’administration. Un titre ou résumé d’issue privée doit être rédigé pour publication avant d’entrer dans ce fichier.

Les références `issueId` servent uniquement à la sélection interne. La réponse HTTP utilise l’identifiant public `id`, jamais le nom du dépôt privé. `linkToIssue: true` publie explicitement le lien GitHub et donc le nom du dépôt ; laisser ce champ absent pour une issue privée. Une dépendance n’est affichée que si son identifiant public figure aussi dans la sélection. Une discussion liée n’est affichée que si son fil figure dans les forums sélectionnés.

## Préparer et vérifier une publication

Le CLI fournit un exemple neutre et valide le même contrat que l’API :

```sh
go run ./cmd/community-feed -example > publication.new.json
go run ./cmd/community-feed -check publication.new.json \
  -guild-id 33333 -forum-ids 11111 -issue-ids example/project#1
```

Adapter les résumés et identifiants avant utilisation. Relire le fichier entier, puis remplacer atomiquement `publication.json` par le fichier validé, sur le même système de fichiers. Monter le dossier en lecture seule dans le conteneur pour que le remplacement soit visible. Le CLI ne publie rien et n’écrit sur aucun service externe. Sous Windows PowerShell, conserver un encodage UTF-8 sans BOM lors de l’écriture de sa sortie JSON.

Le document a la forme suivante (dates d’exemple, à actualiser) :

```json
{
  "version": 1,
  "publishedAt": "2026-10-16T12:00:00Z",
  "expiresAt": "2026-10-16T13:00:00Z",
  "forums": [{
    "forumId": "11111", "threadId": "22222",
    "title": "Une proposition approuvée pour publication",
    "kind": "proposal", "status": "open", "archived": false,
    "updatedAt": "2026-10-16T12:00:00Z"
  }],
  "roadmap": [{
    "issueId": "example/project#1", "id": "example-feature",
    "title": "Une évolution sélectionnée", "status": "planned",
    "dependencies": [], "threadId": "22222"
  }]
}
```

Les sujets utilisent `kind: proposal|problem` et `status: open|planned|in_progress|resolved|declined`. La roadmap utilise `status: planned|in_progress|delivered`, un `summary` facultatif de 1 000 caractères au plus, et `dependencies` contenant des identifiants publics. Une date n’est affichée que si `date: YYYY-MM-DD` et `dateConfirmed: true` sont présents. L’absence de date n’entraîne aucune estimation. Les titres sont limités à 200 caractères, sans HTML ni caractères de contrôle.

## Modification, retrait et durée de validité

Modifier un titre ou statut dans la nouvelle publication remplace l’ancien à la prochaine lecture. `archived: true` conserve le fil en lecture ; retirer son entrée le fait disparaître. Pour retirer toute la publication, retirer le fichier ou le remplacer par des listes vides et des dates valides. Les suppressions de fils, changements de droits et issues dépubliées doivent être répercutés dans cet instantané par son éditeur.

La durée de validité ne peut pas dépasser 24 heures, et la publication ne peut pas être datée dans le futur. Une durée courte réduit le délai possible entre un changement amont et son retrait. Un fichier manquant, expiré, trop gros, mal formé ou contenant un champ inconnu renvoie `503` sans résultat ancien. Le Hub efface son affichage au prochain rafraîchissement, au plus après une minute tant que la page est visible, et relit à son retour au premier plan. Aucun cache persistant ne conserve les résumés.

Le fichier est limité à 256 Kio et 100 entrées par liste. Les deux routes `GET /api/v1/community` et `GET /api/v1/roadmap` partagent une limite de 30 lectures par minute et par adresse cliente. Les paramètres de sélection dans l’URL sont refusés. Ces routes n’écrivent aucun vote et n’appellent pas Discord/GitHub ; seul l’exporteur ci-dessous consomme leurs lectures.

Les tests locaux vérifient les exclusions privées, les références cachées, les dates confirmées, les modifications, archives, retraits, expiration, erreur source, authentification et absence de restauration après déconnexion. Les tests navigateur utilisent uniquement des publications fictives.

## Exporteur synchronisé en lecture seule

Un manifeste distinct de la publication définit les fils choisis et les textes GitHub autorisés. Avec `threadIds`, les nouveaux sujets n’apparaissent qu’après ajout explicite de leur identifiant. Pour approuver tous les sujets publics actuels et futurs d’un forum, remplacer `threadIds` par `allThreads: true` ; les deux modes sont exclusifs. Les forums conservent chacun leur type et leur correspondance de tags. Exemple neutre :

```json
{
  "version": 1,
  "guildId": "33333",
  "forums": [{
    "id": "11111", "kind": "proposal", "threadIds": ["22222"],
    "defaultStatus": "open", "statusTags": {"88888": "in_progress"}
  }],
  "issues": [{
    "issueId": "example/project#1", "id": "example-feature",
    "title": "Résumé approuvé pour publication", "dependencies": [],
    "threadId": "22222", "openStatus": "planned",
    "completedStatus": "delivered", "labelStatuses": {"en cours": "in_progress"}
  }]
}
```

Fournir `COMMUNITY_DISCORD_TOKEN` dans l’environnement de l’exporteur, avec un bot disposant uniquement des lectures nécessaires. `COMMUNITY_GITHUB_TOKEN` est facultatif pour les issues publiques ; pour un dépôt privé, utiliser un jeton limité aux dépôts nécessaires avec la permission `Issues: read`. La sélection par issue est ensuite appliquée par l’exporteur. Ne jamais mettre un secret dans le manifeste ou dans les arguments de commande. Ces variables sont propres au CLI. Le worker intégré réutilise `DISCORD_BOT_TOKEN` déjà fourni à l’API ; il ne reçoit pas de token GitHub supplémentaire et ne peut donc pas lire une issue GitHub privée.

```sh
go run ./cmd/community-feed -sync manifest.json -output /config/community/publication.json
```

Pour un export externe, programmer ce passage toutes les deux minutes avec le planificateur de l’hôte, sans exécutions simultanées. Le répertoire doit être accessible en écriture à l’exporteur et en lecture au serveur ; le fichier temporaire et le fichier cible restent sur le même système de fichiers. Sur Unix, les fichiers sont privés (`0600`) : exécuter l’exporteur avec le même UID que le serveur lecteur. La commande ne démarre pas de tâche persistante et ne réalise aucune écriture distante.

À chaque passage, l’exporteur consulte les rôles du serveur Discord, les forums et les fils sélectionnés. Il exige un forum de type 15 et des fils publics de type 11 dans le serveur et le parent attendus, sans NSFW, spoiler ou métadonnées masquées. Le rôle `@everyone` doit pouvoir voir le forum et son historique ; toute restriction de ces droits par rôle ou membre entraîne son exclusion. Des permissions manquantes ou ambiguës ne sont pas devinées. Avant de publier, les fils retenus sont relus, puis les rôles et permissions des forums sont vérifiés à nouveau. Aucun message, auteur, réaction, vote ou fil privé n’est extrait. `updatedAt` indique ici l’observation, pas une date de modification inconnue. [Canaux et types Discord](https://docs.discord.com/developers/resources/channel), [permissions](https://docs.discord.com/developers/topics/permissions), [lecture des rôles](https://docs.discord.com/developers/resources/guild).

Les tags Discord utilisent leurs IDs stables et une correspondance de statuts définie dans `statusTags`. Sans tag reconnu, seul `defaultStatus` explicitement choisi peut être utilisé ; sinon le sujet est omis. Des tags contradictoires entraînent l’omission du sujet. L’archivage reste visible, les titres publics sont relus et les suppressions sont retirées.

GitHub ne fournit que la présence, l’état et les labels de chaque issue sélectionnée. Les titres et résumés exportés proviennent **toujours du manifeste approuvé**, même pour un dépôt public ; aucun texte privé brut n’est copié. Une issue fermée n’est livrée que si GitHub indique `completed` et qu’une correspondance `completedStatus` a été configurée. Les fermetures `not_planned` ou `duplicate` sont retirées ; une PR n’est pas traitée comme une issue. Des labels de statut contradictoires invalident le passage. [Contrat officiel des issues GitHub](https://docs.github.com/en/rest/issues/issues#get-an-issue).

La sélection est bornée à 20 forums, 50 fils explicitement nommés et 50 issues. La découverte `allThreads` lit la liste des fils actifs du serveur puis les archives publiques de chaque forum choisi, avec au plus cinq pages de 100 archives. Elle déduplique les identifiants avant les lectures individuelles et refuse une publication de plus de 100 fils ; une liste incomplète ou trop volumineuse retire la publication au lieu d’en montrer une partie. Les autres forums et les fils privés ne sont pas publiés. Voir [les fils actifs Discord](https://docs.discord.com/developers/resources/guild#list-active-guild-threads) et [les archives publiques paginées](https://docs.discord.com/developers/resources/channel#list-public-archived-threads). Les lectures utilisent des URLs construites sur les domaines officiels, sans redirection, cinq secondes par appel, 1 Mio par réponse et 45 secondes pour le passage entier. Après découverte éventuelle, les ressources sont lues directement par ID : aucun lien `next` arbitraire ni liste d’issues n’est parcouru. La pagination des archives utilise uniquement le curseur horodaté documenté par Discord. Une réponse `403` ou `404` retire la ressource ; une panne, un contrat invalide ou `429` retire immédiatement l’ancien fichier de publication. Le délai demandé par la source est conservé dans `publication.json.retry-after` et respecté aux prochaines exécutions, sans attente bloquante ni nouvel appel prématuré. Ne pas supprimer ce fichier pour contourner une limite de débit.

Les tests utilisent des transports simulés : permissions de rôle/membre, changement de permissions pendant une lecture, canaux privés et NSFW, tags, titre modifié, archivage, suppression, textes GitHub privés, redirections, bornes, remplacement atomique et reprise après limite de débit. Le 18 octobre 2026, un passage en lecture seule sur les deux forums autorisés a produit une publication valide de deux sujets, sans issue GitHub. La réception des interactions et les votes dans Discord restent une recette distincte.

## Actualisation intégrée à l’API

Cette option évite un planificateur ou un conteneur supplémentaire. Placer le manifeste approuvé dans le volume existant et configurer, par exemple :

```dotenv
COMMUNITY_SYNC_MANIFEST=/app/data/community-manifest.json
COMMUNITY_FEED_FILE=/app/data/community-publication.json
COMMUNITY_FORUM_IDS=11111,44444
ROADMAP_ISSUE_IDS=
```

Le manifeste et la publication doivent être deux chemins absolus distincts. L’UID 65532 doit lire le manifeste et écrire le dossier de publication ; les fichiers produits sont privés (`0600`). La liste des forums et des issues du manifeste doit être incluse dans la sélection configurée, avec le même serveur Discord. Une sélection invalide retire l’ancien fichier sans lire de source supplémentaire.

Le worker effectue un passage dès le démarrage, puis toutes les deux minutes, sans chevauchement ; il s’arrête avec l’API et réutilise les mêmes limites, permissions et délais de reprise que le CLI. Ne pas programmer un export externe sur le même fichier. Une valeur vide désactive le worker et conserve le fonctionnement par publication externe. Les nouvelles discussions apparaissent au passage suivant ; une archive déplacée pendant la lecture peut attendre le cycle suivant. Les pages cessent d’afficher la publication si elle expire ou si une lecture échoue.
