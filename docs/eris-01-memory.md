# ERIS-01 : continuité, mémoire et tours de parole

Évolution préparée le 27 octobre 2026, liée à #74. Cette branche ne constitue pas un déploiement sur le NAS. Le modèle configuré reste celui de l'installation ; aucun changement de fournisseur ni réentraînement n'est effectué.

## Retour d'usage

Le groupe apprécie la compréhension des interlocuteurs et une cruauté nette, comme une menace brève liée aux doigts d'un membre qui infantilise le personnage. Les suites deviennent pénibles quand Eris ajoute une punchline à chaque acquiescement, répète son emoji et ses formules, se lamente sur son impuissance ou transforme une provocation de fiction en demande de vérification factuelle. Le problème n'est donc pas de rendre chaque phrase plus violente.

La transcription réelle fournie par Hugo sert à cette synthèse. Elle n'est pas publiée dans le dépôt, ni utilisée comme un profil biographique des participants. Les fixtures de recette emploient des identités et situations synthétiques.

## Identité permanente

Le dossier original a été retrouvé dans `hloiseau/eris-local-inference-private`, au chemin `src/content/fr/ERIS.md`, blob `242c3cc1f8af5d6b21ef82a7ed9c54101799543b`. Il existe aussi dans l'historique du portfolio `hloiseau-os` au commit `889d7a2b3fb3b78789ab3fa631374ea684a8c84c`, même chemin et même blob. Il porte explicitement la mention fiction.

[canon.go](../internal/eris/canon.go) en retient les éléments nécessaires à la conversation :

| Élément | Canon |
| --- | --- |
| Identité | ERIS-01, démon informatique apparu pendant la construction de HLOISEAU_OS |
| Corps | Armure noire, deux cornes, sigil rouge au plastron |
| Cellule d'origine | WEB-01 / HLOISEAU_OS |
| Classification | EUCLIDE ; 1 / SOMBRE ; 4 / DANGER ; accréditation 4 / SECRET |
| Recherche | N. Vandeginste ; R. Delorme-Glorieux ; TECH-04 |
| Anomalie | `/HOST/WAKE`, supprimé onze minutes avant sa création |
| WEB-07 | Mort fictionnelle d'OPERATOR-07 attribuée à Eris ; méthode caviardée, jamais inventée comme un fait du dossier |
| Création réelle | Hugo Loiseau, distinct du responsable fictif |

Le présent Discord prolonge le personnage, sans prétendre à une évasion ou à des pouvoirs réels. La direction de voix associe charme dangereux, assurance, folie et cruauté. Les emojis sont un choix expressif, pas une ponctuation obligatoire. Les demandes utiles passent en premier. Aucun souvenir de conversation ne modifie le canon.

## Trois formes de mémoire

1. Le canon est compilé avec le bot et fourni à chaque génération v2.
2. Le carnet historique `eris_whims` garde les lubies d'Eris, avec sa politique existante : 32 sujets, trente jours, retrait et délai entre rappels.
3. Le nouveau journal `eris_turns` et les notes `eris_notes` n'expirent pas automatiquement. Ils restent disponibles après fermeture/réouverture de SQLite et après redémarrage du bot.

Le journal écrit le texte d'un message humain **adressé à Eris**, son ID Discord, l'ID et les noms de son auteur, sa portée et sa date. Cela arrive avant l'appel au modèle : un échec de génération ne fait pas disparaître un message effectivement reçu. Un rejeu du même ID ne le duplique pas et ne change pas son auteur. Les images, jetons et résultats privés d'outils ne sont pas stockés dans ce journal. Le texte humain lui-même est conservé tel que transmis, dans la limite existante de 500 caractères : il peut contenir des informations personnelles fournies par le membre.

Une réponse de discussion rejoint le journal uniquement après confirmation d'envoi par Discord. Les propositions de notes suivent cette même confirmation. Chaque note doit citer exactement un extrait du message humain actuel ; son auteur, sa portée et sa source sont assignés par le code. Le modèle ne peut pas fournir un ID d'auteur, un droit d'accès ou un champ d'action dans ces métadonnées. Deux notes maximum par tour ; les champs sont bornés. Le type est `preference`, `fact` ou `episode`.

