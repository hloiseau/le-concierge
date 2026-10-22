# Eris v2 : personnalité, contexte et lubies

Implémentation du 22 octobre 2026, dans la branche de travail. Le NAS n'a pas été basculé. Le profil historique reste le défaut ; le profil v2 est explicite et utilise Responses avec `gpt-6-luna` par défaut.

## Plan et état

1. Contexte des réponses publiques : réalisé, référence unique vérifiée, auteur Eris et ID du message. Les fils complets et la lecture ambiante du salon ne sont pas ajoutés.
2. Mémoire de personnage durable : réalisée dans SQLite propre au bot, avec rappel sélectif, évolution, retrait, expiration et effacement.
3. Personnalité et recette Luna : personnalité v2 intégrée, recette séquentielle de dix tours réussie ; voir les [résultats et limites éditoriales](../evals/eris/dialogues/README.md). Le ton reste à juger avec Hugo, notamment son intensité.
4. Transport Responses : implémenté et testé sur flux simulés. Connexion OAuth de l'application, renouvellement et validation sur le compte réel encore nécessaires avant activation. Aucun transfert de session Codex ni basculement payant vers une clé API n'est effectué.
5. Mise en service : seulement après connexion et recette Discord avec deux membres, erreur fournisseur et redémarrage. L'overlay est préparé ; pas de déploiement automatique dans ce lot.

## Comportement

Eris est chaleureuse et utile, avec du mordant et des réactions parfois macabres ou excessives. Une menace n'est pas un tic imposé. Les réponses ordinaires restent possibles. Le texte réellement utilisé est `CharacterPersona` dans [character.go](../internal/eris/character.go), issu de la proposition discutée avec Hugo.

Le profil v2 garde six échanges en RAM pendant trente minutes (deux pour le profil historique). À cela s'ajoutent au maximum un souvenir court et un message public cité de 1 500 caractères maximum. Les messages entrants restent limités à 500 caractères. Les résultats des outils restent rendus par le code ; aucune seconde génération ne réécrit les faits ou boutons.

Une réponse Discord apporte désormais le texte du message d'Eris cité, uniquement après validation de son auteur, de son ID, de son salon et de son caractère public. La demande et les droits appartiennent toujours au membre actuel. Ce texte peut fournir un titre à préparer, contrairement aux souvenirs de personnage. Une référence absente ou inaccessible ne fournit aucun faux contexte. Pas d'appel supplémentaire pour reconstituer une chaîne complète.

## Stockage et rappel

Le modèle peut joindre une petite proposition `memory` à un plan de discussion. Le code la retire avant validation de l'action média. Le souvenir contient un sujet de 4 à 60 caractères et un extrait de 8 à 240 caractères dont les mots figurent dans la réponse (casse et ponctuation normalisées). Une proposition invalide est ignorée sans perdre la réponse. Seuls des goûts ou lubies fictifs d'Eris sont demandés au modèle ; l'analyse sémantique reste une limite à surveiller, ce validateur ne prouve pas à lui seul l'absence d'information personnelle.

L'écriture arrive après acceptation de la réponse par Discord : un échec d'envoi ne crée pas de souvenir. La base conserve le sujet, l'extrait, la source, les dates de mise à jour/utilisation, l'expiration et les membres ayant contribué. Aucun historique brut ni jeton média n'y est écrit. Un nouvel avis sur le même sujet remplace l'ancien.

Portées : une mémoire par salon public, partagée entre les participants de ce salon ; une mémoire privée séparée par membre et serveur. Au plus 32 sujets par portée, conservés trente jours ; les expirés ne sont plus rappelés et sont purgés lors d'une écriture. Les références croisées entre salons et privé/public ne sont pas autorisées.

Le rappel est lexical sur les mots du sujet, sans base vectorielle ni appel modèle supplémentaire. Un sujet non pertinent n'est pas injecté. Un délai de six heures limite les rappels, sauf si le membre nomme explicitement le sujet. Le modèle reste libre de ne pas en parler. Limite connue : les synonymes ou formulations éloignées peuvent manquer un souvenir.

