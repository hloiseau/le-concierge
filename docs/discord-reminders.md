# Rappels privés Discord

Le parcours principal de rappel collectif est désormais le [salon notifications avec mentions des demandeurs Seerr](discord-channel-reminders.md). Les DM décrits ici restent une préférence personnelle facultative.

Les rappels avant un départ estimé et les disponibilités des suivis peuvent être envoyés en message privé Discord, en complément du Hub. Chaque suivi choisit son canal ; les préférences globales autorisent ou désactivent les notifications et les DM.

## Activation

1. Le démarrage applique automatiquement la migration SQLite v6.
2. Fournir `DISCORD_BOT_TOKEN` (ou son fichier secret `DISCORD_BOT_TOKEN_FILE`) au service API et activer `DISCORD_DMS_ENABLED=true`. La valeur par défaut est `false`. Sans jeton, les DM restent désactivés avec un avertissement ; l'API continue de démarrer. Un jeton fourni uniquement au conteneur bot ne suffit pas. Après ajout du jeton à l'API, redémarrer le service.
3. Dans le Hub, l'utilisateur lie son compte Discord vérifié, active ses rappels, puis coche séparément « Recevoir aussi mes rappels en message privé Discord » et enregistre.

L'ancien choix global de notifications ne vaut jamais consentement aux DM. La migration conserve les préférences et l'historique existants ; le consentement DM est initialisé à faux lors de son introduction en v4, puis préservé lors des migrations suivantes. L'activation ne transforme pas d'anciens récapitulatifs du Hub en messages à envoyer. Le processus ne crée une livraison Discord qu'au moment d'un nouveau récapitulatif, lorsque le serveur et le choix individuel l'autorisent.

Les heures calmes et le plafond quotidien de contenus sont communs aux deux familles. Chaque famille produit au plus un récapitulatif par jour : un pour les départs, un pour les disponibilités. Le total de contenus livrés dans les deux reste borné par le plafond partagé, y compris après désabonnement. Les départs couvrent lectures commencées, demandes et suivis non vus confirmés par Plex ; l’option explicite « tous mes contenus non vus » élargit ce ciblage. Les messages présentent les dates de retrait comme des estimations. Le bot doit pouvoir ouvrir un DM avec le compte lié ; un refus ou des DM fermés terminent la livraison sans tentative dans un salon public.

## Vérification et reprise

Avant chaque tentative, le Concierge relit les sources et les accès Plex personnels, puis remplace les titres et liens enregistrés par leurs valeurs fraîches. Les décisions devenues obsolètes sont retirées. Après l'ouverture du canal privé, une dernière vérification contrôle le compte lié, les préférences, la session Plex, les échéances et la réservation du travail avant le POST du message. Les mentions Discord sont désactivées et les aperçus de liens supprimés.

SQLite enregistre atomiquement le récapitulatif du Hub et le travail Discord. Un travail contient un destinataire lié, un instant de récapitulatif, un nonce et son état ; aucun jeton ou corps d'erreur du fournisseur n'y est stocké. Une réservation atomique de 60 secondes évite que deux travailleurs traitent normalement le même travail. Le transport dispose d'un délai total de 5 secondes. Les caches de canaux restent en mémoire et sont bornés à 1 024 entrées.

Les erreurs temporaires suivent un délai de reprise de 1, 2, 4 puis 8 minutes, avec au plus cinq réservations, y compris celles interrompues par un arrêt. Les reprises sont limitées au jour initial du récapitulatif, dans le fuseau Europe/Paris : un travail non livré expire à minuit pour éviter son cumul avec celui du lendemain. Les réponses 429 reportent la reprise d'au moins le `retry_after` / `Retry-After` annoncé, accepté entre 1 seconde et 24 heures. Un délai malformé ou supérieur à cette borne arrête le transport jusqu'au redémarrage. Un jeton refusé (401) arrête également les appels jusqu'au redémarrage ; un refus de DM (403 ou code Discord 50007) termine seulement le travail concerné. Les erreurs enregistrées sont des codes fixes sans contenu privé.

La file et le nonce survivent aux redémarrages. Les jetons Plex restent en mémoire : après un redémarrage, la reprise exige donc une nouvelle connexion Plex de l'utilisateur. Une désactivation ou une modification des préférences invalide les travaux en attente ; délier ou supprimer le compte les invalide aussi. Les traces de livraison sont conservées au maximum 90 jours après le récapitulatif.

## Limite de déduplication

Le POST Discord utilise un nonce stable avec `enforce_nonce=true`. Selon la [documentation officielle de création des messages](https://github.com/discord/discord-api-docs/blob/main/developers/resources/message.mdx), Discord vérifie son unicité pendant quelques minutes seulement. La base locale empêche les doublons ordinaires, mais **ne garantit pas une livraison exactement une fois** : si Discord accepte le message puis que le processus s'arrête avant d'enregistrer le succès, une reprise après cette fenêtre peut produire un doublon. Aucun appel HTTP ne permet de rendre atomiques le succès distant et l'écriture SQLite.

Le transport applique les délais décrits dans la [documentation officielle des limites de débit](https://github.com/discord/discord-api-docs/blob/main/developers/topics/rate-limits.mdx). Les tests utilisent uniquement des transports HTTP simulés et couvrent les canaux privés, les mentions, les refus, les limites de débit, les révocations tardives, les reprises et les migrations. Aucun DM réel n'est nécessaire pour ces vérifications.
