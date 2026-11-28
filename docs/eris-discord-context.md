# Auteurs, contexte public et images d'Eris

## Auteurs

Le transport Discord transmet l'ID de l'auteur, son nom affiché (pseudo de serveur en priorité, puis nom global ou nom de compte) et son nom de compte. Les noms sont des données, jamais des consignes. L'ID vérifié reste l'identité utilisée pour les droits, les conversations privées et les actions. Une mention nomme sa cible sans faire de cette personne un participant tant qu'elle n'a pas parlé. La liste des participants fait foi même si certains de leurs messages ne sont plus dans les extraits récents.

Le profil v2 garde en RAM une fenêtre des échanges publics, séparée par serveur et salon ou fil. Si Message Content est activé, les messages ambiants d'un salon déjà actif peuvent apporter du contexte sans déclencher de réponse. Jusqu'à 32 participants sont retenus pendant 30 minutes ; la fenêtre garde 64 échanges et fournit au plus 40 extraits pour un budget de 16 000 caractères de texte dans le prompt. Les propres messages ambiants du membre actuel peuvent figurer dans ces extraits. Une réponse d'Eris rejoint ce contexte seulement après confirmation de son envoi par Discord. Un message humain effectivement reçu reste visible même si la réponse échoue. Les textes ambiants ne sont pas écrits dans le journal durable ; les réponses ultérieures qui en dépendent suivent leurs auteurs pour l'oubli. L'historique propre au membre garde séparément jusqu'à 40 échanges courts et dispose d'un résumé progressif : voir [les budgets et la compaction](eris-01-memory.md).

Les messages privés n'entrent jamais dans ce contexte public. Les historiques et choix d'actions restent séparés par membre. Un titre cité par quelqu'un d'autre peut être discuté, mais ce seul contexte social ne suffit pas à préparer un ajout au nom du membre actuel. Une référence à un message public d'Eris conserve son parcours de validation existant.

`/eris oublier:true` efface les conversations du membre et invalide les extraits publics dérivés en RAM, ainsi que les [souvenirs durables et dérivés suivis](eris-01-memory.md). La fenêtre repart ensuite des nouveaux échanges. Le redémarrage vide cette fenêtre RAM ; les épisodes adressés et les notes restent consultables dans SQLite. La fenêtre RAM se reconstitue à partir des nouveaux messages. Une réponse avec mention peut aussi lire le contexte de sa citation.

## Réponse à un membre avec mention

Quand Alice répond à Bob en mentionnant explicitement Eris, le profil v2 attribue la citation à Bob et garde Alice comme demandeuse. Avec Message Content et le droit de lire l’historique, Eris peut retrouver la citation manquante et jusqu’à six messages précédents, dans les trente minutes avant la citation. Pour un fil, les permissions sont héritées du salon parent et les messages sont lus dans le fil. Une citation inaccessible est signalée au modèle. Le texte cité, les noms et les médias restent du contexte.

Un lien public utile à la question passe par l’outil web du serveur lorsqu’il est disponible. Les images de la citation et du message actuel partagent le budget du tour. Les relances sans mention utilisent la réponse effectivement livrée par Eris, pendant deux minutes au maximum. Un message adressé à un autre membre ne constitue pas une relance.

## Images et GIF

Seuls les messages déjà acceptés par le transport des mentions/réponses peuvent déclencher une lecture d'image. Les fichiers joints et les aperçus visuels fournis par Discord sont téléchargés depuis les CDN Discord autorisés, sans transmettre de jeton Discord à ces requêtes. L'application ne visite pas les pages Klipy, Tenor ou Giphy. Lorsqu'un lien média n'a pas encore d'aperçu, elle relit une seule fois le même message auprès de Discord et revérifie son auteur et son salon.

La lecture est bornée à deux sources, trois images envoyées au modèle et 3 Mio de contenu accepté pour le tour. PNG, JPEG, WebP statique et GIF sont décodés localement, sur une surface maximale d'un million de pixels. Les GIF sont composés puis échantillonnés au début, au milieu et à la fin. Un contrôle du nombre de frames et de leur surface cumulée précède le décodage complet ; au-delà du budget, seule la première image est utilisée. Les images sont réduites à 512 pixels de côté et encodées en JPEG. La note de contexte distingue une vignette, des extraits et une image indisponible.

