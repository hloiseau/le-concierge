# Essayer Eris ensemble

Le laboratoire `cmd/eris-lab` garde une conversation ouverte avec le vrai `Chat`
de la candidate et le modèle explicitement configuré. Il utilise Responses, une
base SQLite temporaire et le contexte public partagé entre interlocuteurs
synthétiques. Aucun accès Discord, Plex, Seerr ou monitoring n'est fourni.
Les outils renvoient un résultat explicitement simulé, jamais une vraie action.

Pour une séance dans le chat, Hugo écrit son message, l'agent transmet la ligne
au processus existant, puis rapporte le texte retourné sans le réécrire.
Le retour de Hugo est une observation pour la recette ; il n'entre pas dans le
dialogue d'Eris sauf demande explicite. Garder le prompt fixe pendant la séance.

L'entrée et la sortie utilisent une ligne JSON par tour. Le premier résultat
identifie le modèle, la révision fournie par le lanceur et l'empreinte du persona
avec le canon. La variable `ERIS_MODEL` sélectionne le modèle et
`ERIS_API_KEY_FILE` désigne le fichier d'authentification déjà provisionné ; aucune
clé n'est fournie dans la commande ou les messages. `ERIS_LAB_REVISION` consigne
la révision poussée du binaire. Le processus reste ouvert entre les messages.

Sur le NAS, `tools/eris_lab_nas.py --binary CHEMIN_BINAIRE --revision SHA_COMPLET`
lance un conteneur éphémère nommé `eris-lab-79`, avec le binaire Linux compilé
depuis cette révision. Le lanceur lit la configuration du bot pour reprendre son
modèle et monter uniquement le fichier du jeton du modèle en lecture seule.
Il ne modifie aucun service existant. Le conteneur s'arrête à la fermeture de
l'entrée ; la mémoire temporaire de la séance disparaît alors.
Si une rotation remplace ce fichier de jeton, relancer la séance utilise le nouveau.

```json
{"text":"Salut Eris, comment tu vas ?"}
{"speaker":"Alice","text":"Bob m'accuse de tricher à notre jeu.","ambient":true}
{"speaker":"Bob","text":"Eris, qu'est-ce que tu en penses ?","explicit":true}
{"speaker":"Hugo","text":"Merci, nickel 👍"}
{"speaker":"Hugo","forget":true}
```

Sans `speaker`, le nom est Hugo. Les identités et le salon sont fictifs et
distincts de ceux du vrai Discord. `ambient` fournit un message observé dans un
salon déjà actif, sans réponse ; `explicit` permet la reprise après silence,
comme un appel explicite. L'oubli efface les données du membre dans cette base
d'essai. Quitter puis relancer le processus repart d'une base neuve.

Une ligne contenant `silent:true` signifie qu'Eris s'est tue ; ne pas lui
inventer une réplique. Une erreur n'est pas une réponse du personnage. Les
résultats et commentaires de la séance restent privés par défaut ; seuls des
bilans nettoyés et des scénarios synthétiques peuvent être ajoutés au dépôt.

Ce laboratoire teste le langage, le contexte, la mémoire et le choix des outils.
Il ne teste pas le Gateway Discord, ses permissions, le classement des relances
implicites ou le rythme réel de participation spontanée. Ces points demandent
ensuite un essai dans un salon Discord choisi explicitement.