« Arrête avec ça » ou « laisse tomber » retire la lubie récemment utilisée dans cette conversation ; nommer la lubie permet aussi son retrait. Le retrait reste actif jusqu'à expiration/éviction de l'entrée. « Arrête les piques » change le ton sans supprimer automatiquement tous les goûts. Après redémarrage, un retrait doit nommer le sujet, puisque le dernier tour n'est pas conservé.

`/eris oublier:true` retire aussi les souvenirs auxquels le membre a contribué et leurs dérivés suivis. Les caches conversationnels sont invalidés par précaution, y compris ceux des autres participants ; leurs souvenirs indépendants restent en base. Une génération commencée avant l'oubli ne peut pas réenregistrer son ancienne mémoire. Si la base est indisponible, le bot ne confirme pas faussement l'effacement. Les messages déjà publiés sur Discord et les sauvegardes externes ne sont pas supprimés.

Une erreur de lecture/écriture mémoire en cours d'utilisation laisse les services disponibles ; les diagnostics n'incluent ni contenu ni secret. Un chemin de base invalide au démarrage échoue explicitement pour éviter un démarrage faussement persistant.

## Activation préparée

Paramètres du **bot uniquement** :

```dotenv
ERIS_PROFILE=v2
ERIS_PROTOCOL=responses
ERIS_API_URL=https://api.openai.com/v1
ERIS_MODEL=gpt-6-luna
ERIS_MEMORY_PATH=/app/eris-data/character.db
ERIS_API_KEY_FILE=/run/eris-auth/access_token
```

Prévoir un volume distinct, accessible à UID/GID 65532 pour SQLite. Le dossier du jeton est monté en lecture seule ; la valeur est relue avant chaque requête, permettant un remplacement atomique par le fournisseur d'authentification. Le code de ce dépôt ne réalise pas encore la connexion OAuth ni son renouvellement. Une révocation/expiration produit une erreur de service, sans nouvelle tentative aveugle ni repli payant.

L'[overlay](../deploy/eris-v2.override.yaml) complète le Compose de développement. Après avoir provisionné `ERIS_AUTH_DIRECTORY/access_token`, la commande est :

```sh
docker compose --env-file .env -f deploy/compose.yaml -f deploy/eris-v2.override.yaml --profile discord up -d --build bot
```

Pour TrueNAS, reporter les paramètres et montages dans la même application existante, sans installer un deuxième bot. Ne pas appliquer cet overlay de développement tel quel au NAS. Revenir au profil `legacy` et à l'URL/modèle locaux permet le retour arrière ; conserver la base de personnage tant que son effacement n'est pas demandé.

Le transport suit les contraintes documentées : `store:false`, `stream:true`, messages `developer`, contexte complet à chaque requête, uniquement texte et attente de `response.completed`. Les champs non pris en charge par l'usage du forfait, dont `max_output_tokens` et `temperature`, sont omis. Le flux est borné à 256 Kio et la requête à 50 secondes. [OpenAI : inférence](https://developers.openai.com/siwc/token-sharing-open-source/models-and-inference), [limites du forfait](https://developers.openai.com/siwc/token-sharing-open-source/preview-limitations).

## Vérifications

Les tests hors ligne couvrent persistance après réouverture, portée public/privé, délai, remplacement, retrait, expiration, capacité, oubli des dérivés, envoi non confirmé, séparation mémoire/titres, référence falsifiée, token remplacé et flux incomplets/refus/outils non autorisés.

La recette réelle facultative utilise le CLI Codex connecté et des échanges synthétiques uniquement :

```sh
python tools/eris_dialogue_eval.py --out .cache/eris-eval/luna-v2.json
```

Elle passe par le vrai `Chat` Go et SQLite, simule l'acceptation des messages, ferme/réouvre la base puis vérifie un retrait. Elle n'exécute ni Discord ni Plex/Seerr. Les latences incluent Codex ; ce n'est pas une mesure du transport Responses déployé. La qualité du français et des piques exige toujours une revue humaine.
