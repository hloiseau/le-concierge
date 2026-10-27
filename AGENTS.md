# Instructions aux agents

Lire [CONTRIBUTING.md](CONTRIBUTING.md) avant de modifier ce dépôt.

## Reprise Eris prioritaire

Pour la régression signalée le 27 octobre 2026, lire d'abord
[le document de reprise](docs/eris-reprise-autre-pc.md) et l'issue #79.
Les nouveaux outils recensés dans #78 sont en attente. Retrouver et préserver le
build `eris-concierge:participation-28f46de` avant de retoucher la personnalité.

## Versionner le travail livré

- Un build utilisé sur le NAS dont les sources ne sont pas poussées et
  identifiables est une livraison incomplète. Ne pas laisser la seule version
  fonctionnelle dans une image locale, un arbre sale ou un historique de chat.
- Examiner l'état Git, les branches, worktrees et changements locaux avant toute
  synchronisation. Préserver le travail existant ; pas de reset, nettoyage,
  checkout forcé ou stash automatique pour obtenir artificiellement un arbre
  propre. Faire la récupération dans une branche ou un worktree distinct.
- Versionner les sources utiles, prompts, migrations, tests, fixtures synthétiques,
  scripts, modèles de configuration et documentation. Inspecter les fichiers et
  les changements indexés avant commit ; ne pas ajouter aveuglément tout le dossier.
- Ne pas commiter secrets, configurations NAS remplies, bases SQLite, exports ou
  transcriptions réelles, jetons, sauvegardes privées et rapports bruts contenant
  des données non publiables. Leur sauvegarde privée est distincte du versionnage
  du code. Les exemples utilisent des valeurs fictives.
- Avant un déploiement partagé : commit sur une branche, push vérifié, PR liée au
  ticket, contrôles pertinents, source exacte du build et image traçable. Une PR
  de travail peut rester ouverte ; préciser explicitement si elle n'est pas fusionnée.
  Les instructions explicites de Hugo restent prioritaires, notamment pour un
  essai exceptionnel : documenter alors sa source et ses limites.
- Privilégier une image publiée et une référence immuable vérifiée. Un nom local
  ou un label hérité d'une image de base ne prouve pas la version du binaire.
  Identifier séparément API et bot ; ne pas remplacer une image spécialisée par
  `main` sans comparer leurs écarts.
- Après déploiement, consigner dans une note versionnée et sans secrets : PR,
  SHA complet des sources, tag/digest de l'image, services concernés, profil et
  modèle, migrations, contrôles réellement observés et procédure de retour arrière.
  Les sauvegardes remplies restent dans un emplacement privé.
- Avant de terminer : vérifier le push et donner les liens commit/PR, les tests
  effectués, les modifications restant locales et la version réellement déployée.
  Distinguer « écrit », « testé », « poussé », « fusionné » et « déployé ».

Un déploiement sur une branche de travail doit ensuite être réconcilié avec le
dépôt : PR actualisée et différences documentées. Ne pas déclarer le travail
terminé avec seulement « ça marche sur ce PC ».
