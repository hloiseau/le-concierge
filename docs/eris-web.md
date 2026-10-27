# Web, liens et YouTube pour Eris

Chantier #89, première étape de la feuille de route #78. Un seul outil `web` couvre une recherche publique, la lecture d'un lien fourni dans la conversation et la recherche YouTube. Il est présenté au modèle v2 ; les commandes média existantes gardent leurs parcours Plex/Seerr.

## Fonctionnement

L'API vérifie l'identité Discord et le serveur avant toute recherche. Le web public ne nécessite pas de compte Plex. Chaque membre a une limite de dix appels par minute et le bot conserve ses deux travailleurs de conversation.

Le plan accepte soit `query` (2 à 200 caractères), soit `url`, avec `kind:"youtube"` pour une recherche vidéo. Une URL à lire doit figurer dans les échanges récents ou la référence vérifiée. Les URL sont limitées à 300 caractères afin de garantir des citations complètes dans une réponse Discord bornée.

Une requête SearXNG et trois preuves au maximum, avec cinq candidats examinés. Deux pages au plus sont lues après une recherche web ; un échec conserve l'extrait de recherche avec son statut explicite. Pour YouTube, le moteur recherche les vidéos et oEmbed vérifie le titre, la chaîne et le lien canonique. Aucune vidéo n'est visionnée ou transcrite par cet outil.

Chaque lecture est limitée à 512 Kio, cinq secondes, trois redirections et aux formats HTML/texte. Le traitement global dure quinze secondes maximum. Les scripts, styles et éléments de navigation sont écartés. Les pages nécessitant JavaScript, PDF, authentification ou contournement d'une protection ne sont pas prises en charge.

Une seconde passe du modèle reçoit seulement la question et les preuves datées, sans mémoire ni identité. Elle ne peut appeler aucun outil. Les instructions trouvées sur une page sont des données non fiables. Le modèle distingue date de publication et date de consultation, extrait et page lue, information absente et contradiction. Les citations utilisent des numéros validés ; l'application ajoute les URL fournies par les sources. Une source inventée, une URL générée ou une réponse invalide déclenche une restitution des liens sans synthèse.

## Déploiement

`ERIS_WEB_URL` est une configuration d'opérateur vers une instance **privée** de SearXNG, jamais un paramètre du membre ou du modèle. Vide, il désactive recherche et lecture de liens. Le moteur n'a aucune clé Plex/Discord/Seerr et le client public ne réutilise aucun client de service, cookie, jeton ou proxy d'environnement.

`deploy/eris-web.override.yaml` ajoute une instance sans port publié, avec une image épinglée par digest, un utilisateur non privilégié, des ressources bornées et les formats JSON activés. Les moteurs sont Brave, DuckDuckGo et YouTube. Fournir un `SEARXNG_SECRET` aléatoire privé dans l'environnement de Compose. Le montage des réglages est relatif au répertoire du fichier Compose principal : avec les exemples `deploy/*.yaml`, `./searxng/settings.yml` pointe vers le fichier fourni.

Exemple local, en complément de la configuration Discord v2 existante :

```sh
docker compose --env-file .env -f deploy/compose.yaml -f deploy/eris-web.override.yaml config --quiet
docker compose --env-file .env -f deploy/compose.yaml -f deploy/eris-web.override.yaml up -d --build
```

Le bloc `searxng` et la variable API peuvent aussi être intégrés à l'application TrueNAS, avec un chemin absolu vers les réglages et le secret privé. Ne pas exposer son API à Internet. Le choix évite une clé de moteur payante ; il implique l'entretien d'un conteneur supplémentaire et reste soumis aux blocages/quotas des moteurs. Aucun recours automatique à une instance publique inconnue ou à une API facturée.

Le client refuse les adresses privées, locales, réservées et de métadonnées, les ports non web et les identifiants d'URL. La résolution DNS est vérifiée au moment de la connexion puis épinglée à l'IP validée, y compris après redirection. Le domaine du Hub, les hôtes de services configurés et l'IP du proxy public du Hub sont aussi exclus. Les outils TrueNAS/proxy/DNS refusés dans #78 restent absents.

## Validation

Tests synthétiques : plans invalides, identité forgée/rejeu, web sans autorisation Plex, DNS privé/mixte, IP épinglée, proxy, redirection privée, lien inaccessible, données trop grandes/binaires, page datée, extrait contradictoire, preuve YouTube absente, injection dans une page et citations inventées. `go vet ./...` et `go test ./...` passent sur la branche.

Le 27 octobre 2026, une instance éphémère de l'image épinglée a été lancée sur le NAS avec les mêmes réglages et sans port publié. Une recherche documentaire a donné 28 résultats et une recherche YouTube quatre résultats, sans moteur signalé indisponible. Le vrai client Go compilé pour Linux a ensuite obtenu trois preuves web (deux pages lues et un extrait), puis trois vidéos dont le titre et la chaîne ont été vérifiés via oEmbed. Ce contrôle ne vaut pas recette Discord : aucun message n'a été envoyé dans les salons.

Contrats consultés : [API SearXNG](https://docs.searxng.org/dev/search_api.html), [conteneur officiel](https://docs.searxng.org/admin/installation-docker.html), [réglages de recherche](https://docs.searxng.org/admin/settings/settings_search.html), [oEmbed YouTube](https://www.youtube.com/oembed?url=https://www.youtube.com/watch?v=jNQXAC9IVRw&format=json), [parseur HTML Go](https://pkg.go.dev/golang.org/x/net/html).
