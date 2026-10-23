# Demandes Eris sans confirmations redondantes

Retour de Hugo du 23 octobre 2026 : Eris demandait de choisir un titre, puis de confirmer ce même choix. La sélection en langage naturel vaut désormais validation de la demande.

- « Demande Arrival » : si Seerr renvoie un seul résultat correspondant exactement au titre explicitement demandé, la demande part directement.
- Plusieurs résultats ou un titre approchant : Eris affiche les candidats. « Prends le deuxième », le titre ou « celui de 2021 » envoie le choix sans redemander « tu confirmes ? ».
- Une recommandation seule n'envoie rien. Le texte indique comment demander un des titres proposés ; la sélection doit correspondre entièrement à un choix offert.
- Une saison manquante reste une vraie question. L'envoi direct d'une série exige une saison unique explicitement nommée ; sinon les saisons proposées sont visibles avant sélection.
- « Non », « annule », une phrase négative/conditionnelle, une réponse ambiguë, un choix périmé ou celui d'un autre membre ne créent pas de demande.

La décision d'envoi direct est prise par le code à partir du message Discord relu ou de la commande signée et des métadonnées Seerr. Le modèle ne fournit ni consentement, ni identifiant de membre, ni jeton de confirmation. Le titre, l'année et les saisons ne sont pas devinés par ce raccourci. Une formulation non reconnue conserve une clarification visible, au lieu de multiplier les confirmations ou d'interpréter librement le consentement.

Le chemin d'écriture reste commun : identité/Plex/Seerr et droits revalidés, jeton consommé avant le POST, aucune relance automatique après une réponse incertaine. Choisir ou annuler consomme l'ensemble des candidats de cette proposition. Les anciennes confirmations et les boutons historiques continuent à fonctionner.

Les tests utilisent de faux transports Discord/Seerr : titre exact, mauvaise année, homonymes, saison explicite/manquante/modifiée, négation, condition, révocation, réponse à une autre conversation, sélection par année, concurrence, annulation, preuve rejouée et résultat de POST incertain. Aucun ajout réel de média ni message Discord de test.