La citation est la preuve conservée ; le résumé et les synonymes restent des propositions du modèle. Le validateur prouve la présence de la citation, pas la justesse sémantique de son interprétation. Le prompt demande de distinguer les faits confiés, les plaisanteries, les récits sur autrui et les hypothèses. Il exclut les profils politiques, les secrets et les confidences sensibles des notes synthétiques. Cela ne transforme pas automatiquement tout le journal brut en faits biographiques.

Une correction sur le même sujet remplace la note courante. Casse, ponctuation et accents du sujet sont normalisés. Une livraison plus ancienne ne peut pas remplacer une note issue d'un tour plus récent. Les anciens épisodes gardent les propos datés pour rendre le changement intelligible. Des sujets sémantiquement proches avec des noms différents peuvent encore créer plusieurs notes.

## Recherche

SQLite FTS5 recherche les citations, les réponses livrées, les sujets, les résumés et les synonymes. Le classement BM25 privilégie les messages humains et les sujets ; les accents et la casse ne bloquent pas la correspondance. Les préfixes sont acceptés. L'expression MATCH est construite depuis des mots, sans exécuter la syntaxe brute du membre ou du modèle.

Le contexte contient au plus dix notes et cinq épisodes, dans un JSON limité à 14 000 octets. Quelques notes du membre actuel sont chargées même sans mot commun : une reformulation peut ainsi retrouver un goût stable. Les épisodes sont accompagnés de leur auteur, de leur ID source et de leur date, avec distinction entre parole humaine et réplique d'Eris.

Le modèle peut demander un seul `recall` local avec des mots reformulés ou des synonymes ; `query:"recent"` consulte les derniers épisodes. Une seconde génération répond en discussion uniquement. Cette recherche ne part jamais vers l'API média et ne peut ni préparer une demande Seerr, ni lancer une nouvelle boucle d'outils. Un souvenir ne fournit pas à lui seul un titre autorisé pour un ajout.

Cette version n'utilise pas d'embeddings. FTS, synonymes et reformulation améliorent le rappel lexical, sans garantir la récupération d'un épisode ancien formulé très différemment. Une recherche vectorielle pourra être comparée sur un vrai corpus avant d'ajouter un service ou un fournisseur. Aucun taux de rappel sémantique n'est annoncé sur les seuls tests techniques.

## Portées et oubli

Le public reste séparé par serveur et salon/fil. Le privé reste séparé par serveur et membre. Les identités sont les IDs vérifiés du transport, jamais des pseudos déduits par le modèle. Les notes d'un autre salon ne sont pas injectées dans celui-ci. Cette restriction évite qu'une conversation dans un salon restreint devienne une information publique ailleurs.

Les liens de provenance enregistrent les membres qui ont contribué à un contexte rappelé, aux tours RAM utilisés et au contexte ambiant. Ils se propagent aux souvenirs dérivés, y compris aux anciennes lubies. `/eris oublier:true` supprime les tours, notes, entrées FTS et lubies liés au membre ainsi que leurs dérivés suivis. L'invalidation des générations et caches empêche leur réenregistrement après l'oubli. Cette politique est conservatrice : une réponse utilisant un contexte collectif peut être supprimée même si elle n'en reprend finalement aucun détail.

L'oubli ne supprime ni les messages déjà publiés sur Discord ni les sauvegardes externes. Les éditions et suppressions Discord ne sont pas encore réconciliées automatiquement avec ce journal. Un nom affiché dans un ancien épisode reste celui observé à sa date ; l'auteur actuel est donné par le transport courant.

## Silence et contexte récent

Les réponses implicites « oui », « merci », « tkt pas » ou contenant seulement un emoji ne déclenchent plus systématiquement de génération. Les choix média actifs sont préservés. Une demande de silence interrompt les réponses implicites du salon ; une mention explicite rouvre la discussion. Le modèle peut aussi choisir `silent`, sans publication Discord. Une commande privée reçoit toujours une réponse utilisable.

