# La voix d’Eris après les outils

Le 28 octobre 2026, Hugo signale une rupture de personnage sur le diagnostic de Toy Story 5 : la réponse technique se présente comme un rapport, puis Eris retrouve sa voix lorsqu’on lui fait remarquer ce changement.

Le test `TestV2DiagnosticUsesErisVoiceInsteadOfRawServiceProse` reproduit le problème sur le vrai parcours `Chat.ReplyWithTools` : un seul appel au modèle, puis le rapport affiché mot pour mot. La personnalité n’a aucune occasion d’en rédiger la restitution. Web/AniList/Ouicheur ont déjà une synthèse, mais celle-ci ne passe pas par le même message de voix que la conversation.

En v2, les lectures vérifiées de diagnostic, fiche cinéma, catalogue, nouveautés, demandes et métriques peuvent maintenant demander une rédaction par Eris. L’API l’autorise explicitement avec `narrate`; le client conserve une liste d’actions de lecture admises. Les erreurs et refus, choix en attente, créations, préférences modifiées et confirmations d’action gardent leur restitution contrôlée. Le modèle ne reçoit aucun nouvel outil lors de la rédaction et ne rappelle aucune mémoire ou autre conversation.

Le diagnostic expose uniquement un titre, une étape connue, une progression présentée à la même précision que le rapport, un nombre d’épisodes manquants éventuellement connu, une observation d’indexation Plex et l’indication de lecture partielle. Aucune preuve Arr brute, chemin, tracker, clé, version ou journal n’entre dans ce nouvel objet. Les champs absents restent inconnus. C’est le modèle qui formule ces observations, plutôt que de reprendre un paragraphe administratif déjà rédigé. Le rapport reste le repli disponible.

La voix commune est aussi utilisée par les synthèses web et catalogue externe. Le persona est conservé : l’objectif est une explication naturelle et adaptée à la question, sans juron, emoji ou vanne obligatoire. Le blocage, la progression, l’import et la lecture dans Plex restent distincts. Une lecture partielle ou une cause non confirmée garde ses réserves.

Les liens sont retirés des données de rédaction puis ajoutés par l’application à l’identique. La réponse JSON est bornée ; sortie malformée, nouveau nombre, URL ajoutée ou prétention de réparation reconnue provoquent un repli sur le résultat vérifié. La rédaction est limitée à douze secondes et ne relance aucune lecture ou action métier. Ces contrôles ne sont pas une preuve sémantique exhaustive : la fidélité et le naturel restent à observer en situation.

La [recette comparative](../evals/eris/tool-voice-2026-10-28.json) utilise six échanges synthétiques et le même modèle configuré : cas signalé, progression, saison incomplète, cause inconnue, réalisation d’un film et salutation. Les essais intermédiaires sur le rapport rédigé restaient trop administratifs ; le diagnostic structuré réduit nettement cette reprise de formulation. L’essai final garde un appel métier par demande. La réponse complète prend environ cinq à neuf secondes dans cette recette, contre trois à quatre secondes avant : une passe de rédaction est ajoutée. Ce relevé ne constitue ni une mesure de charge ni un score automatique de personnalité.

Validation : test de régression, tests du client Discord jusqu’au texte livré, isolation du contexte, conservation des liens, rejets des sorties non admises, annulation de rédaction, observations bornées et maintien des contrôles d’accès du diagnostic. La recette n’envoie aucun message Discord réel et ne consulte aucune demande réelle.

Un autre défaut trouvé sur ce parcours est corrigé : le client du bot refusait les preuves Ouicheur pourtant autorisées par l’API. Le filtre accepte désormais l’action `gifts` et continue à refuser des preuves inattendues pour une autre action.

La [documentation officielle OpenAI sur les prompts](https://developers.openai.com/api/docs/guides/prompting) recommande de placer la voix dans les instructions de rôle et de rejouer les évaluations lors des changements. L’effet sur Eris est évalué par la recette ci-dessus et le retour humain, sans changer de modèle.
