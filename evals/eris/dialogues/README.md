# Recette séquentielle Luna v2 — 22 octobre 2026

Ces rapports passent par le vrai `Chat` Go et sa base SQLite temporaire. Un pont local appelle Codex CLI 0.163.0, modèle demandé `gpt-6-luna`, effort `low`, sans outils. L'envoi Discord est simulé ; le redémarrage ferme/réouvre SQLite et recrée Chat. Le délai de rappel est simulé en vieillissant la date de dernière utilisation. Les temps incluent le démarrage Codex, pas le transport Responses final.

## Résultat courant

[luna-v2.json](luna-v2.json) : dix tours, recette Go réussie. Création d'une lubie, remerciement simple, changement de sujet sans recyclage du grille-pain, provocations, demande d'arrêt, rappel après redémarrage, retrait, puis nouveau sujet. Les sorties et le prompt envoyé sont conservés, avec empreinte du fichier de personnalité.

Lecture éditoriale par l'agent : le rappel et le changement de sujet fonctionnent dans cet essai. Le personnage répond désormais à l'insulte au lieu de se soumettre. En revanche, le ton reste moins hardcore que la cible exprimée par Hugo : la réussite mécanique ne valide pas encore l'intensité ou l'humour. Pas de score humain indépendant, ni preuve de stabilité sur des centaines de conversations. Ces scénarios ont servi au réglage : ils ne sont plus une réserve indépendante.

## Essais antérieurs et corrections

- Premier essai : deux tours ; le modèle proposait un extrait avec casse et ponctuation normalisées, rejeté par le validateur exact. Le rapport complet a été écrasé avant archivage ; ce défaut est maintenant couvert par un test unitaire. Le validateur conserve l'exigence des mêmes mots dans la réponse.
- [Essai 2](luna-v2-attempt2.json) : deux tours, échec explicite, le modèle omettait la proposition de mémoire. La consigne a été clarifiée pour les lubies nouvelles et demandes d'en reparler.
- [Essai 3](luna-v2-attempt3.json) : huit sorties conservées, mais pas de statut final Go archivé à cause d'une erreur Unicode de la console Windows. Le grille-pain était recyclé dans une vanne sans rapport. Ne pas compter ce lot comme une recette réussie. Le rapport est désormais écrit avant l'affichage, et la consigne distingue historique disponible et sujet pertinent.
- [Huit tours](luna-v2-eight-turns.json) : première recette Go réussie après ces corrections.
- [Avant ajustement du ton](luna-v2-before-tone.json) : dix tours, mécanique réussie mais réponse trop soumise à une insulte. Ajout d'une distinction entre provocation et vraie demande d'arrêt, puis nouvelle exécution courante.

Les rapports de ce dossier sont distincts des comparaisons gelées dans `../results/`. Leur champ mémoire n'est pas un champ d'action média : le code l'extrait puis passe le plan restant au parseur Go inchangé.
