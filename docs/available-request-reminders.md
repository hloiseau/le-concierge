# Relance facultative des anciennes demandes non vues

Issue [C26](https://github.com/hloiseau/le-concierge/issues/26). Le choix
`availableUnwatched` est distinct des rappels généraux et des DM Discord ; sa
valeur initiale est `false`, y compris après migration d’un compte qui avait
déjà activé ses notifications. Le Hub propose ce choix dans les préférences.

Une relance est éligible entre 30 jours inclus et 90 jours exclus après la plus
récente de deux dates : création de la demande Seerr et ajout de l’élément dans
Plex. Le délai utilise des journées de 24 heures. Il sépare cette relance de la
notification immédiate de disponibilité de Seerr. Activer l’option ne relance
pas tout l’ancien catalogue hors de cette fenêtre.

Les preuves sont relues avec le compte connecté :

- liste complète des demandes du propriétaire Seerr vérifié ; statuts en
  attente, approuvé ou terminé, jamais refusé/supprimé ; date de création et
  `media.ratingKey` ou `media.ratingKey4k` requis pour présélectionner les
  candidats sans lire les métadonnées des autres contenus de la bibliothèque ;
- présence positive dans `/library/all?unwatched=1`, avec le jeton Plex personnel ;
- métadonnées de l’élément exact : même clé, même GUID, même `addedAt`, fichier
  jouable, absence de compteur de lecture ou de progression ; édition ambiguë exclue.
  La lecture personnelle demande `checkFiles=1` et `checkFileAvailability=1`,
  conserve `skipRefresh=1` et exige `exists` et `accessible` positifs pour toutes
  les parties d’une version ; des champs absents ne valent pas confirmation ;
- correspondance exacte TMDB, et, pour un épisode, métadonnées de la saison et
  de la série confirmant l’ascendance et la bibliothèque ;
- aucun début ni achèvement de lecture dans l’historique personnel Tautulli
  pour cet élément et son GUID, même si Plex le marque actuellement non vu ;
- aucune appartenance de l’élément, de sa saison ou de sa série à une collection
  Maintainerr active avec une action de retrait. Ce dernier filtre est
  conservateur : il exclut aussi les dates inconnues et les échéances au-delà de
  30 jours, que la page des départs n’affiche pas nécessairement.

Une série produit au maximum une ligne par demande et saison, désignant un
épisode précis dont les preuves sont établies. Elle ne prétend jamais que toute
la saison est disponible ou jamais vue. La disparition de cet épisode ne
déclenche pas une seconde relance pour un autre épisode déjà couvert par la
même demande. Les demandes refusées, retirées, les sources en erreur et les
droits révoqués ne peuvent pas alimenter une ancienne liste de secours.

La réponse et l’affichage distinguent la date d’ajout de toute échéance :

```json
{
  "id": 1,
  "title": "Exemple de film demandé",
  "type": "movie",
  "reason": "available_unwatched",
  "milestoneDays": 0,
  "availableAt": "2026-09-01T10:00:00Z",
  "sentAt": "2026-10-11T10:00:00Z",
  "url": "https://app.plex.tv/desktop/#!/server/example/details?key=%2Flibrary%2Fmetadata%2F1"
}
```

Le libellé est « Ajouté dans Plex le… ». `availableAt` reprend le `addedAt`
de Plex, pas la date de demande, de partage avec cet utilisateur, de première
observation du Concierge ou une date de suppression. `estimatedAt` est absent.

La relance réutilise `reminder_outbox`, le récapitulatif quotidien existant,
les heures calmes de Paris, le plafond commun d’éléments et la file Discord
durable. Les départs imminents passent avant les relances dans ce plafond.
La clé de déduplication dépend du propriétaire, du serveur, de la demande et de
la saison. Les décisions expirent à la fin de la fenêtre ; leur rétention de
90 jours supplémentaires empêche qu’une purge les rende à nouveau éligibles.
Le worker et l’inbox relisent les preuves ; le compte, le grant, le consentement
et la durée de validité sont revérifiés avant livraison. La révocation et les
reprises suivent les mêmes règles que les autres [DM](discord-reminders.md).
Après création du canal privé Discord, les preuves distantes des relances sont
relues une seconde fois, hors verrou de session, puis l’autorisation locale est
revérifiée. Cette lecture dispose d’au plus quatre secondes dans le budget
global de cinq secondes de l’envoi ; un dépassement reporte la livraison sans
envoyer le contenu. Un épisode témoin remplacé avant livraison actualise la
ligne en attente tout en conservant les tentatives et le délai de reprise.

Budget supplémentaire lorsque cette option est activée : 18 secondes maximum,
1 000 demandes Seerr au plus, 5 000 résultats Plex au plus, et 100 lectures de
métadonnées au plus, parents compris. Un dépassement rend l’observation
incomplète et reporte la livraison. Les éléments hors de la fenêtre sont
écartés avant ces lectures ; après preuve personnelle d’un épisode et de ses
parents, les autres épisodes du même groupe déjà retenu ne consomment plus
ce budget. Une autre saison non encore vérifiée conserve ses propres contrôles.
Aucun jeton n’est persisté et aucun appel
amont ne modifie les médias.

Sources primaires : [API Plex Media Server](https://developer.plex.tv/pms/),
[état vu/non vu Plex](https://support.plex.tv/articles/201018487-mark-as-watched-or-unwatched/),
[contrat MediaRequest Seerr](https://docs.seerr.dev/api/get-media-request/),
[modèle et routes Seerr](https://github.com/seerr-team/seerr/blob/develop/server/routes/request.ts).
Les contrats Maintainerr 3.30.1 et Tautulli 2.18.2 restent ceux des rappels de
départ. Les tests utilisent des réponses synthétiques : aucun DM réel ni appel
au NAS n’a été effectué pour valider cette évolution.
