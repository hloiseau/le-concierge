# Déployer et tester sur TrueNAS SCALE 25.10

Les deux premières versions sont fusionnées dans `main`. Les images ci-dessous ont été construites et publiées par GitHub Actions ; leur téléchargement anonyme a été vérifié le 15 octobre 2026. Le fonctionnement sur le NAS et avec tes comptes reste à tester.

## 1. Choisir le YAML

| Fichier | Usage |
| --- | --- |
| [truenas.yaml](../deploy/truenas.yaml) | API + Hub ; démarre sans identifiants pour vérifier l'accueil et les liens |
| [truenas-discord.yaml](../deploy/truenas-discord.yaml) | API + Hub + bot ; renseigner d'abord l'application Discord |
| [Caddyfile.truenas](../deploy/Caddyfile.truenas) | Deux blocs à ajouter au Caddy existant |
| [truenas-admin.sh](../deploy/truenas-admin.sh) | Vérification, enregistrement Discord, association Seerr et sauvegarde |

**Une seule application TrueNAS**, nom suggéré `media-hub`. Pour ajouter le bot plus tard, remplacer le YAML de cette application par la variante complète en conservant les paramètres et le dataset. Ne pas installer les deux variantes en parallèle : elles utilisent les mêmes noms de conteneurs et la même base.

Les deux dépôts GitHub restent séparés ; le déploiement réunit leurs services dans le même réseau Docker. L'API ne publie aucun port. Le Hub utilise `18080`, le bot `18081`, liés à l'IP LAN choisie. Vérifier que ces ports sont libres ; ils ne remplacent pas les ports HTTP/HTTPS de Caddy.

## 2. Préparer le dataset et les valeurs

Créer dans **Datasets** un dataset dédié, par exemple `Main/tank/concierge`, monté sous `/mnt/Main/tank/concierge`. Accorder lecture/écriture/traversée à l'UID/GID **65532**. Pour un dataset neuf avec permissions POSIX, dans le Shell TrueNAS :

```bash
sudo chown 65532:65532 /mnt/Main/tank/concierge
sudo chmod 700 /mnt/Main/tank/concierge
```

Ces commandes visent uniquement le nouveau dossier : ne pas les appliquer au dataset des médias ou à son parent. Si le dataset utilise des ACL, régler l'accès de l'UID 65532 dans l'éditeur ACL. Le montage refuse de créer silencieusement un dossier manquant.

Remplacer dans le YAML :

| Valeur | À mettre |
| --- | --- |
| `192.0.2.10` dans les ports | IP LAN réelle du NAS, joignable depuis Caddy |
| `PUBLIC_ORIGIN` | `https://hub.hugoloiseau.fr`, ou le domaine choisi, sans chemin |
| Chemin `source` du volume | Chemin du dataset créé |
| `SERVICE_LINKS_JSON` | URLs publiques HTTPS de tes services ; laisser vides les liens inconnus |
| `NUXT_PUBLIC_DISCORD_URL` | Invitation Discord, facultative |
| `PLEX_URL` + `PLEX_SERVER_ID` | Adresse interne PMS et son `machineIdentifier` ; remplir ensemble |
| `SEERR_URL` + `SEERR_API_KEY` | Adresse interne Seerr et clé API ; remplir ensemble |
| Variables `DISCORD_*` | Voir le [mini tutoriel Discord](discord-quickstart.md) |

L'adresse interne peut être HTTP, par exemple `http://IP_DU_NAS:32400`. `localhost` dans un conteneur désigne ce conteneur, pas le NAS. Le `machineIdentifier` Plex est notamment exposé par le PMS dans `/identity` lorsque cette route est accessible sur ton réseau. Aucun token propriétaire Plex n'est demandé : chaque utilisateur lie son propre compte.

Les secrets du YAML restent dans l'administration TrueNAS et dans ton gestionnaire de mots de passe ; ne pas publier le fichier rempli sur GitHub. Si un secret contient un `$`, Compose attend `$$` pour conserver ce caractère littéral. Les variantes `_FILE` sont également prises en charge, voir le [guide détaillé](deployment.md).

## 3. Installer et ajouter Caddy

Dans **Apps → Discover Apps → ⋮ → Install via YAML**, choisir `media-hub` et coller le YAML adapté. Attendre que l'API et le Hub soient sains. Le YAML complet exige aussi une clé publique Discord et les identifiants valides ; le tutoriel Discord commence par leur récupération.

