# Contexte élargi et résumé progressif — Sol 6.1

La fenêtre v2 est passée de six échanges à 80 messages, soit jusqu'à quarante échanges complets courts. Elle est bornée à 32 000 caractères Unicode et se compacte par lots lorsqu'un seuil est dépassé. Le modèle résume alors les messages qui sortent de la fenêtre ; le résumé courant est sourcé, limité et persistant dans SQLite. Le contexte récent prévaut sur un ancien résumé.

## Essais avec le modèle réel

Les échanges de remplissage sont des fixtures de livraisons acceptées, sans appels réseau. Les réponses aux questions et la synthèse utilisent réellement `gpt-6.1-sol` via le CLI autorisé par Hugo. Aucun membre ou message réel du groupe n'est utilisé. Les relevés bruts contenant les prompts complets restent locaux ; seul ce bilan est publié.

Le premier lot réussit avec quatre appels : rappel d'un projet et d'une prochaine étape posés 36 échanges plus tôt, compaction, réponse à partir du contexte compacté, puis réponse à partir du seul résumé sauvegardé dans une base isolée.

Le lot final renforce le cas avec une correction de l'étape en cours. Le test Go réussit sur six appels au modèle : quatre réponses et deux synthèses.

| Phase | Résultat observé |
| --- | --- |
| 36 échanges antérieurs | Cassiopée et le login par mail sont rappelés à partir de la fenêtre brute, sans compaction prématurée. |
| Première compaction | La source initiale quitte la fenêtre récente et entre dans un résumé avec citation exacte. |
| Correction encore récente | Le modèle retient la nouvelle étape, connexion Plex, alors que l'ancien résumé mentionne encore le mail. |
| Deuxième compaction | La correction est intégrée au résumé progressif ; le mail est traité comme une étape abandonnée. |
| Rechargement depuis SQLite | Une nouvelle instance de Chat retrouve le projet et la connexion Plex à partir du résumé persistant. |
| Oubli | L'assertion SQL vérifie que le résumé a été supprimé. |

La base utilisée pour le rechargement contient uniquement le résumé et un message synthétique sans les faits recherchés. Cela empêche le journal d'épisodes de fournir discrètement la réponse à la place du résumé. Le rôle original de chaque citation est ajouté par le code ; un modèle ne peut pas inventer un ID de source accepté.

## Vérifications hors ligne

Les tests vérifient aussi la conservation de quarante échanges courts, les plafonds de texte Unicode, la fenêtre publique élargie, le refus des citations ou champs inconnus, la continuité des sources entre deux synthèses, le repli extractif sur erreur, l'exclusion des résultats d'outils et réponses non livrées, la séparation des portées et l'effacement transitif après redémarrage. L'oubli pendant une synthèse empêche son réenregistrement et la génération d'une réponse à partir de ce contexte périmé. Un résumé ne fournit pas à lui seul un titre autorisé pour une demande média.

Les choix média déjà vérifiés et les suppressions non prises en charge utilisent le repli extractif quand un débordement doit être traité : ils n'attendent pas un appel de synthèse. L'inférence de synthèse ordinaire est bornée à vingt secondes, sans outil média.

## Limites

Les budgets comptent des caractères, pas les tokens exacts de Sol. La requête textuelle v2 complète est bornée à 96 000 caractères, avec une réserve pour une seconde recherche mémoire ; les images ont leurs propres limites. Un résumé de huit points reste une compression avec perte. La validation des citations ne garantit pas toute l'interprétation sémantique du modèle.

Le banc passe par Go Chat et un pont Chat Completions local vers le CLI. Il ne valide ni le Gateway Discord ni Responses sur le NAS. Les timings incluent le lancement du CLI à chaque appel et ne mesurent pas la latence du bot. La session récente en RAM expire toujours après trente minutes d'inactivité ; seul le résumé compacté, les notes et les épisodes sont persistants. Cette recette n'effectue aucun déploiement.
