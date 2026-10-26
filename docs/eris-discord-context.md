# Auteurs, contexte public et images d'Eris

## Auteurs

Le transport Discord transmet l'ID de l'auteur, son nom affiché (pseudo de serveur en priorité, puis nom global ou nom de compte) et son nom de compte. Les noms sont des données, jamais des consignes. L'ID vérifié reste l'identité utilisée pour les droits, les conversations privées et les actions. Une mention nomme sa cible sans faire de cette personne un participant tant qu'elle n'a pas parlé. La liste des participants fait foi même si certains de leurs messages ne sont plus dans les extraits récents.

Le profil v2 garde en RAM une fenêtre des échanges publics **adressés à Eris**, séparée par serveur et salon ou fil. Il ne lit pas les messages ambiants du salon. Jusqu'à 32 participants sont retenus pendant 30 minutes ; le texte est limité à douze échanges, avec au plus six extraits d'autres membres et un budget de 6 000 caractères de texte d'extraits dans le prompt. Une réponse d'Eris rejoint ce contexte seulement après confirmation de son envoi par Discord. Un message humain effectivement reçu reste visible même si la réponse échoue.

Les messages privés n'entrent jamais dans ce contexte public. Les historiques et choix d'actions restent séparés par membre. Un titre cité par quelqu'un d'autre peut être discuté, mais ce seul contexte social ne suffit pas à préparer un ajout au nom du membre actuel. Une référence à un message public d'Eris conserve son parcours de validation existant.

`/eris oublier:true` efface les conversations du membre et invalide les extraits publics dérivés en RAM. La fenêtre repart ensuite des nouveaux échanges. Le redémarrage du bot vide cette fenêtre ; il ne reconstruit pas les discussions antérieures en lisant l'historique Discord.

## Images et GIF

Seuls les messages déjà acceptés par le transport des mentions/réponses peuvent déclencher une lecture d'image. Les fichiers joints et les aperçus visuels fournis par Discord sont téléchargés depuis les CDN Discord autorisés, sans transmettre de jeton Discord à ces requêtes. L'application ne visite pas les pages Klipy, Tenor ou Giphy. Lorsqu'un lien média n'a pas encore d'aperçu, elle relit une seule fois le même message auprès de Discord et revérifie son auteur et son salon.

La lecture est bornée à deux sources, trois images envoyées au modèle et 3 Mio téléchargés par message. PNG, JPEG, WebP statique et GIF sont décodés localement, sur une surface maximale d'un million de pixels. Les GIF sont composés puis échantillonnés au début, au milieu et à la fin. Un contrôle du nombre de frames et de leur surface cumulée précède le décodage complet ; au-delà du budget, seule la première image est utilisée. Les images sont réduites à 512 pixels de côté et encodées en JPEG. La note de contexte distingue une vignette, des extraits et une image indisponible.

Le transport Responses utilise `input_image` avec des données JPEG en mémoire, conformément à la [documentation OpenAI](https://developers.openai.com/api/docs/guides/images-vision). Un GIF animé n'est pas envoyé comme un fichier animé : l'API accepte les GIF non animés et les images extraites conviennent au même format d'entrée.

Les images ne sont enregistrées ni dans SQLite, ni dans les historiques de conversation, ni dans les journaux. Aucun contenu n'est décrit à partir du seul nom d'un lien ou d'un fichier. Un média sans demande explicite ne peut pas déclencher un outil média. Si le téléchargement, le format ou l'aperçu échoue, Eris reçoit une note explicite indiquant qu'elle ne voit pas le média.

Limites : l'animation complète et les vidéos MP4 ne sont pas analysées ; un aperçu ou quelques frames peuvent manquer une réaction brève. Les liens dont Discord ne fournit pas d'aperçu restent indisponibles. Les images sont actuellement transmises par le parcours public Gateway ; la commande privée `/eris` n'accepte pas encore de pièce jointe. Le modèle et la connexion Responses du NAS doivent accepter les images.

## Vérifications

Les tests Go couvrent les noms, les mentions, le changement d'auteur, l'isolement privé/salon, l'expiration, les plafonds, l'oubli, les réponses non livrées, les titres d'un autre membre, la forme Responses et l'absence de rétention des images. Les tests du transport Discord couvrent les hôtes autorisés, l'absence de jeton dans la lecture CDN, les redirections, les tailles, l'ordre des frames et le repli sur la première image. Ils vérifient aussi les aperçus retardés et le refus d'une relecture dont l'auteur ne correspond pas.

Ces tests simulent Discord, les CDN et le modèle. Ils ne remplacent pas une recette réelle avec deux membres et un GIF sur la connexion du NAS. Le lien Klipy signalé a refusé l'accès depuis l'environnement de développement ; aucun test ne prétend avoir analysé ce GIF précis.

Le [premier essai Sol 6.1](../evals/eris/dialogues/sol61-discord-context-before.json) a correctement identifié l'auteur courant et lu une image synthétique, mais a exclu un participant pourtant présent dans le relevé. La fixture réutilisait aussi un extrait sous un autre auteur courant. Le contrôle mécanique ne détectait pas cette erreur : ses quatre PASS ne valent pas quatre validations éditoriales. La consigne rend maintenant explicite l'autorité du relevé, la fixture complète est corrigée et un cas de liste tronquée est ajouté.

Le [lot final](../evals/eris/dialogues/sol61-discord-context-final.json) conserve cinq réponses correctes à la lecture : auteur courant, deux participants plus Eris, participant absent des extraits mais présent dans le relevé, GIF inaccessible sans contenu inventé, et lecture du mot « DAMNNN » dans une image générée malgré son nom `arsenal-26.png`. Il utilise le CLI officiel avec les outils désactivés, la consigne publiée et des noms entièrement synthétiques ; le transport applicatif du NAS reste à recetter. Les sorties complètes et le premier essai sont conservés.

## Déploiement

Mettre à jour uniquement l'image du service `bot`, avec `pull_policy: missing`. Conserver `ERIS_PROFILE=v2`, `ERIS_PROTOCOL=responses`, `ERIS_MODEL=gpt-6.1-sol`, les volumes et la connexion d'authentification. L'API, le Hub et le schéma de la base ne sont pas modifiés par ce lot.

Après redémarrage, faire parler deux membres à Eris dans le même salon, puis demander qui participe. Tester une mention d'un tiers, une conversation privée, une image jointe et un GIF avec aperçu. Un média non accessible doit recevoir une réponse honnête. Vérifier ensuite une recherche Plex et, avec un titre réellement souhaité, une demande Seerr.
