# Découverte Plex dans Discord

Les commandes `/nouveautes`, `/surprise` et `/recap` nécessitent une connexion Plex au Hub et une liaison Discord dans **Mon compte**. Elles utilisent le jeton du membre et vérifient l’identité du serveur Plex configuré avant de transmettre ce jeton. Les réponses sont privées par défaut. Les mentions sont désactivées et les titres neutralisés.

Après mise à jour du bot, les commandes sont réenregistrées au démarrage si son token est configuré ; la procédure ponctuelle de `cmd/bot` reste disponible. Aucune variable supplémentaire n’est nécessaire : les paramètres Discord, Plex et de liaison API existants s’appliquent. Ces commandes fonctionnent uniquement à la demande ; aucune publication programmée n’est activée.

## Partager dans le salon

Utiliser `/nouveautes public:true`, `/surprise public:true` ou `/recap public:true` pour afficher le résultat aux autres membres du salon. Le filtre `type` de `/surprise` reste utilisable. Sans cette option, ou avec `public:false`, la réponse reste privée.

Le partage contient uniquement les titres et informations du catalogue, sans lien Plex personnel ni boutons de navigation. `/nouveautes public:true` partage la première sélection ; la navigation reste disponible en mode privé. Les demandes personnelles, comptes, livres et diagnostics ne proposent pas cette option. Les problèmes de liaison, d’accès ou de source restent privés et ne produisent aucune publication.

L’API autorise séparément le contenu public après les vérifications d’identité et d’accès. Le bot acquitte toujours en privé, termine ce message privé, puis publie un message de suivi dans le salon ; il confirme le résultat de l’envoi en privé. Cet ordre respecte [le comportement des réponses différées Discord](https://docs.discord.com/developers/interactions/receiving-and-responding#followup-messages), dont la visibilité ne peut pas être changée après création. Une erreur réseau après publication peut laisser l’état de l’envoi incertain : le message privé invite alors à vérifier le salon avant de réessayer.

## `/surprise`

Le filtre facultatif permet de choisir films ou séries ; sans filtre, les deux types sont réunis. Le tirage utilise `crypto/rand` sur les identifiants Plex uniques de tout le catalogue autorisé qui a été lu. Les titres marqués supprimés et les séries explicitement sans épisode sont exclus. Les candidats sont examinés dans un ordre aléatoire sans répétition : un film doit avoir un média dont toutes les parties sont présentes et accessibles selon Plex ; une série doit avoir au moins un épisode répondant à ce critère. Un catalogue vide donne une réponse explicite. Le titre retenu est contrôlé à nouveau avec le jeton du membre avant sa restitution.

La disponibilité est vérifiée par une lecture personnelle du détail avec `checkFiles=1`, `checkFileAvailability=1` et `skipRefresh=1`. Chaque partie d’au moins une version doit porter les indicateurs `exists` et `accessible` à `true` ou `1` ; des champs absents ne valent pas confirmation. Une liste d’épisodes sert seulement à sélectionner une clé, puis le détail de cet épisode est relu dans le budget annoncé. Le Concierge ne télécharge aucun fichier et ne lance aucune lecture vidéo. Une modification du catalogue pendant la vérification peut imposer de relancer la commande.

## `/recap`

La période couvre les **sept jours calendaires terminés**, de minuit J−7 inclus à minuit J exclu, dans le fuseau **Europe/Paris**. Les deux dates sont affichées. Exemple : le 16 octobre 2026, la période est du 9 au 15 octobre inclus. Les changements d’heure sont respectés : cette période peut durer 167, 168 ou 169 heures.

Les films et épisodes ajoutés pendant cette période sont sélectionnés avant le regroupement. Chaque série n’apparaît qu’une fois, même si ses épisodes occupent plusieurs pages ; les identifiants d’épisodes répétés sont ignorés. Les droits sont revérifiés sur les épisodes eux-mêmes. Les titres sont triés du plus récent au plus ancien. Les ajouts du jour ne font pas partie du récapitulatif. Le récapitulatif décrit les ajouts : il exclut les fichiers explicitement absents ou inaccessibles dans la liste, et conserve les ajouts dont les indicateurs de fichier sont omis, sans annoncer leur disponibilité de lecture.

## Limites et validation

Les lectures utilisent [`GET /library/all`, les requêtes de médias et la pagination documentées par Plex](https://developer.plex.tv/pms/). Le catalogue est parcouru sans cache partagé : au maximum **25 pages de 200 entrées**, avec **6 secondes** de budget global pour la commande. `/surprise` autorise en plus **20 lectures de métadonnées ou de pages d’épisodes** pour vérifier les candidats tirés ; chaque page contient au plus 20 épisodes. Une limite atteinte donne une erreur explicite. Le total annoncé doit être présent et stable. Un total supérieur à 5 000, une pagination incomplète ou incohérente provoque une réponse explicite ; aucun tirage partiel n’est présenté comme un tirage du catalogue complet. Une indisponibilité ou révocation interrompt le résultat.

Le message tient dans les limites Discord. Si le récapitulatif contient trop de titres, il indique combien restent à consulter dans Plex. Les réponses privées peuvent contenir des liens vers l’application Plex, sans jeton ni adresse privée du serveur ; ces liens sont retirés du résultat partagé.

Les tests locaux avec serveurs simulés couvrent le tirage parmi les positions du catalogue, la pagination et ses plafonds, les filtres, les doublons entre pages, les bornes de période, les changements d’heure, l’isolation des jetons, la révocation, les signatures Discord, les replays, la suppression de compte pendant une lecture et les limites de message. Le partage vérifie aussi l’option explicite, les commandes autorisées, l’absence de publication sur erreur, le contenu public, l’ordre des appels et les échecs d’envoi. Aucun message Discord réel n’est envoyé par ces tests.