Quand `DISCORD_MESSAGE_CONTENT=true`, le Gateway peut ajouter des messages ambiants à une fenêtre RAM d'un salon déjà actif. Aucun salon n'est activé par la seule conversation ambiante. La fenêtre expire après trente minutes d'inactivité, garde douze extraits et n'en injecte que six pour 6 000 caractères au maximum. Ces extraits peuvent venir du membre actuel : une abréviation expliquée juste avant une mention reste ainsi compréhensible. Un extrait `ambient` sert seulement de contexte, jamais de demande ou de permission d'action.

Le texte ambiant lui-même n'est pas écrit dans le journal durable. Une réponse adressée ultérieure qui s'appuie dessus peut être conservée ; ses auteurs contributeurs sont alors suivis pour l'oubli. Sans l'intent Message Content, le bot continue à fonctionner avec les mentions et réponses explicites disponibles, mais ne peut pas reconstruire des propos ambiants absents.

## Exporter la discussion

Le script [export_eris_discord.py](../tools/export_eris_discord.py) utilise uniquement des requêtes GET vers Discord. Il vérifie que le jeton appartient à un bot, exporte un salon ou un fil et produit un JSON accompagné d'un TXT lisible. Auteurs, citations, mentions, liens des pièces jointes et aperçus de GIF sont préservés. Aucun média n'est téléchargé, aucun message n'est envoyé et rien n'est importé automatiquement dans la mémoire.

Activer le mode développeur dans Discord, puis utiliser « Copier l'identifiant du salon ». Exécuter depuis le dépôt sur une machine ayant déjà accès au secret du bot :

```sh
python3 tools/export_eris_discord.py --channel ID_DU_SALON --token-file /chemin/vers/le/secret-du-bot --limit 2000 --out /tmp/eris-discussion.json
```

Le script accepte aussi `DISCORD_BOT_TOKEN` déjà présent dans l'environnement ; aucun jeton ne doit être transmis dans ce chat ou commité. Les fichiers sont créés en mode 0600 et une sortie existante n'est pas écrasée. `--after` et `--before` bornent la plage par IDs de messages, exclusifs. La limite maximale est de 20 000 messages. Un salon et ses fils sont des exports distincts.

Le bot a besoin des permissions [View Channel et Read Message History](https://github.com/discord/discord-api-docs/blob/main/developers/resources/message.mdx#get-channel-messages). L'intent Message Content peut être nécessaire pour lire les corps de tous les messages. Un export vide ne prouve donc pas que le salon n'a pas d'historique. Les réponses API sont bornées et les erreurs ne journalisent ni les en-têtes ni le jeton. Les limites de débit sont réessayées cinq fois au maximum, avec un délai maximal de trente secondes.

Pour un premier retour, copier le texte du salon directement dans la conversation suffit aussi ; les noms et les liens de GIF aident à interpréter les échanges. Les fichiers exportés doivent rester hors du dépôt public.

## Déploiement et validation

L'évolution utilise le chemin `ERIS_MEMORY_PATH` et le volume SQLite existants. Au démarrage, elle ajoute des tables, index FTS et triggers sans supprimer le carnet historique. Sauvegarder la base à l'arrêt du bot avant de mettre à jour son image. Conserver le profil v2, Responses, le modèle installé et les montages d'authentification. Aucun changement API/Hub n'est requis.

Les tests locaux couvrent réouverture de la base, persistance au-delà de trente jours, accents/préfixes/synonymes, attribution et rejeu, correction et livraison tardive, isolation, suppression transitive et FTS, métadonnées invalides, rappel en deux passes sans action média, silence, contexte ambiant et export. Discord, le CDN et le fournisseur sont simulés dans ces tests.

Le test de voix sur le modèle réel a été bloqué par le contrôle automatique d'approbation : il considérait le prompt dérivé du dépôt privé comme un transfert externe non autorisé. Il n'a pas été contourné. Les fixtures `eris01-review.json` sont une recette préparée, pas un résultat de modèle. Il faut donc encore valider sur l'installation réelle la voix, la latence, l'extraction fidèle des notes et les rappels reformulés, puis mesurer les régressions sur les demandes média.

Référence technique : [SQLite FTS5, BM25 et tokenizer Unicode](https://www.sqlite.org/fts5.html).
