# Mesures du serveur et activité Plex

Les tickets Concierge #16, #17, #18 et Hub #11, #12 disposent d'un chemin complet : sources en lecture seule → API privée → historique SQLite → page `/performances`. Les mesures s'activent dès qu'une source est configurée : Tautulli avec son identité Plex, Toparr, ou un fichier de mesures hôte. L'activité Plex et le GPU Toparr fonctionnent indépendamment du collecteur hôte.

## État de la validation

Validation sur le NAS les 17–18 octobre 2026 : TrueNAS 25.10.7, Intel Arc A310 `8086:56a6`, pilote i915, PCI `03:00.0`. Le collecteur hôte est installé sous UID 3001, sans capacité effective (`CapEff=0`), et planifié toutes les 60 secondes. CPU, RAM, température CPU `k10temp:Tctl`, réseau physique `enp5s0`, disques et pool ZFS `Main` sont ingérés. Les cinq disques sélectionnés incluent le cache et excluent le disque de démarrage. Une collecte a pris 1,04 s écoulée, 0,062 s CPU, environ 21 Mio RSS et produit 3233 octets. L'API UID 65532 lit le dossier via un montage en lecture seule ; ACL et absence de droit d'écriture vérifiées. Le contrôle Tautulli 2.18.2 et de son identité Plex réussit.

L'instance Toparr 2.0.1 existante expose un seul GPU `card0`, Arc A310, sélection automatique. Deux GET depuis le client Go ont pris environ 9 ms et fourni un horodatage frais, 14,7 W, 0 MHz, et des moteurs absents. Le raccordement est déployé : la puissance et la fréquence ont été vérifiées dans l'historique SQLite après redémarrage. Ces valeurs viennent de Toparr ; l'absence de moteur n'est pas interprétée comme un GPU inactif. Aucun transcodage réel n'a été déclenché pour cette validation. La comparaison pendant un transcodage et le coût après une journée restent à vérifier ; ces observations ponctuelles ne suffisent pas à clôturer toute la validation matérielle de #16.

Sources primaires et correspondances :

