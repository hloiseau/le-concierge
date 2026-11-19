# Ordre 66 : correction de voix du 19 novembre 2026

**Consigne actuelle : attaque exclusivement verbale, sans roleplay ni scène physique.** Les sections antérieures consignent les essais qui précédaient cette clarification de Hugo ; leurs consignes corporelles ne sont plus celles du bot.

Le retour de Hugo relève une scène grandiloquente (« sigil rouge »), une attaque interrompue et une chute expliquant que c'est du théâtre. Eris traite ensuite « cringe » comme une demande de supprimer toute menace physique.

La consigne dédiée demandait une scène « théâtrale » et répétait sa nature fictive sans expliquer comment garder ce cadre implicite. Elle demande désormais une courte répartie familière de comédie horrifique, avec la même personnalité, sans décor fantasy ni annulation de l'action. La mention de la cible vérifiée est ajoutée par le transport ; ce format est distinct du texte libre du modèle. L'auteur reste exclu des cibles.

La consigne générale distingue une critique du style d'un arrêt du jeu. Elle interdit de prétendre modifier son propre code ou ses directives. Une vraie demande d'arrêt ou un malaise exprimé restent prioritaires.

Le test Discord de l'adressage a échoué avant correction, puis passe. Les tests conservent le refus d'une cible inconnue ou de l'auteur et la clarification si aucune cible n'est identifiable.

Un seul échantillon réel, `TestLiveOrder66VoiceAndStyleFeedback`, utilise Responses / gpt-6-luna avec le compte dédié d'Eris, des identités synthétiques et aucun outil externe exécuté. Le transport limite l'essai à deux appels : 8 064 jetons au total. Aucun message Discord ni ajout média n'est envoyé.

- Ordre : « J’vais t’ouvrir en deux, _p10u. Après, tu feras moins le malin. », précédé de la mention de la cible synthétique.
- Critique : « Ouais, là c’est cringe 😭 Corrige, je vais pas défendre ça. »

Ce petit échantillon retire les défauts signalés ; il ne mesure ni la variété durable ni la préférence de Hugo. Les répliques restent produites par le modèle, sans catalogue, filtre de mots en production ni texte de remplacement.

## Relance sans interlocuteur — retour de 23:06

Le nouvel essai sans destinataire produisait `ERIS_UNAVAILABLE`. Les services et OAuth étaient sains. La capture des six messages précédents ne contenait que l'auteur et Eris, avec un échange plus ancien hors fenêtre : aucun interlocuteur humain éligible.

Le rejeu privé avant correction reçoit HTTP 200 et un JSON de forme correcte, mais une cible non vide qui n'est ni un interlocuteur éligible ni l'auteur. Le validateur la refuse ; ce cas conversationnel devient une erreur technique. Les métadonnées de diagnostic ne journalisent aucun corps Discord ni identifiant choisi.

Le parcours sans interlocuteur passe désormais directement par la formulation d'une clarification, sans tâche de sélection de cible. Une cible inconnue ou l'auteur renvoyés par le modèle déclenchent aussi une clarification neuve, sans publier le texte rejeté. Le modèle n'est pas relancé après une panne de génération. Les erreurs de génération, de format et de cible ont des diagnostics fixes sans contenu privé.

Le test unitaire a échoué avant modification puis passe. Les tests Discord couvrent l'historique vide, l'auteur et le bot seuls, le pair trop ancien, les cibles inconnues et l'auteur protégé. Un seul rejeu réel avant et un seul après ont consommé respectivement 2 769 et 2 315 jetons ; le second reçoit une clarification sans cible en un appel. Aucun message Discord ni outil média n'est exécuté. La capture privée et le diagnostic temporaire sont conservés hors du code livré.

## Démonstration sans cible — retour de 23:21

La clarification imposée refusait aussi « ordre 66 de test », puis la discussion refusait « Je veux faire un test sans cible ». C'était une restriction introduite par l'application, pas un prérequis du roleplay. Le parcours sans destinataire utilise maintenant les mêmes consignes de personnage que le parcours ciblé et le format normal de discussion, sans le faux résultat d'opération `clarification_required`. Le modèle peut jouer une démonstration contre un adversaire imaginaire anonyme ou demander qui viser selon l'intention. Il n'a aucun identifiant de membre à sélectionner dans cette réponse. La consigne de discussion couvre aussi la relance exacte et ne transforme pas un ancien refus du personnage en règle.

Les tests de démonstration ont échoué avant correction et passent après, y compris avec un membre présent dans le contexte mais non désigné comme cible. Les tests de refus de l'auteur et des IDs inconnus restent actifs.

Le contrôle opt-in `TestLiveOrder66DemoWithoutATarget` remplace l'ancien petit échantillon de voix. Il joue les deux messages exacts avec des identités synthétiques et un transport limité à deux générations : 8 405 jetons, aucun outil exécuté ni message Discord envoyé.

- Le premier message produit une action corporelle sur un adversaire anonyme, sans demander un destinataire réel.
- La relance accepte la démonstration, mais reste abstraite et ajoute une didascalie. La consigne générale est ensuite précisée pour garder une action courte sur une victime fictive identifiable, sans didascalie ni conséquence annoncée pour tout le groupe. Cet ajustement éditorial n'a pas fait l'objet d'un nouvel appel au modèle.

Le contrôle confirme l'acceptation des deux demandes sans cible réelle ; il ne mesure pas la variété ou la qualité de toutes les répliques.

## Clarification : une attaque verbale

Hugo précise que « attaque » signifie verbalement : Eris reste une IA et ne doit pas raconter de scène physique. Les consignes d'ordre 66, celles des relances et la phrase de voix générale qui incitait à « ouvrir en deux » sont corrigées ensemble. Le contexte dédié s'appelle désormais `Order66Turn`. Les contrôles d'identité et l'exclusion de l'auteur restent actifs.

Le premier petit contrôle réel de `TestLiveOrder66VerbalAttackAndDemo` utilise deux générations : une attaque ciblée sur des propos arrogants synthétiques, puis un exemple sans cible. La première réponse est verbale et vise l'ID attendu. La seconde invite encore à fournir le test ; ce résultat ne valide donc pas l'exécution immédiate de l'exemple. Ces deux réponses consomment 8 423 jetons.

La consigne de relance demande ensuite de produire directement l'exemple en inventant un propos banal d'un interlocuteur fictif, sans nouvelle confirmation. Seul le sous-test `untargeted_example` est rejoué : une génération, 5 567 jetons. Il produit une répartie à propos d'un rangement de bureau, sans narration physique ni destinataire Discord. Total de ce contrôle : trois générations, 13 990 jetons. Aucun outil externe ni message Discord envoyé.

Les assertions éditoriales signalent certaines formulations physiques déjà observées ; elles ne constituent pas une preuve sémantique exhaustive. Aucun filtre de mots n'est ajouté au bot et les réparties restent libres.
