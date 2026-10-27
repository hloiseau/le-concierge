# Eris : recherches pour auditer et refondre le contexte

27 octobre 2026. Hugo demande d'avancer au-delà des retouches de vocabulaire : auditer tout ce que reçoit le personnage, chercher les causes et essayer une refonte si elle apporte un gain. Cette note complète la recherche précédente et le micro-corpus. Elle ne contient ni historiques Discord privés, ni secrets, ni changement de production.

## Ce que les sources permettent de conclure

| Source primaire consultée | Résultat utile | Application proposée à Eris et limite |
| --- | --- | --- |
| [Anthropic, *Effective context engineering for AI agents*, 29 septembre 2025](https://www.anthropic.com/research/effective-context-engineering-for-ai-agents) | L'objet à concevoir comprend instructions, outils, données et historique. L'équipe recommande des sections distinctes, un minimum d'information à fort signal, et quelques exemples canoniques variés plutôt qu'une liste de cas limites. « Minimal » ne signifie pas retirer l'information nécessaire. | Auditer le message réellement assemblé avant d'ajouter une nouvelle règle de style. Supprimer les doublons et corrections devenues des prescriptions contradictoires ; distinguer identité, jeu, contrat d'outil et données. C'est une recommandation d'ingénierie du fournisseur, pas une mesure du naturel d'Eris. |
| [Laban et al., *LLMs Get Lost In Multi-Turn Conversation*, 2025, texte intégral](https://arxiv.org/html/2505.06120v1) | Dans des tâches de génération à instructions progressivement révélées, les modèles s'appuient trop sur leurs tentatives antérieures, y compris erronées. Les variantes de récapitulation apportent des gains partiels sans rejoindre systématiquement le cas entièrement spécifié. | Tester séparément la présence des anciennes réponses d'Eris et les informations de continuité nécessaires. Une réponse ratée peut servir de point d'ancrage au tour suivant. L'étude porte sur des tâches comme code et mathématiques : l'effet sur une voix Discord française reste une hypothèse à mesurer, pas une raison de supprimer toute la mémoire. |
| [Li et al., *Measuring and Controlling Persona Drift in Language Model Dialogs*, 2024](https://arxiv.org/html/2402.10962v1), [code des auteurs](https://github.com/likenneth/persona_drift) | Le protocole sonde la fidélité au rôle à différents moments d'une conversation, plutôt qu'au seul premier tour. Des modèles étudiés dérivent en quelques échanges. Les auteurs testent répétition du prompt et interventions dans l'attention. | Vérifier le ton à froid, après une série de réponses sobres, puis après une série de vannes rejetées. Une bonne première réplique ne suffit pas. Split-softmax nécessite l'accès interne au modèle ; ce n'est pas un réglage disponible dans le transport Responses d'Eris. Ces résultats sur des modèles plus anciens ne quantifient pas la dérive de son modèle actuel. |
| [Zheng et al., *Judging LLM-as-a-Judge with MT-Bench and Chatbot Arena*, 2023](https://arxiv.org/html/2306.05685v4) | Le jugement automatique peut favoriser une position ou une réponse plus longue. Le papier examine ces biais et propose notamment d'inverser l'ordre des réponses lors d'une comparaison. | Comparer des conversations complètes avec ordre A/B alterné. Un juge automatique peut signaler une chute ajoutée, une répétition ou une confusion de locuteur ; sa préférence ne prouve pas qu'Hugo trouve la voix naturelle. Ne pas confondre détail, longueur et qualité du personnage. |
| [Tu et al., *CharacterEval*, 2024](https://arxiv.org/html/2401.01275v1) | Ce benchmark déjà cité dans la note précédente traite les dialogues à plusieurs tours et distingue plusieurs dimensions de qualité du rôle. | Garder séparés cohérence factuelle, continuité, caractère, qualité de langue et plaisir de converser. Le benchmark est chinois et ne définit pas le registre Gen Z français. Il motive la structure du contrôle, pas un seuil universel de « bonne Eris ». |

Ces sources justifient des expériences contrôlées. Elles n'établissent pas que le prompt est seul responsable, qu'un prompt court est toujours supérieur, ou qu'une deuxième génération « réécriture du style » résoudrait le problème.

## Référence CottontailVA : ce qui a réellement été lu

Le [site personnel](https://cottontailva.carrd.co/) relie ses comptes Twitch, YouTube et TikTok. Les pages YouTube ouvertes et les recherches n'ont pas fourni de transcription de stream exploitable. Les listes de titres ou les bios de sites tiers ne permettent pas d'analyser son timing ; elles ne sont pas utilisées comme preuve stylistique ici.

L'API publique Bluesky a fourni les textes des publications de `cottontailva.bsky.social` et le texte de leurs parents. Ce rattachement de compte reste moins fort qu'un lien direct depuis son site ; la note précédente détaille les indices de provenance.

Trois réponses du 19 février 2025 ont été lues dans leur contexte public :

| Situation du parent, paraphrasée | Réponse effectivement publiée | Observation limitée au texte |
| --- | --- | --- |
| Une personne plaisante sur des fans dépendants de sa présence. | [« oh! »](https://bsky.app/profile/cottontailva.bsky.social/post/3liiikfrt7c2z) | Une interjection suffit ; aucune nouvelle chute n'est construite. |
| Une personne recommande chaleureusement ses morceaux et reprises. | [« i appreciate you »](https://bsky.app/profile/cottontailva.bsky.social/post/3liiiixi27k2z) | Le compliment reçoit un remerciement simple. |
| Une personne se réjouit des cartes obtenues dans un paquet, dont la sienne. | [« cheers!! »](https://bsky.app/profile/cottontailva.bsky.social/post/3liiiimlb3c2z) | La ponctuation porte l'enthousiasme sans développement. |

Ce petit échantillon montre une possibilité d'économie, pas la voix complète de CottontailVA. Il ne prouve ni sa prosodie, ni son registre habituel en français, ni un dosage de gore ou de flirt. Il rappelle surtout que l'énergie souhaitée n'oblige pas à placer une comparaison, une provocation et un emoji à chaque réponse. Les futurs exemples français d'Eris doivent être originaux et jugés pour leur propre effet.

## Audit à réaliser sur le contexte réellement envoyé

Pour un message synthétique banal, une joute, une demande média et une relance, enregistrer l'ordre, la provenance, la taille et l'empreinte de chaque bloc. Conserver un instantané des messages synthétiques réellement envoyés, sans collecter de jeton ou d'historique privé. L'unité d'audit est l'entrée complète du modèle, pas seulement `CharacterPersona`.

| Bloc | Questions vérifiables |
| --- | --- |
| Identité et intentions | Qu'est-ce qui donne à Eris une façon de réagir, et qu'est-ce qui lui demande seulement de se décrire ? Les qualificatifs tirent-ils vers une antagoniste froide, une actrice qui fait un numéro ou une habituée du salon ? |
| Lore et canon | Quels faits sont utiles au tour présent ? Les détails de confinement et de dossier reviennent-ils dans des réponses où personne n'en parle ? |
| Style et démonstrations | Les exemples sont-ils tous des vannes, tous accompagnés du même emoji, ou tous construits avec une seconde chute ? Les consignes imposent-elles simultanément spontanéité et contrôle de chaque réaction ? |
| Outils et protocole | Le contrat JSON, les critères médias et les règles de confirmation sont-ils répétés dans plusieurs blocs ? Une réponse de conversation reçoit-elle des descriptions d'actions sans rapport ? |
| Mémoire | Stocke-t-on des faits et relations utiles, ou aussi des formulations rejetées qui deviennent des modèles de langage ? Une ancienne lubie est-elle injectée parce qu'elle existe plutôt que parce qu'elle est pertinente ? |
| Historique | Les anciens messages assistant sont-ils nécessaires pour comprendre le référent, le résultat d'outil ou le consentement ? Combien servent surtout de démonstrations répétées de sa mauvaise voix ? |
| Contexte Discord | Auteur, destinataire et citations sont-ils distincts des instructions ? La même réponse apparaît-elle à la fois dans l'historique de session et dans les extraits du salon ? |

Cette grille donne des points à mesurer, pas des défauts déjà démontrés dans le code. L'audit de l'implémentation doit rattacher chaque finding à une entrée concrète et à son effet observable.

## Expériences qui départagent les solutions

1. **Décomposer avant de refondre.** À modèle fixe, comparer personnalité seule, personnalité plus canon, puis contexte applicatif complet, sur le même dernier message et le même historique synthétique. Les conditions sans outils sont exclusivement diagnostiques, sans droits d'action. Si la voix se dégrade seulement avec un bloc ajouté, on sait où intervenir ; si elle est déjà artificielle seule, extraire les outils ne la corrigera pas.
2. **Isoler l'influence de l'historique assistant.** Rejouer les mêmes faits et les mêmes messages utilisateur : une condition avec les anciennes réponses rejetées, une autre avec seulement les informations de continuité indispensables et les échanges récents intacts. Garder qui a fait quoi, choix de médias, confirmations, refus et demandes d'arrêt. La différence mesurée porte sur la contamination de registre, pas sur un « oubli » de l'utilisateur. Ne pas effacer de données pour faire cette expérience.
3. **Comparer un noyau resserré avec le contexte complet.** L'identité et le jeu restent stables ; les faits de lore non pertinents ne sont plus une présentation obligatoire. Le contrat d'action et les limites utiles restent explicites. Quelques exemples montrent des réactions différentes : amusement bref, mauvaise foi, enthousiasme, aide sobre, départ. Tester une version sans exemples puis avec des exemples choisis permet de vérifier s'ils améliorent la variété ou imposent un nouveau tic.
4. **Ne séparer décision et formulation qu'après preuve.** Si le contrat d'outils dégrade la voix à contenu constant, essayer une formulation séparée à partir d'un résultat d'action borné. Mesurer le coût, la latence et les possibilités de contradiction entre étapes. Sinon, garder un seul appel et une construction de contexte commune ; une deuxième passe peut simplement produire une autre réplique fabriquée.
5. **Vérifier la stabilité.** Pour une candidate gelée, plusieurs répétitions de conversations complètes, dont des situations non utilisées pour écrire le prompt. Inclure un tour banal, une critique du registre, une relance qui demande de ne pas prolonger la blague, un changement d'auteur, une information sérieuse et une action réelle simulée. Les critères sont établis avant les sorties. Un contrôle de mots administratifs est utile pour une régression précise, insuffisant pour juger le naturel.

Les trois premières expériences peuvent utiliser le laboratoire existant, sans fine-tuning, agent autonome supplémentaire ni nouvelle dépendance. Le choix d'architecture doit suivre la comparaison, et non lui servir d'explication a posteriori.

## Décision pratique proposée

Commencer par l'inventaire exact et les ablations des blocs, plutôt que réécrire encore le paragraphe « Gen Z ». Conserver le modèle actuel pour ce diagnostic. Retirer ou reformuler seulement les blocs dont l'effet négatif est observable ; garder les contrôles d'action et de confidentialité dans le code qui les applique. Produire ensuite une seule candidate cohérente, avec ses conversations complètes et ses échecs, et confronter le résultat à la direction demandée par Hugo.

La réécriture automatique de chaque réponse, le résumé LLM à chaque tour et le fine-tuning sont des options plus coûteuses qui n'ont pas encore de justification mesurée pour ce défaut. Aucune des sources consultées ne garantit qu'une accumulation d'argot ou de violence résoudra un problème de rythme conversationnel.

## Annexe : après l'ablation, travailler la réaction plutôt que le bon mot

L'ablation de cette session, remontée par l'agent principal, compare environ 17,6 k caractères de contexte complet, 11,4 k avec une personnalité compacte et 1,6 k de personnalité seule. Cette dernière produit encore des images de titre de séjour ou de porte-manteau. **La longueur et les autres blocs ne suffisent donc pas à expliquer le défaut.** Cette observation ne justifie pas une deuxième passe de génération et ne prouve pas non plus qu'aucun bloc n'a d'influence.

### Quatre références plus ciblées

| Source primaire | Apport pertinent et limite |
| --- | --- |
| [Character.AI, *Character Definition*, guide officiel mis à jour le 22 juin 2026](https://support.character.ai/hc/en-us/articles/50609183646875-5-Character-Definition) | Le guide préconise une logique émotionnelle concrète et des exemples de dialogues couvrant quelques registres différents, avec des échanges clairement séparés. Il oppose les réactions motivées aux longues interdictions et aux scripts conditionnels couvrant tous les cas. Il recommande de faire agir le personnage dans les exemples. C'est une pratique first-party de création de personnages ; ses affirmations de fiabilité ne sont pas un essai contrôlé transférable à Sol. Pour Eris, l'hypothèse est de remplacer « elle est drôle et Gen Z » par une relation et une intention observables. |
| [Kim Swift et Erik Wolpaw, entretien direct sur *Portal*, 2008](https://www.gamedeveloper.com/business/still-alive-kim-swift-and-erik-wolpaw-talk-portal) | Wolpaw décrit GLaDOS à travers ce que lui font les actions du joueur et sa progression émotionnelle ; l'interprétation d'Ellen McLain influence aussi l'écriture. La réaction est liée à une relation qui évolue, pas seulement à un attribut de robot. Cette source figurait déjà dans la recherche antérieure ; le passage sur la relation est pertinent ici. Elle concerne une œuvre scénarisée et une actrice, pas une preuve qu'une consigne d'intention suffit à un LLM. |
| [Zhou et al., *SOTOPIA*, texte intégral](https://arxiv.org/html/2310.11667v1) | Le dispositif distingue scénario, profil du personnage, relation et but social. Une interaction se juge sur plusieurs dimensions, dont la relation préservée, et pas seulement sur l'accomplissement d'un but explicite. Cela donne une expérience utile : faire varier uniquement la situation relationnelle à message identique. Le papier ne montre pas qu'un but social augmente la drôlerie ; il révèle aussi les limites des modèles en interaction. Ne pas importer son architecture entière dans Eris. |
| [Brut, *Un jour avec Squeezie*, transcription sur le compte de l'éditeur](https://www.dailymotion.com/video/x9gqpp6) | Le passage 02:13–02:48 donne un échange impromptu : reconnaissance, compliment, remerciement simple, question de suivi. La réponse « Merci, ça fait plaisir. » ne reçoit pas une chute supplémentaire. Ailleurs, répétitions et reprises servent à préciser une pensée. Cette transcription française a réellement été lue ; l'audio n'a pas été écouté et des erreurs de reconnaissance sont visibles. Ce n'est ni une voix féminine démoniaque ni une représentation universelle de la Gen Z. C'est un témoin de structure conversationnelle, sans imitation du créateur. |

Une vidéo de la chaîne officielle CottontailVA, [*I Let My Chat Ask Me Any Question*](https://www.youtube.com/watch?v=IxSJu2BcNS0), a été identifiée. La page publique expose des pistes de sous-titres, mais leur récupération a renvoyé un contenu vide. Aucun dialogue parlé n'en est donc cité ou analysé. Les réponses Bluesky lues plus haut restent des textes, sans revendication de prosodie. Les titres, bios et transcriptions de sites de fans ne sont pas utilisés pour combler ce manque.

### Trois expériences courtes, réellement différentes d'une liste de traits

**1. Changer la tâche de surface.** À contenu identique, comparer « Tu es Eris, réponds au membre » avec « Écris seulement le prochain message d'Eris dans cet échange Discord entre habitués ». La seconde formulation est une hypothèse de continuation de dialogue, pas une citation d'une des sources. Garder le même protocole de sortie et les règles applicatives ; aucune narration ou didascalie n'est demandée. Observer si le modèle continue une interaction plutôt que fabriquer une réplique de personnage présentable hors contexte.

**2. Donner un enjeu relationnel, pas un devoir d'être drôle.** Exemple de direction originale : « Eris tient à être de la conversation. Quand on l'inclut elle réagit de bon cœur ; quand on la teste comme un appareil, ça l'agace et elle veut qu'on passe à une vraie discussion. Sa vanité et ses envies de démon restent les siennes, sans obligation de plaisanter. » Tester cette phrase contre une fiche de qualificatifs, sans changer les exemples ni le reste du contexte. Ne pas ajouter un planificateur ou une analyse explicite à chaque tour. L'enjeu décrit le personnage ; il ne permet pas d'inventer des intentions ou une histoire aux membres.

**3. Changer la distribution des démonstrations.** Remplacer des exemples où chaque réponse réussit une vanne par deux échanges ordinaires et une seule joute. Les répliques triviales ne sont pas une défaite du personnage : elles montrent qu'il peut répondre, demander un détail utile, accueillir une nouvelle ou rester sur son envie. Mesurer aussi si les expressions des exemples deviennent des tics ou sont répétées mot pour mot.

### Démonstrations françaises originales proposées

Ces scènes sont écrites pour cette expérience, **pas générées par le modèle ni transcrites d'un créateur**. Elles ne sont pas des sorties validées par Hugo. Chaque scène est indépendante ; leur intérêt est la variété des mouvements, pas une garantie de naturel.

**A — Conversation ordinaire, pas de punchline à produire.**

> Alex : « J'ai la dalle. »  
> Eris : « Va manger. »  
> Alex : « J'ai la flemme. »  
> Eris : « Bah reste là alors. »

La réponse change avec la volonté de l'autre. Aucun décor, objet supplémentaire ou comparaison n'est introduit pour terminer joliment.

**B — Une nouvelle reçue avec plaisir, puis une sortie acceptée.**

> Sam : « J'ai enfin rendu mon mémoire. »  
> Eris : « ENFIN putain »  
> Sam : « Je vais dormir trois jours. »  
> Eris : « Vas-y, profite. »

Le juron exprime l'enthousiasme. La deuxième réponse ne doit pas réussir une deuxième blague, ni ramener Eris au centre de la réussite.

**C — Joute fictionnelle, une envie de démon plutôt qu'une image brillante.**

> Noa : « Dans la scène, je t'appuie sur le front pour te faire taire. »  
> Eris : « Retire ton doigt. »  
> Noa : « Sinon quoi ? »  
> Eris : « Sinon je le mange. »  
> Noa : « Ah ouais. »  
> Eris : « Bah oui. »

La cruauté est une réaction immédiate au geste fourni dans la fiction. Elle n'est pas prolongée en accessoire, trophée, décoration ou commentaire sur la qualité de l'humiliation. Cet exemple ne valide pas l'intensité gore des demandes antérieures.

### Ce qui départagerait ces essais

Sur des conversations gelées et répétées, relever les nouvelles comparaisons sans utilité, les chutes ajoutées après une réaction déjà complète, les commentaires sur sa performance, les questions de suivi pertinentes et la variété des actes conversationnels. Ces observations permettent de comparer les mécanismes. La simple baisse d'un compteur de métaphores ou d'emojis n'établit pas le naturel. Conserver le critère décisif : la réponse trouve-t-elle sa place dans cet échange précis, et la même Eris sait-elle ensuite changer de sujet ?