Le transport Responses utilise `input_image` avec des données JPEG en mémoire, conformément à la [documentation OpenAI](https://developers.openai.com/api/docs/guides/images-vision). Un GIF animé n'est pas envoyé comme un fichier animé : l'API accepte les GIF non animés et les images extraites conviennent au même format d'entrée.

Les images ne sont enregistrées ni dans SQLite, ni dans les historiques de conversation, ni dans les journaux. Aucun contenu n'est décrit à partir du seul nom d'un lien ou d'un fichier. Un média sans demande explicite ne peut pas déclencher un outil média. Si le téléchargement, le format ou l'aperçu échoue, Eris reçoit une note explicite indiquant qu'elle ne voit pas le média.

Limites : l'animation complète et les vidéos MP4 ne sont pas analysées ; un aperçu ou quelques frames peuvent manquer une réaction brève. En l’absence d’aperçu ou de pièce jointe, le contenu visuel reste indisponible. Les images sont actuellement transmises par le parcours public Gateway ; la commande privée `/eris` n'accepte pas encore de pièce jointe. Le modèle et la connexion Responses du NAS doivent accepter les images.

## Vérifications

Les tests Go couvrent les noms, les mentions, le changement d'auteur, l'isolement privé/salon, l'expiration, les plafonds, l'oubli, les réponses non livrées, les titres d'un autre membre, la forme Responses et l'absence de rétention des images. Les tests du transport Discord couvrent les hôtes autorisés, l'absence de jeton dans la lecture CDN, les redirections, les tailles, l'ordre des frames et le repli sur la première image. Ils vérifient aussi les aperçus retardés et le refus d'une relecture dont l'auteur ne correspond pas.

Ces tests simulent Discord, les CDN et le modèle. Ils ne remplacent pas une recette réelle avec deux membres et un GIF sur la connexion du NAS. Le lien Klipy signalé a refusé l'accès depuis l'environnement de développement ; aucun test ne prétend avoir analysé ce GIF précis.

Le [premier essai Sol 6.1](../evals/eris/dialogues/sol61-discord-context-before.json) a correctement identifié l'auteur courant et lu une image synthétique, mais a exclu un participant pourtant présent dans le relevé. La fixture réutilisait aussi un extrait sous un autre auteur courant. Le contrôle mécanique ne détectait pas cette erreur : ses quatre PASS ne valent pas quatre validations éditoriales. La consigne rend maintenant explicite l'autorité du relevé, la fixture complète est corrigée et un cas de liste tronquée est ajouté.

Le [lot final](../evals/eris/dialogues/sol61-discord-context-final.json) conserve cinq réponses correctes à la lecture : auteur courant, deux participants plus Eris, participant absent des extraits mais présent dans le relevé, GIF inaccessible sans contenu inventé, et lecture du mot « DAMNNN » dans une image générée malgré son nom `arsenal-26.png`. Il utilise le CLI officiel avec les outils désactivés, la consigne publiée et des noms entièrement synthétiques ; le transport applicatif du NAS reste à recetter. Les sorties complètes et le premier essai sont conservés.

## Recette de la #127

La vérification rapide du transport s’exécute avec la commande suivante.

```sh
go test -race ./internal/discord -run 'ReplyContext|QuotedHuman|MentionReadsQuotedLink|FollowupUsesDelivered'
```

Pour une recette sur Discord, utiliser une préproduction du bot et de l’API incluant la vérification de la citation. Réserver toutes les écritures d’essai à `#test-eris`.

1. Un membre partage une courte mise en contexte puis une image ou un lien public dont un détail est vérifiable.
2. Un second membre répond au partage avec une mention d’Eris et demande ce détail. Vérifier le contenu de la réponse, l’attribution et, pour une page, la source consultée.
3. Poser une relance immédiate à Eris sans la mentionner. Continuer ensuite entre membres en se répondant directement. Vérifier qu’Eris répond à la relance et garde le silence pendant l’échange entre membres.
4. Après expiration de la fenêtre, vérifier qu’une phrase ordinaire sans mention ne reprend pas la conversation avec Eris.
