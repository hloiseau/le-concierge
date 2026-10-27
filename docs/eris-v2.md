# Eris v2 : personnalité, contexte et lubies

Évolution du 27 octobre : [canon ERIS-01, mémoire durable des membres, journal d'épisodes, silence et contexte ambiant](eris-01-memory.md). Les limites de trente jours décrites ci-dessous concernent uniquement l'ancien carnet de lubies, pas le nouveau journal. Cette évolution n'est pas encore déployée par cette PR.

Déployé sur le NAS le 22 octobre 2026 à la demande de Hugo, depuis la branche de travail. Le bot actif utilise le profil v2 et Responses avec `gpt-6-luna`, via la connexion ChatGPT propre à Eris. Le profil historique reste le défaut dans le code. Voir le [relevé de déploiement](eris-v2-nas.md).

## Plan et état

1. Contexte des réponses publiques : référence unique vérifiée, auteur Eris et ID du message. La nouvelle version ajoute un contexte ambiant RAM dans les salons actifs si Message Content est activé ; les fils complets ne sont pas reconstruits.
2. Mémoire de personnage durable : réalisée dans SQLite propre au bot, avec rappel sélectif, évolution, retrait, expiration et effacement.
3. Personnalité et recette Luna : personnalité v2 intégrée, recette séquentielle de dix tours réussie ; voir les [résultats et limites éditoriales](../evals/eris/dialogues/README.md). Le ton reste à juger avec Hugo, notamment son intensité.
4. Transport Responses : validé sur le compte réel depuis le NAS, avec une réponse de discussion et un plan d'outil synthétique. Le [service OAuth personnel](../tools/eris-auth/README.md) gère la connexion et le renouvellement avec le devkit officiel. Aucun transfert de session Codex ni basculement payant vers une clé API n'est effectué.
5. Mise en service : réalisée sur le NAS avec sauvegarde et contrôles de santé, connexion Discord confirmée. La recette humaine dans Discord, notamment à deux membres et sur l'intensité du personnage, reste à effectuer.

## Comportement

Après la [première correction de voix](../evals/eris/voice-review.md), le [retour d'usage du 26 octobre](../evals/eris/chaos-review.md) demande une Eris plus exubérante : emojis expressifs, joie inquiétante et réactions déjantées, avec le même langage graphique dans les joutes consenties. Le calme sert de contraste occasionnel. L'aide reste exacte et une demande réelle d'arrêt met fin au jeu. Le texte proposé est `CharacterPersona` dans [character.go](../internal/eris/character.go). Une modification de cette consigne compilée exige une nouvelle image du bot.

Le [second retour du même jour](../evals/eris/graphic-review.md) constate que l'exubérance avait remplacé la cruauté par des farces inoffensives. La comédie horrifique redevient le registre principal : les petites provocations de la joute et les demandes de vengeance suscitent spontanément des conséquences corporelles macabres, sans attendre une demande explicite de démonstration. Les emojis accompagnent cette cruauté. Les questions utiles, les réussites, l'arrêt du jeu et les confidences gardent une réponse adaptée.

Le profil v2 garde jusqu'à quarante échanges courts du membre (80 messages, budget de 32 000 caractères), dans une session RAM expirant après trente minutes d'inactivité. Le profil historique garde deux échanges. À débordement, les anciens messages alimentent un résumé progressif sourcé, persistant dans SQLite quand la mémoire est configurée. Le [contexte Discord](eris-discord-context.md) ajoute les noms vérifiés par le transport, une fenêtre bornée des échanges publics et les images réellement accessibles. La [mémoire durable](eris-01-memory.md) apporte les notes et épisodes sourcés après redémarrage. Les discussions privées et les autorisations d'action restent séparées par membre. Les images ne sont pas conservées dans l'historique. Les messages entrants restent limités à 500 caractères. Les résultats des outils restent rendus par le code ; aucune seconde génération ne réécrit les faits ou boutons média. La compaction et une recherche locale de souvenirs peuvent employer des générations supplémentaires, sans outil média.

Une réponse Discord apporte désormais le texte du message d'Eris cité, uniquement après validation de son auteur, de son ID, de son salon et de son caractère public. La demande et les droits appartiennent toujours au membre actuel. Ce texte peut fournir un titre à préparer, contrairement aux souvenirs de personnage. Une référence absente ou inaccessible ne fournit aucun faux contexte. Pas d'appel supplémentaire pour reconstituer une chaîne complète.

## Suivre une réponse sans mention — travail local du 26 octobre

Après un message public effectivement envoyé par Eris, un message humain du salon peut poursuivre l'échange pendant deux minutes, même avec jusqu'à cinq messages intercalés. Exemple : Eris demande « Et toi, ça va ? », un autre membre intervient, puis le premier écrit « nickel » sans mention ni bouton Répondre. Un autre membre peut aussi répondre à une question ouverte au groupe. « Eris, tu connais ce film ? » permet de l'appeler par son nom, sans notification Discord.

Le bot doit avoir `ERIS_PROFILE=v2` et `DISCORD_MESSAGE_CONTENT=true`, avec Message Content Intent autorisé dans Discord. Ces paramètres restent propres au bot ; l'API n'a pas besoin d'un profil d'inférence. Le bot relit au maximum six messages précédents pour retrouver sa réponse. Un classement séparé, sans personnalité ni mémoire privée, reçoit aussi les messages intercalés avec leurs auteurs et décide si la relance lui est clairement destinée. Il est limité à huit secondes et partage la capacité d'inférence existante. Les essais des autres membres sont espacés de dix secondes ; l'interlocuteur en cours reste prioritaire. En cas de doute ou d'erreur, Eris reste silencieuse.

Les messages destinés à quelqu'un d'autre sont ignorés sans effacer immédiatement le contexte d'Eris. Une expiration, un oubli concerné, une déconnexion ou le dépassement de la fenêtre de six messages ferme cette possibilité. Une nouvelle réponse d'Eris la rouvre. Un message supplémentaire arrivé pendant le classement ou la génération annule l'intervention implicite en attente ; les outils revérifient aussi que ce tour est encore courant avant leur appel. Les mentions explicites et les réponses Discord restent utilisables pour reprendre un échange interrompu.

Les outils reçoivent seulement l'ID du contexte public, jamais une identité déduite par le modèle. L'API relit elle-même le salon, le message du membre et les six messages précédents, puis vérifie le contexte avec les mêmes règles que le bot. Une réponse plus récente d'Eris empêche de réutiliser une ancienne proposition. « Le deuxième » peut sélectionner une proposition sans ping ; un autre membre ne peut pas reprendre ce choix à son compte. Les demandes du membre (titres, états, saisons et éditions, par titre ou en liste) sont consultables en salon. Les filtres de visionnage restent privés, comme les données de compte et les conversations privées.

Les [cas synthétiques](../evals/eris/dialogues/addressing-cases.json) et le [rapport Luna](../evals/eris/dialogues/luna-addressing.json) vérifient le classement. Ils ne constituent pas une recette en conditions réelles dans Discord. Ce changement est indépendant du réglage de personnalité, repris après le rejet de la première candidate par Hugo ; voir le [bilan de personnalité](../evals/eris/blend-comparison.md).

## Participation spontanée — travail local, non déployé

Dans chaque salon souhaité (par exemple Général et Absolute cinéma), un administrateur utilise `/eris-salon participation:activer`. `/eris-salon participation:desactiver` coupe les interventions spontanées ; `/eris-salon participation:etat` ou `/eris-salon` affiche le réglage. Les réponses sont éphémères. Les permissions administrateur sont revérifiées dans l'interaction signée. Aucun compte Plex n'est nécessaire. Les fils ont leur propre réglage, sans héritage automatique du salon parent.

Tous les salons sont désactivés par défaut. Le réglage est conservé par serveur et salon dans la base `ERIS_MEMORY_PATH`, déjà persistante pour les lubies, sans historique brut. La commande nécessite le profil v2, Message Content et cette base ; elle refuse d'annoncer une activation si ces prérequis manquent. La commande est enregistrée au démarrage du bot comme les autres commandes. Les activations suivantes ne demandent ni changement de code ni redéploiement.

Le filtrage local n'appelle pas le modèle à chaque message. Une recherche de nom de film peut ouvrir une occasion ; sinon il faut huit messages éligibles dans un échange actif, avec au moins deux auteurs dans le contexte récent. Le contexte contient au plus six messages de 250 caractères, en RAM, séparés par auteur ; une pause de deux minutes le réinitialise à la prochaine utilisation. Les bots, mentions d'autres personnes, réponses Discord à d'autres messages, messages anciens et contenus privés sont exclus. Le filtre est volontairement conservateur et peut manquer des occasions.

Au plus une évaluation spontanée par cinq minutes et par salon, même en cas de silence, de panne ou de résultat devenu obsolète. Un seul appel choisit soit le silence, soit une courte réaction pour identifier un film ou placer un bon mot. Aucune mémoire privée ni outil média n'est accessible par ce parcours. Une réponse incertaine sur un film doit rester une hypothèse. Le plafond ne s'applique pas aux messages adressés à Eris. Une conversation active et les traitements déjà en cours sont prioritaires sur les interventions spontanées.

Si personne ne rebondit sur une intervention spontanée, Eris n'en envoie pas une autre dans ce salon pendant cette session. Un échange adressé à Eris rouvre cette possibilité. Un nouveau message ou une désactivation pendant la génération empêche la publication du résultat en attente. `/eris oublier:true` purge aussi les contextes spontanés contenant des messages de ce membre. Les plafonds et contextes en RAM repartent à zéro au redémarrage ; les salons activés restent enregistrés.

Les tests hors ligne couvrent permissions, persistance, isolation des salons, contexte intercalé, budget, silence, panne et désactivation pendant un appel. La pertinence des interventions et le ton restent à vérifier avec le modèle réel et dans Discord après déploiement.

## Stockage et rappel

Le modèle peut joindre une petite proposition `memory` à un plan de discussion. Le code la retire avant validation de l'action média. Le souvenir contient un sujet de 4 à 60 caractères et un extrait de 8 à 240 caractères dont les mots figurent dans la réponse (casse et ponctuation normalisées). Une proposition invalide est ignorée sans perdre la réponse. Seuls des goûts ou lubies fictifs d'Eris sont demandés au modèle ; l'analyse sémantique reste une limite à surveiller, ce validateur ne prouve pas à lui seul l'absence d'information personnelle.

L'écriture d'une lubie arrive après acceptation de la réponse par Discord : un échec d'envoi ne crée pas de souvenir de personnage. Ce carnet conserve le sujet, l'extrait, la source, les dates de mise à jour/utilisation, l'expiration et les membres ayant contribué. Aucun jeton média n'y est écrit. Le nouveau journal durable conserve séparément les messages humains adressés et les réponses de discussion livrées. Un nouvel avis sur le même sujet remplace l'ancienne lubie.

Portées : une mémoire par salon public, partagée entre les participants de ce salon ; une mémoire privée séparée par membre et serveur. Au plus 32 sujets par portée, conservés trente jours ; les expirés ne sont plus rappelés et sont purgés lors d'une écriture. Les références croisées entre salons et privé/public ne sont pas autorisées.

Le rappel est lexical sur les mots du sujet, sans base vectorielle ni appel modèle supplémentaire. Un sujet non pertinent n'est pas injecté. Un délai de six heures limite les rappels, sauf si le membre nomme explicitement le sujet. Le modèle reste libre de ne pas en parler. Limite connue : les synonymes ou formulations éloignées peuvent manquer un souvenir.

« Arrête avec ça » ou « laisse tomber » retire la lubie récemment utilisée dans cette conversation ; nommer la lubie permet aussi son retrait. Le retrait reste actif jusqu'à expiration/éviction de l'entrée. « Arrête les piques » change le ton sans supprimer automatiquement tous les goûts. Après redémarrage, un retrait doit nommer le sujet, puisque le dernier tour n'est pas conservé.

`/eris oublier:true` retire aussi les souvenirs auxquels le membre a contribué et leurs dérivés suivis. Les caches conversationnels sont invalidés par précaution, y compris ceux des autres participants ; leurs souvenirs indépendants restent en base. Une génération commencée avant l'oubli ne peut pas réenregistrer son ancienne mémoire. Si la base est indisponible, le bot ne confirme pas faussement l'effacement. Les messages déjà publiés sur Discord et les sauvegardes externes ne sont pas supprimés.

Une erreur de lecture/écriture mémoire en cours d'utilisation laisse les services disponibles ; les diagnostics n'incluent ni contenu ni secret. Un chemin de base invalide au démarrage échoue explicitement pour éviter un démarrage faussement persistant.

## Activation

Paramètres du **bot uniquement** :

```dotenv
ERIS_PROFILE=v2
ERIS_PROTOCOL=responses
ERIS_API_URL=https://api.openai.com/v1
ERIS_MODEL=gpt-6-luna
ERIS_MEMORY_PATH=/app/eris-data/character.db
ERIS_API_KEY_FILE=/run/eris-auth/access_token
```

Prévoir un volume distinct, accessible à UID/GID 65532 pour SQLite. Le dossier du jeton est monté en lecture seule ; la valeur est relue avant chaque requête, permettant un remplacement atomique par le [service d'authentification](../tools/eris-auth/README.md). Une révocation/expiration produit une erreur de service, sans nouvelle tentative aveugle ni repli payant.

L'[overlay](../deploy/eris-v2.override.yaml) complète le Compose de développement. Après avoir provisionné `ERIS_AUTH_DIRECTORY/access_token`, la commande est :

```sh
docker compose --env-file .env -f deploy/compose.yaml -f deploy/eris-v2.override.yaml --profile discord up -d --build bot
```

Pour TrueNAS, reporter les paramètres et montages dans la même application existante, sans installer un deuxième bot. Ne pas appliquer cet overlay de développement tel quel au NAS. Revenir au profil `legacy` et à l'URL/modèle locaux permet le retour arrière ; conserver la base de personnage tant que son effacement n'est pas demandé.

Le transport suit les contraintes documentées : `store:false`, `stream:true`, messages `developer`, contexte complet à chaque requête, uniquement texte et attente de `response.completed`. La route du forfait peut renvoyer `output:[]` dans cet événement final ; dans ce cas, le texte provient des éléments `response.output_item.done` déjà finalisés. Les deltas seuls ne suffisent jamais. Ce format a été constaté puis validé avec Luna sur le NAS, pour une discussion et un choix d'outil synthétique. Les champs non pris en charge par l'usage du forfait, dont `max_output_tokens` et `temperature`, sont omis. Le flux est borné à 256 Kio et la requête à 50 secondes. [OpenAI : inférence](https://developers.openai.com/siwc/token-sharing-open-source/models-and-inference), [limites du forfait](https://developers.openai.com/siwc/token-sharing-open-source/preview-limitations).

## Vérifications

Le [parcours de demande allégé](eris-natural-requests.md) remplace la double validation : une demande explicite avec résultat unique exact part directement, et une sélection en texte parmi les candidats suffit à l'envoyer. Les anciennes confirmations restent compatibles.

Les tests hors ligne couvrent persistance après réouverture, portée public/privé, délai, remplacement, retrait, expiration, capacité, oubli des dérivés, envoi non confirmé, séparation mémoire/titres, référence falsifiée, token remplacé et flux incomplets/refus/outils non autorisés.

La recette réelle facultative utilise le CLI Codex connecté et des échanges synthétiques uniquement :

```sh
python tools/eris_dialogue_eval.py --out .cache/eris-eval/luna-v2.json
```

Elle passe par le vrai `Chat` Go et SQLite, simule l'acceptation des messages, ferme/réouvre la base puis vérifie un retrait. Elle n'exécute ni Discord ni Plex/Seerr. Les latences incluent Codex ; ce n'est pas une mesure du transport Responses déployé. La qualité du français et des piques exige toujours une revue humaine.

