# Ordre 66 : correction de voix du 19 novembre 2026

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
