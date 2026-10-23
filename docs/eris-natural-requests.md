# Demandes Eris sans confirmations redondantes

Retour de Hugo du 23 octobre 2026 : Eris demandait de choisir un titre, puis de confirmer ce même choix. La sélection en langage naturel vaut désormais validation de la demande.

- « Demande Arrival » : si Seerr renvoie un seul résultat correspondant exactement au titre explicitement demandé, la demande part directement.
- Plusieurs résultats ou un titre approchant : Eris affiche les candidats. « Prends le deuxième », le titre ou « celui de 2021 » envoie le choix sans redemander « tu confirmes ? ».
- Une recommandation seule n'envoie rien. Le texte indique comment demander un des titres proposés ; la sélection doit correspondre entièrement à un choix offert.
- Une saison manquante reste une vraie question. L'envoi direct d'une série exige une saison unique explicitement nommée ; sinon les saisons proposées sont visibles avant sélection.
- « Non », « annule », une phrase négative/conditionnelle, une réponse ambiguë, un choix périmé ou celui d'un autre membre ne créent pas de demande.

La décision d'envoi direct est prise par le code à partir du message Discord relu ou de la commande signée et des métadonnées Seerr. Le modèle ne fournit ni consentement, ni identifiant de membre, ni jeton de confirmation. Le titre, l'année et les saisons ne sont pas devinés par ce raccourci. Une formulation non reconnue conserve une clarification visible, au lieu de multiplier les confirmations ou d'interpréter librement le consentement.

Le chemin d'écriture reste commun : identité/Plex/Seerr et droits revalidés, jeton consommé avant le POST, aucune relance automatique après une réponse incertaine. Choisir ou annuler consomme l'ensemble des candidats de cette proposition. Les anciennes confirmations et les boutons historiques continuent à fonctionner.

Eris ne peut ni annuler une demande déjà envoyée dans Seerr ni supprimer un média Plex. Elle explique cette limite dans le salon et renvoie vers un administrateur, sans demander le titre ni consulter le compte à la place. Annuler une demande Seerr ne retire pas un fichier Plex. Les formulations explicites de suppression sont interceptées dans le chat et revérifiées sur le texte Discord côté API avant tout outil ou renvoi en privé. Un titre seul après cette explication conserve l'intention de suppression ; une nouvelle demande explicite permet de changer d'intention. « Annule » pendant un choix en cours continue d'abandonner ce choix. Les listes personnelles et filtres de visionnage restent privés.

Les tests utilisent de faux transports Discord/Seerr : titre exact, mauvaise année, homonymes, saison explicite/manquante/modifiée, négation, condition, révocation, réponse à une autre conversation, sélection par année, concurrence, annulation, preuve rejouée et résultat de POST incertain. Aucun ajout réel de média ni message Discord de test.

## Régression suppression et style — 23 octobre 2026

La consigne ne décrivait pas assez clairement l'impossibilité de supprimer et le routeur acceptait un plan d'ajout ou de consultation pour une phrase de suppression. Le test `TestRemovalExplainsLimitsWithoutOfferingAnAddition` reproduisait ces deux chemins et la relance inutile « donne-moi le titre ». Il passe avec le garde-fou, y compris quand le modèle fournit un mauvais plan. Les tests couvrent aussi la suite du dialogue, une réponse citée, la révocation des choix dans le chat, un changement explicite vers l'ajout, la preuve Discord relue par l'API et les intentions d'oubli distinctes.

La personnalité demande maintenant des expressions naturelles, des images compréhensibles liées au sujet, et une correction simple après une erreur. Le macabre et l'excès restent possibles ; cela ne garantit pas la qualité de chaque vanne.

Le [rejeu Luna](../evals/eris/dialogues/removal-luna.json) conserve 14 tours : 13 réponses et un échec HTTP 503 sur une paraphrase. Les quatre formulations explicites de suppression sont traitées sans appel au modèle ; aucun outil média n'est exécuté dans le scénario de suppression. Le [contrôle ciblé de la paraphrase](../evals/eris/dialogues/removal-luna-paraphrase-recheck.json) réussit ensuite sur deux tours. Aucun effacement, ajout média ou message Discord réel n'est envoyé. Les réponses de style sont plus compréhensibles dans cet échantillon, mais le pétage de pile retombe encore sur un grille-pain : la variété reste à juger en usage.
