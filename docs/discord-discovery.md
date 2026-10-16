# Découverte Plex dans Discord

Les commandes `/surprise` et `/recap` nécessitent une connexion Plex au Hub et une liaison Discord dans **Mon compte**. Elles utilisent le jeton du membre et vérifient l’identité du serveur Plex configuré avant de transmettre ce jeton. Les réponses sont privées, même lorsque la commande est lancée dans un salon partagé. Les mentions sont désactivées et les titres neutralisés.

Après mise à jour du bot, réenregistrer les commandes avec la procédure habituelle de `cmd/bot`. Aucune variable supplémentaire n’est nécessaire : les paramètres Discord, Plex et de liaison API existants s’appliquent. Les deux commandes fonctionnent uniquement à la demande ; aucune publication programmée n’est implémentée ou activée.

## `/surprise`

Le filtre facultatif permet de choisir films ou séries ; sans filtre, les deux types sont réunis. Le tirage utilise `crypto/rand` sur les identifiants Plex uniques de tout le catalogue autorisé qui a été lu. Les titres marqués supprimés et les séries explicitement sans épisode sont exclus. Les candidats sont examinés dans un ordre aléatoire sans répétition : un film doit avoir un média dont toutes les parties sont présentes et accessibles selon Plex ; une série doit avoir au moins un épisode répondant à ce critère. Un catalogue vide donne une réponse explicite. Le titre retenu est contrôlé à nouveau avec le jeton du membre avant sa restitution.

La disponibilité correspond aux médias déclarés par Plex : les indicateurs explicites de fichier absent ou inaccessible sont respectés. Aucun fichier du NAS n’est ouvert et aucune lecture vidéo n’est déclenchée. Une modification du catalogue pendant la lecture peut imposer de relancer la commande.

## `/recap`

La période couvre les **sept jours calendaires terminés**, de minuit J−7 inclus à minuit J exclu, dans le fuseau **Europe/Paris**. Les deux dates sont affichées. Exemple : le 16 octobre 2026, la période est du 9 au 15 octobre inclus. Les changements d’heure sont respectés : cette période peut durer 167, 168 ou 169 heures.

Les films et épisodes ajoutés pendant cette période sont sélectionnés avant le regroupement. Chaque série n’apparaît qu’une fois, même si ses épisodes occupent plusieurs pages ; les identifiants d’épisodes répétés sont ignorés. Les droits sont revérifiés sur les épisodes eux-mêmes. Les titres sont triés du plus récent au plus ancien. Les ajouts du jour ne font pas partie du récapitulatif.

## Limites et validation

Les lectures utilisent [`GET /library/all`, les requêtes de médias et la pagination documentées par Plex](https://developer.plex.tv/pms/). Le catalogue est parcouru sans cache partagé : au maximum **25 pages de 200 entrées**, avec **6 secondes** de budget global pour la commande. `/surprise` autorise en plus **20 lectures de métadonnées ou de pages d’épisodes** pour vérifier les candidats tirés ; chaque page contient au plus 20 épisodes. Une limite atteinte donne une erreur explicite. Le total annoncé doit être présent et stable. Un total supérieur à 5 000, une pagination incomplète ou incohérente provoque une réponse explicite ; aucun tirage partiel n’est présenté comme un tirage du catalogue complet. Une indisponibilité ou révocation interrompt le résultat.

Le message tient dans les limites Discord. Si le récapitulatif contient trop de titres, il indique combien restent à consulter dans Plex. Les liens ciblent l’application publique Plex et ne contiennent ni jeton ni adresse privée du serveur.

Les tests locaux avec serveurs simulés couvrent le tirage parmi les positions du catalogue, la pagination et ses plafonds, les filtres, les doublons entre pages, les bornes de période, les changements d’heure, l’isolation des jetons, la révocation, les signatures Discord, les replays, la suppression de compte pendant une lecture et les limites de message. Un essai sur le serveur Plex réel et dans Discord reste nécessaire avant déploiement.
