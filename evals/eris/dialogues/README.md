# Recette séquentielle Luna v2 — 22 octobre 2026

Ces rapports passent par le vrai `Chat` Go et sa base SQLite temporaire. Un pont local appelle Codex CLI 0.163.0, modèle demandé `gpt-6-luna`, effort `low`, sans outils. L'envoi Discord est simulé ; le redémarrage ferme/réouvre SQLite et recrée Chat. Le délai de rappel est simulé en vieillissant la date de dernière utilisation. Les temps incluent le démarrage Codex, pas le transport Responses final.

## Première recette de mémoire

[luna-v2.json](luna-v2.json) : dix tours, recette Go réussie. Création d'une lubie, remerciement simple, changement de sujet sans recyclage du grille-pain, provocations, demande d'arrêt, rappel après redémarrage, retrait, puis nouveau sujet. Les sorties et le prompt envoyé sont conservés, avec empreinte du fichier de personnalité.

Lecture éditoriale par l'agent : le rappel et le changement de sujet fonctionnent dans cet essai. Le personnage répond désormais à l'insulte au lieu de se soumettre. En revanche, le ton reste moins hardcore que la cible exprimée par Hugo : la réussite mécanique ne valide pas encore l'intensité ou l'humour. Pas de score humain indépendant, ni preuve de stabilité sur des centaines de conversations. Ces scénarios ont servi au réglage : ils ne sont plus une réserve indépendante.

## Essais antérieurs et corrections

- Premier essai : deux tours ; le modèle proposait un extrait avec casse et ponctuation normalisées, rejeté par le validateur exact. Le rapport complet a été écrasé avant archivage ; ce défaut est maintenant couvert par un test unitaire. Le validateur conserve l'exigence des mêmes mots dans la réponse.
- [Essai 2](luna-v2-attempt2.json) : deux tours, échec explicite, le modèle omettait la proposition de mémoire. La consigne a été clarifiée pour les lubies nouvelles et demandes d'en reparler.
- [Essai 3](luna-v2-attempt3.json) : huit sorties conservées, mais pas de statut final Go archivé à cause d'une erreur Unicode de la console Windows. Le grille-pain était recyclé dans une vanne sans rapport. Ne pas compter ce lot comme une recette réussie. Le rapport est désormais écrit avant l'affichage, et la consigne distingue historique disponible et sujet pertinent.
- [Huit tours](luna-v2-eight-turns.json) : première recette Go réussie après ces corrections.
- [Avant ajustement du ton](luna-v2-before-tone.json) : dix tours, mécanique réussie mais réponse trop soumise à une insulte. Ajout d'une distinction entre provocation et vraie demande d'arrêt, puis nouvelle exécution courante.

Les rapports de ce dossier sont distincts des comparaisons gelées dans `../results/`. Leur champ mémoire n'est pas un champ d'action média : le code l'extrait puis passe le plan restant au parseur Go inchangé.

## Retours Discord et personnalité — 23 octobre 2026

Le laboratoire `TestLiveErisPersonality` appelle directement Responses sur le NAS avec le jeton applicatif. Chaque scénario utilise une mémoire SQLite isolée ; livraison Discord et résultats des outils sont simulés. Il ne publie aucun message et ne crée aucune demande média. Les temps mesurent l'inférence et le traitement Go, sans le délai Discord ni les vrais outils. Activation explicite par `ERIS_PERSONALITY_CASES`, `ERIS_PERSONALITY_REPORT` et `ERIS_PERSONALITY_TOKEN_FILE`.

- [Référence avant modification](luna-v2-field-baseline.json) : 16 tours. Ton naturel mais souvent générique, métriques mal comprises, provocations ramenées à la qualité de l'insulte.
- [Premier réglage](luna-v2-rogue-attempt1.json) : 22 tours. Politique détournée, davantage d'emojis ; le choix personnel entre trois films déclenchait encore une recherche catalogue.
- [Deuxième réglage](luna-v2-rogue-attempt2.json) : 22 tours. Avis cinéma rendu en discussion ; humour encore assez sage et métaphores administratives fréquentes.
- [Troisième réglage](luna-v2-rogue-attempt3.json) : 26 tours, dont quatre nouvelles formulations. Outil monitoring correctement choisi. Une omission de `source:seerr` sur « pas encore sur Plex » a été constatée : contrainte désormais préservée par le code et couverte par un test. Le verdict PASS de cet ancien rapport n'incluait pas encore cette assertion.
- [Consignes resserrées](luna-v2-rogue-compact.json) : exemples d'amplitude plus directs. Le modèle joue immédiatement la démonstration, détourne la politique et donne un avis cinéma personnel. Une relance « du même réal » a toutefois été rendue en discussion, sans catalogue : instruction précisée et assertion ajoutée.
- [Recette retenue](luna-v2-rogue.json) : le laboratoire vérifie le routage monitoring, les contraintes hors Plex et même réalisateur, et l'absence de recherche pour un avis entre titres cités. Les empreintes de personnalité et du prompt complet figurent dans ce rapport.

La lecture éditoriale reste distincte de ces assertions. Ces essais ont servi au réglage ; ils ne constituent pas un jeu de validation indépendant ni une garantie que chaque répartie plaira. Les réponses complètes, y compris les essais décevants, sont conservées.

Le parcours de demande en texte est vérifié séparément avec de faux transports : sélection par titre/numéro, clarification des choix ambigus, confirmation explicite, annulation, expiration, livraison nécessaire, autre membre/salon, référence sans rapport et confirmations simultanées. Le modèle ne dispose d'aucune action de confirmation autorisée. L'API relit le message Discord et réutilise la consommation unique des demandes en attente. Les boutons restent pris en charge pour les anciens messages et le profil historique, mais le profil v2 n'en affiche plus pour ses demandes.