- [TrueNAS 25.10 reporting.realtime](https://api.truenas.com/v25.10.0/api_events_reporting.realtime.html) documente CPU en %, température nullable en °C, mémoire en octets, débits disque/réseau en octets/s et rôle `REPORTING_READ`. Il ne promet pas de métrique Intel Arc.
- [TrueNAS 25.10 reporting.netdata_get_data](https://api.truenas.com/v25.10.0/api_methods_reporting.netdata_get_data.html) expose les graphes CPU, charge, mémoire, disque, interface et ARC. L'API versionnée utilise JSON-RPC/WebSocket ; aucun endpoint REST ou nom de graphe GPU hypothétique n'est ajouté au client.
- [Tautulli 2.18.2 get_activity](https://github.com/Tautulli/Tautulli/blob/v2.18.2/plexpy/webserve.py) et [parseur Plex de cette version](https://github.com/Tautulli/Tautulli/blob/v2.18.2/plexpy/pmsconnect.py) définissent décisions, codecs, débit en kbit/s et indicateurs réels de décodeur/encodeur matériel. L'API vérifie la version et le `pms_identifier` attendu avant chaque lecture fraîche.
- [Linux proc](https://www.kernel.org/doc/html/latest/filesystems/proc.html), [statistiques disque](https://www.kernel.org/doc/html/latest/admin-guide/iostats.html) et [capteurs hwmon](https://docs.kernel.org/hwmon/sysfs-interface.html) définissent les compteurs CPU, mémoire, réseau, disque et températures. Le compteur de secteurs disque utilise des secteurs de 512 octets ; les valeurs `temp*_input` utilisées ici sont en milli-degrés Celsius. Le choix du capteur est explicite : aucun `hwmon0` arbitraire n'est assimilé au CPU.
- [Manuel intel_gpu_top livré par intel-gpu-tools](https://manpages.debian.org/bookworm/intel-gpu-tools/intel_gpu_top.1.en.html) décrit JSON, sélection DRM explicite, intervalle et limites de permissions/perf. [Source du producteur JSON](https://github.com/mkahola/igt/blob/master/tools/intel_gpu_top.c) consultée pour les champs et unités ; le format exact installé doit encore être confronté à une capture réelle. Le premier échantillon de durée quasi nulle est rejeté.
- [OpenZFS zpool-list](https://openzfs.github.io/openzfs-docs/man/master/8/zpool-list.8.html) définit la commande de lecture `list`, les colonnes exactes, les options sans en-tête et sans arrondi. Le stockage présenté correspond aux pools explicitement choisis, pas à une estimation de taille des médias.

## Activation et diagnostic

La configuration Tautulli/Plex existante suffit pour afficher l'activité Plex : laisser `MONITORING_ENABLED` absent ou vide. `MONITORING_ENABLED=false` reste une désactivation explicite, y compris lorsqu'une source est configurée. Une installation issue de l'ancien exemple peut encore avoir cette valeur `false` : la retirer ou utiliser `true` lors de la mise à jour. Le Hub choisit l'historique des sessions Plex par défaut lorsqu'aucun collecteur hôte n'est relié.

L'activité Plex ne fournit pas CPU, RAM, GPU ou réseau du NAS. CPU, RAM et réseau utilisent le collecteur ci-dessous, son dossier partagé en lecture seule et `METRICS_FILE` ; le GPU peut utiliser l'API Toparr existante. Le champ `hostStatus` de `/api/v1/monitoring` distingue `not_configured` (aucun fichier prévu), `error` (lecture ou validation impossible), `stale` (plus de deux minutes) et `available`. Le statut de cette source ne masque ni Toparr ni Plex. Un échec de chargement de l'historique ou des détails administrateur ne masque plus les observations actuelles.

Le 17 octobre 2026, trois lectures du Tautulli configuré dans l'environnement de développement ont réussi : version 2.18.2, identité Plex concordante et activité valide. Cet environnement avait Tautulli/Plex configurés, sans `MONITORING_ENABLED` ni `METRICS_FILE`, ce qui permet de reproduire le masquage par l'ancien défaut `false`. Ce contrôle ne prouve pas la configuration des conteneurs déployés sur le NAS et ne valide aucun capteur matériel.

Configuration API :

```dotenv
MONITORING_ENABLED=true
METRICS_FILE=/run/concierge-metrics/host.json
# Facultatif : URL interne de l'instance Toparr déjà installée.
TOPARR_URL=http://192.0.2.10:30391
```

`METRICS_FILE` peut rester vide pour Plex ou Toparr seuls. Les variables Tautulli/Plex déjà utilisées par les départs sont réutilisées. Aucun secret Tautulli n'est transmis au navigateur. Le fichier et son dossier sont gérés par l'opérateur ; l'API les monte en lecture seule et n'obtient ni accès `/dev/dri`, ni socket Docker, ni privilège d'administration TrueNAS.

## GPU depuis Toparr

`TOPARR_URL` désigne l'instance existante, joignable depuis l'API. Aucun changement de privilège, de périphérique ou de tâche NAS n'est nécessaire pour ce raccordement. Le contrat consulté est [Toparr v2.0.1, commit b0b8109](https://github.com/ajthom90/toparr/tree/v2.0.1) : cette version lit sysfs et fdinfo directement, contrairement à l'ancienne architecture `intel_gpu_top` encore décrite par son README.

Le client effectue uniquement `GET /api/gpus` et `GET /api/status`. Il extrait `current.timestamp` (secondes Unix), les moteurs Video et Render/3D (%), `frequency.actual` (MHz) et `power.GPU` (W). Ces unités sont implicites dans le [backend 2.0.1](https://github.com/ajthom90/toparr/blob/v2.0.1/app/backends/intel.py). Les moteurs Toparr sont agrégés à partir des clients fdinfo et bornés à 100 % ; ce n'est pas une mesure PMU. VideoEnhance n'est pas assimilé à Video, et `power.PKG` représente le package CPU, jamais la seule puissance GPU. Toparr peut renvoyer 0 MHz lorsque la lecture sysfs échoue : cette version ne distingue pas ce cas du repos. Aucun `gpu_busy` de repli n'est utilisé.

Les [routes Toparr](https://github.com/ajthom90/toparr/blob/v2.0.1/app/main.py) omettent le pilote dans l'inventaire et ne garantissent pas l'isolation des clients entre plusieurs cartes du même pilote. Le client accepte donc uniquement un inventaire non ambigu d'une carte, avec une sélection concordante ou automatique. Il ne modifie jamais la sélection globale. La température GPU éventuelle reste fournie par le fichier hôte.

`toparrStatus` et `toparrUpdatedAt` sont indépendants de `hostStatus` : `not_configured`, `unavailable` au démarrage sans observation, `available`, `stale` après 30 secondes, ou `error` lors d'un échec HTTP/contrat. Les quatre mesures Toparr remplacent celles du fichier hôte lorsque cette source est configurée. Une erreur ne réutilise pas d'ancienne mesure ; un moteur ou une puissance absente reste `null`, tandis qu'un zéro explicitement fourni est conservé. Le [moniteur Toparr](https://github.com/ajthom90/toparr/blob/v2.0.1/app/gpu_monitor.py) peut conserver une ancienne observation après erreur : l'horodatage source est donc vérifié et limite la durée du cache. Un horodatage de plus de 15 secondes dans le futur est rejeté.

Les deux lectures partagent un délai de huit secondes et le cache monitoring de 30 secondes. La fraîcheur GPU est revérifiée après la lecture des autres sources ; son échéance invalide le cache sans raccourcir `expiresAt` pour CPU/Plex. Le Hub dérive `toparrUpdatedAt + 30 s` et masque uniquement les quatre mesures Toparr à cette échéance. Chaque réponse est bornée à 2 Mio. L'historique brut, les PID, noms et commandes des processus Toparr ne sont ni retenus, ni publiés, ni historisés ; seules les quatre mesures entrent dans l'historique agrégé existant. Le flux SSE n'est pas nécessaire pour la cadence actuelle du Hub.

## Collecteur hôte

`tools/collect_metrics.py` est un programme Python 3 standard, indépendant du serveur web. Il ne contacte aucun réseau et ne change ni matériel, ni ZFS, ni permissions système. Exemple à adapter **après** identification des vrais périphériques, exécuté sous un compte dédié sur l'hôte Linux :

```sh
python3 tools/collect_metrics.py --output /run/concierge-metrics/host.json \
  --interface enp1s0 --disk sda --pool media \
  --cpu-temperature /sys/class/hwmon/hwmon2/temp1_input \
  --gpu-device /dev/dri/card0 --interval 60
```

Ces noms sont des exemples, pas les noms constatés du NAS. Commencer par `--once` et par CPU/mémoire sans options matérielles. Ne pas sélectionner à la fois un disque et ses partitions, ni un bridge réseau et son interface physique : leurs débits se recouvrent. Un collecteur conteneurisé doit réellement lire les compteurs hôte ; monter `/proc` ne garantit pas à lui seul le bon espace de noms réseau. L'exécution hôte isolée évite cette ambiguïté.

Permissions minimales : lecture des seuls compteurs `/proc` et capteurs sélectionnés ; écriture uniquement du dossier de sortie ; lecture par le groupe de l'API du fichier en mode 0640. GPU : accès au périphérique Intel explicitement sélectionné et permissions de compteurs perf compatibles avec la politique de l'hôte. ZFS : uniquement les droits nécessaires à `zpool list` sur les pools sélectionnés. Si ces droits manquent, les mesures concernées restent en erreur. Ne pas utiliser `--privileged`, `CAP_SYS_ADMIN`, un compte administrateur du NAS, un socket Docker ou assouplir globalement `perf_event_paranoid` pour faire disparaître cet état. Aucun service/permission de cette nature n'est activé par ce projet.

Le collecteur écrit un fichier temporaire dans le même dossier, synchronise puis remplace atomiquement le snapshot. Monter le **dossier** en lecture seule dans l'API, plutôt que l'inode d'un fichier remplacé. Le dossier doit être fiable et non modifiable par les utilisateurs du Hub. Le snapshot n'est pas une interface d'upload. Budget : 32 Kio, 32 détails maximum, noms et métriques explicitement bornés.

## Sens des mesures

Chaque valeur comprend `value`, `unit`, `status`. `available` peut valoir réellement zéro ; `unavailable` signifie non sélectionné, non supporté, compteur réinitialisé ou information insuffisante ; `error` signifie lecture/validation échouée ; `stale` signifie snapshot hôte de plus de deux minutes. Les états autres qu'`available` ont une valeur `null`.

| Mesure | Calcul et unité |
|---|---|
| CPU | Différence sur une seconde des huit premiers compteurs `/proc/stat`, sans recompter les invités ; temps actif hors idle/iowait, % |
| Charge | Première valeur `/proc/loadavg`, charge moyenne sur une minute, pas un pourcentage |
| RAM | `MemTotal - MemAvailable`, octets ; total séparé, pas une somme de RSS de processus |
| Températures | Capteurs choisis par l'opérateur, milli-°C divisés par 1000 |
| GPU vidéo/rendu | Maximum d'occupation des moteurs Video / Render/3D reconnus, % ; VideoEnhance exclu de Video ; aucune somme de moteurs ni estimation par nombre de transcodages |
| GPU fréquence/puissance | `frequency.actual` si unité MHz / `power.GPU` si unité W ; restent absents si le pilote ne les fournit pas |
| Disques/réseau | Différences de compteurs des seuls périphériques sélectionnés divisées par le temps monotone réel, octets/s ; reset ou périphérique absent donne un trou |
| ZFS | Somme size/alloc des pools choisis, octets ; nom et état de chaque pool réservés aux administrateurs |
| Plex | Sessions, lecture directe, flux directs, transcodages, débit kbit/s ; indicateur matériel uniquement si Plex le signale, inconnu conservé comme null |

Une absence de transcodage ne signifie jamais GPU à 0 %. Les sessions en pause restent des sessions ; le débit déclaré par Plex n'est pas présenté comme le débit mesuré de la carte réseau.

## Historique, charge et confidentialité

- Collecteur : cadence recommandée 60 s, fenêtre CPU/réseau/disque 1 s ; GPU optionnel `intel_gpu_top` limité à environ 4,2 s, ZFS à 3 s. Les lectures texte et sortie GPU sont bornées. Une cadence supérieure à 120 s produit volontairement des périodes périmées entre les observations.
- API : cache partagé 30 s et une seule collecte Tautulli à la fois ; trois commandes de lecture Tautulli par actualisation, délai global 8 s. Aucun retour à d'anciennes sessions après une erreur. La péremption est raccourcie si un snapshot approche de ses deux minutes.
- SQLite : un échantillon agrégé par minute, au plus 10 080 lignes, conservation sept jours, remplacement d'une minute déjà enregistrée. Aucun nom d'utilisateur/titre/adresse/identifiant matériel n'est historisé. Le redémarrage préserve les lignes ; aucune interpolation pendant l'arrêt.
- Fenêtres API fixes : 1 h / pas 1 min, 24 h / pas 5 min, 7 j / pas 30 min ; au maximum 337 points. Agrégation moyenne. Une panne ou un trou entre observations d'un même bucket rend la valeur absente ; les buckets totalement absents sont omis, et la courbe les coupe explicitement. Les buckets partiels aux bords portent la moyenne des observations disponibles.
- Chaque payload stocké est limité à 8 Kio, donc moins de 79 Mio de payloads pour sept jours au plafond. Prévoir au moins 128 Mio supplémentaires pour cette fonction, plus marge WAL/SQLite et sauvegardes partagées. Ce plafond n'est pas une garantie sur la taille totale de la base (autres tables). Le coût d'une collecte a été mesuré ci-dessus ; le volume réel et le coût cumulé après une journée restent à vérifier.
- Les routes ordinaires exigent une session et un accès Plex encore valides. Elles renvoient seulement agrégats. `/api/v1/admin/plex-activity` vérifie le statut administrateur avant lecture et à nouveau après lecture ; seuls noms/titres/codecs, indicateurs matériels et détails des équipements autorisés y figurent. Aucun IP, chemin de fichier, jeton ou identifiant de session brut.
- Le Hub poll toutes les 30 s, suspend en onglet masqué, invalide après logout, retire les valeurs actuelles à leur expiration. L'historique affiche sa propre date de chargement ; il ne prétend pas que les noms de sessions sont historiques. Zoom, infobulles, clavier et tableau utilisent les mêmes points.

## Vérifications avant production

1. Confirmer la version TrueNAS installée et noter la version du noyau/pilote Intel ; lire le vendor/device du DRM choisi et vérifier qu'il s'agit bien de l'Arc A310 visée, pas d'un autre GPU Intel.
2. Vérifier en lecture seule les unités et noms de capteurs ainsi que la présence de `intel_gpu_top -J` et `zpool list`. Confronter un snapshot `--once` au reporting TrueNAS.
3. Observer repos, lecture directe puis transcodage déjà autorisé par l'opérateur ; comparer les moteurs et les flags Plex sans déduire l'un de l'autre. Tester absence de permissions GPU et absence de capteur.
4. Vérifier la disparition des mesures lorsque le fichier n'est plus renouvelé, la survie de l'historique après redémarrage et l'absence de noms dans les réponses ordinaires/sauvegardes de métriques.
5. Mesurer le coût du collecteur et le volume réel après une journée. Aucune de ces validations matérielles n'est remplacée par les fixtures automatisées.

Tests locaux : `go test ./internal/monitoring ./internal/upstream ./internal/store ./internal/httpapi`, `python3 -m unittest discover -s tools -p test_collect_metrics.py`, `tests/monitoring.spec.ts` dans Le Hub.
