# Suivis et préférences avec Eris

Chantiers #84 et #87, cinquième groupe de priorité de #78. Les fonctions métier du Hub livrées dans #19/#20 sont réutilisées : aucune deuxième base de suivis ou boucle de notification propre au modèle.

## Suivis

`follow` résout un titre et éventuellement son année/type dans le catalogue Seerr autorisé. Une saison précise peut être donnée ; sinon le suivi d'une série correspond au premier épisode accessible. Une ambiguïté demande une précision. Le modèle interprète le contexte et peut utiliser un identifiant vérifié obtenu par `lookup_media`.

L'outil appelle le même `createFollow` que le Hub : vérification d'accès, demande active, disponibilité Plex effective, plafond de cent suivis et écriture idempotente. Un titre déjà disponible ne laisse pas une future alerte en attente. La réponse distingue l'enregistrement du suivi et l'envoi réel d'une notification.

Sans demande de DM, le canal est le Hub : la disponibilité y est vérifiée à l'ouverture des suivis, selon le comportement existant. `channel:"discord"` nécessite une demande de DM, le consentement existant aux notifications/DM et l'activation du canal sur le serveur. Créer un suivi n'active jamais ces consentements.

`follows` renvoie les suivis personnels et leurs identifiants réels au modèle. `unfollow` retire un suivi appartenant au membre, sans annuler de demande Seerr ni supprimer de média. Le modèle identifie le suivi avec la conversation et les outils ; il ne présente aucun menu numéroté ou bouton. Si une précision manque réellement, il la demande dans ses propres mots. Identité et autorisation restent revalidées avant écriture.

## Préférences

Les réglages sont consultés et modifiés avec `/eris message:…` en privé. Un échange public renvoie vers ce parcours avant de lire les réglages du compte. Le modèle doit distinguer une consultation d’une modification explicitement demandée ; aucun routeur lexical ne réinterprète son appel.

Le plan `preferences` sans réglage lit les valeurs courantes. Avec `setting`/`value`, il change un réglage explicite par tour : notifications générales, DM, rappels de retraits non vus, relances de demandes disponibles non vues, jours avant retrait, plafond du récapitulatif ou plage d'heures calmes en Europe/Paris. La plage est modifiée atomiquement. Une valeur ou une intention ambiguë demande une précision.

Les règles de validation et la sauvegarde du Hub restent la source de vérité. Les champs non demandés sont conservés. L'enregistrement attend le verrou de livraison, revérifie identité/autorisation sous verrou, puis relit les données avant confirmation. Désactiver un réglage reste possible lorsque le service correspondant n'est plus configuré ; son activation exige les prérequis du Hub. Aucun abonnement ni DM n'est activé par défaut.

## Validation

Tests : fonctions natives et résultats structurés, champs/valeurs non autorisés, preuve Discord vérifiée, retrait distinct d'une suppression média, doublons, média déjà disponible, panne, révocation avant écriture, isolation des membres, consentement DM et écriture/relecture des réglages. Les tests existants des canaux, heures calmes, plafonds et livraisons restent applicables.

Les contrôles de création/modification utilisent des comptes et services synthétiques. Aucun suivi, réglage, DM ou message Discord réel n'a été créé pour cette recette. La réception effective d'une nouvelle alerte reste à observer après déploiement, avec un suivi volontaire du membre.
