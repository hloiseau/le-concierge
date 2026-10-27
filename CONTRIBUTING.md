# Contribuer

Une branche et une PR par évolution cohérente, liée au ticket. Décrire le problème résolu, le résultat, les tests pertinents et les limites restantes. Les contrats API sont versionnés dans le-concierge. Ne pas fermer une issue pour la seule création de son squelette. Ne pas embarquer de secrets, données utilisateurs réelles ou URLs privées du NAS dans les fixtures. Les corrections dans les projets amont font l’objet de tickets distincts et liés.

Les règles de branche et de CI seront configurées lors du bootstrap ; ce document ne garantit pas leur activation.

## Traçabilité des versions déployées

Le code, les consignes de personnalité, tests synthétiques, scripts et modèles de
configuration utiles à une version partagée doivent être commités, poussés et liés
à une PR avant son déploiement, sauf instruction explicite contraire du mainteneur.
Une image locale fonctionnelle ne remplace pas la conservation de ses sources.
Les secrets, bases, configurations remplies et données utilisateurs réelles restent
dans des sauvegardes privées, hors Git.

Identifier la source exacte d'une image et comparer tout build spécialisé à `main`
avant de le remplacer. Documenter séparément sources publiées, fusion, images
réellement exécutées, vérifications et retour arrière. Une version déployée depuis
une branche de travail doit rester suivie dans sa PR et être réconciliée ensuite.

Les [instructions aux agents](AGENTS.md) précisent ces règles. La
[reprise Eris du 27 octobre](docs/eris-reprise-autre-pc.md) décrit la récupération
du build local participation suivie dans #79 ; les nouveaux outils de #78 restent
en attente.