Créer les entrées DNS pour `hub.hugoloiseau.fr` et, si le bot est utilisé, `concierge.hugoloiseau.fr`, vers l'accès public déjà géré par Caddy. Ajouter les blocs du [fichier Caddy](../deploy/Caddyfile.truenas) à ton fichier existant en remplaçant l'IP LAN. Conserver son bloc global, ses ports `8081/8443`, ses volumes et les autres sites. Le second bloc peut attendre l'installation du bot.

Valider puis recharger Caddy avec ta procédure habituelle. Ne pas rediriger les ports `18080/18081` depuis la box : l'accès public passe par Caddy en HTTPS. Ne pas utiliser `127.0.0.1` comme upstream si Caddy est dans un conteneur séparé.

Ouvrir **https://hub.hugoloiseau.fr**. L'accès HTTP direct au port LAN peut servir à vérifier la réponse du serveur, mais la connexion et les mutations exigent l'origine HTTPS configurée.

## 4. Vérifier, puis tester les comptes

Télécharger [truenas-admin.sh](../deploy/truenas-admin.sh) sur le NAS, ou le copier avec ton transfert habituel :

```bash
curl -fL -o truenas-admin.sh https://raw.githubusercontent.com/hloiseau/le-concierge/main/deploy/truenas-admin.sh
```

Depuis le dossier où tu l'as placé :

```bash
sudo bash truenas-admin.sh check
```

Ce contrôle vérifie API/SQLite, le passage Hub → API et la santé HTTP du bot s'il est installé. Il ne valide pas tes identifiants auprès des fournisseurs.

Après le tutoriel Discord : se connecter au Hub, ouvrir **Mon compte**, puis **Lier Plex**. Une fois l'identité Plex vérifiée, associer le bon utilisateur Seerr après comparaison avec son compte réel dans l'administration Seerr :

```bash
sudo bash truenas-admin.sh link-seerr ID_DISCORD ID_SEERR
```

Le script demande des identifiants numériques. Il ne déduit jamais l'association d'un pseudo. Tester avec ton compte puis avec un ami : chacun doit voir ses seules demandes et ses bibliothèques Plex autorisées. Tester `/aide`, `/mes-demandes`, `/nouveautes` dans le serveur Discord configuré.

## 5. Images, mise à jour et sauvegarde

Les YAML figent cette première paire validée :

- API/bot : `ghcr.io/hloiseau/le-concierge:sha-ed5d6d2008ea5f0d6e4f7d4e2d0d9c359244bda2`
- Hub : `ghcr.io/hloiseau/le-hub:sha-a000edd9047061821ebf49a9c2d1162b133a9c72`

Pour suivre les prochaines fusions, le tag disponible est **`edge`**, dans les deux dépôts. `main` et `latest` ne sont pas publiés par cette CI. Un tag ne provoque pas à lui seul la mise à jour d'une application déjà lancée : demander à TrueNAS de récupérer/redéployer l'image après sa publication.

Avant une mise à jour :

```bash
sudo bash truenas-admin.sh backup
```

Conserver aussi une copie hors du dataset de l'application et noter les tags précédents. Le guide détaillé décrit la restauration SQLite et le retour arrière. Les sessions ne survivent pas au redémarrage : reconnecter Discord puis Plex. L'association Seerr et les préférences restent en base.

## Dépannage rapide

| Symptôme | Vérification |
| --- | --- |
| `permission denied` / SQLite impossible à ouvrir | Dataset existant, UID 65532 autorisé, traversée des dossiers parents |
| `cannot assign requested address` | IP LAN fictive `192.0.2.10` encore présente |
| Caddy 502 | Port du Hub, IP accessible depuis Caddy, santé des conteneurs |
| Connexion désactivée | Client ID + secret OAuth2 + guild ID dans l'API |
| Discord refuse le callback | URL exacte et même domaine que `PUBLIC_ORIGIN` |
| Erreur 403 lors d'une action Hub | Ouvrir le domaine HTTPS configuré, pas une IP ou un alias |
| Demandes Seerr en attente de liaison | Lier Plex puis exécuter `link-seerr` avec l'identité vérifiée |
| Bot hors ligne dans la liste Discord | Normal pour cette version HTTP sans Gateway ; tester `/aide` |
| Image refusée au téléchargement | Vérifier le nom/tag exact ; les deux images listées ont été vérifiées accessibles sans connexion |

Les rappels Maintainerr/Tautulli, le catalogue livres et le monitoring restent à développer. Ne pas utiliser ces pages comme preuve qu'un envoi ou une collecte est déjà actif.

Sources : [interface Apps TrueNAS 25.10](https://www.truenas.com/docs/scale/25.10/scaleuireference/apps/), [installation d'applications personnalisées](https://apps.truenas.com/managing-apps/installing-custom-apps/).
