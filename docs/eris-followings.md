# Suivis et préférences avec Eris

Chantiers #84 et #87, cinquième groupe de priorité de #78. Les fonctions métier du Hub livrées dans #19/#20 sont réutilisées : aucune deuxième base de suivis ou boucle de notification propre au modèle.

## Suivis

`follow` résout un titre et éventuellement son année/type dans le catalogue Seerr autorisé. Une saison précise peut être donnée ; sinon le suivi d'une série correspond au premier épisode accessible. Une ambiguïté demande une précision. Le titre doit venir de la conversation récente, pas d'un souvenir ou d'un exemple du prompt.

L'outil appelle le même `createFollow` que le Hub : vérification d'accès, demande active, disponibilité Plex effective, plafond de cent suivis et écriture idempotente. Un titre déjà disponible ne laisse pas une future alerte en attente. La réponse distingue l'enregistrement du suivi et l'envoi réel d'une notification.

Sans demande de DM, le canal est le Hub : la disponibilité y est vérifiée à l'ouverture des suivis, selon le comportement existant. `channel:"discord"` nécessite une demande de DM, le consentement existant aux notifications/DM et l'activation du canal sur le serveur. Créer un suivi n'active jamais ces consentements.

`follows` présente les premiers suivis personnels avec leurs numéros et le lien vers la suite dans le Hub. `unfollow` retire un numéro appartenant au membre ; ce numéro doit avoir été mentionné dans les échanges récents. Le retrait conserve demande Seerr et média Plex. « Retire mon suivi » n'est plus intercepté par l'explication de suppression de médias.

En cas d'homonymes, la réponse propose des choix liés au membre, au salon, à la portée publique/privée et à l'intention initiale pendant dix minutes. « Celui de 2021 » suffit ensuite, sans répéter « préviens-moi ». Un retrait sans numéro propose aussi les suivis à retirer ; « retire le premier » ne devient pas une suppression de média. Le choix est consommé une seule fois et l'identité/autorisation reste revalidée avant écriture.

## Préférences

Les réglages sont consultés et modifiés avec `/eris message:…` en privé. Un échange public renvoie vers ce parcours avant de lire les réglages du compte. Une demande de simple consultation ne peut pas autoriser une écriture proposée par le modèle.

Le plan `preferences` sans réglage lit les valeurs courantes. Avec `setting`/`value`, il change un réglage explicite par tour : notifications générales, DM, rappels de retraits non vus, relances de demandes disponibles non vues, jours avant retrait, plafond du récapitulatif ou plage d'heures calmes en Europe/Paris. La plage est modifiée atomiquement. Une valeur ou une intention ambiguë demande une précision.

Les règles de validation et la sauvegarde du Hub restent la source de vérité. Les champs non demandés sont conservés. L'enregistrement attend le verrou de livraison, revérifie identité/autorisation sous verrou, puis relit les données avant confirmation. Désactiver un réglage reste possible lorsque le service correspondant n'est plus configuré ; son activation exige les prérequis du Hub. Aucun abonnement ni DM n'est activé par défaut.

## Validation

Tests : champs/valeurs non autorisés, intention provenant du message Discord vérifié, titre/numéro issu du contexte, retrait distinct d'une suppression média, doublons, média déjà disponible, panne, révocation avant écriture, isolation des membres, consentement DM et écriture/relecture des réglages. Les tests existants des canaux, heures calmes, plafonds et livraisons restent applicables.

Les contrôles de création/modification utilisent des comptes et services synthétiques. Aucun suivi, réglage, DM ou message Discord réel n'a été créé pour cette recette. La réception effective d'une nouvelle alerte reste à observer après déploiement, avec un suivi volontaire du membre.
