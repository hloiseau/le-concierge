#!/usr/bin/env bash
# Shell hote du NAS, avec les droits Docker : sudo bash truenas-admin.sh ...
set +x
set -euo pipefail
api_container=le-concierge-api
bot_container=le-concierge-bot
hub_container=le-hub
command -v docker >/dev/null || { printf '%s\n' 'Docker introuvable : executer ce script sur le NAS.' >&2; exit 1; }
require_running() {
  local running
  running=$(docker inspect --format '{{.State.Running}}' "$1" 2>/dev/null) || { printf 'Conteneur introuvable : %s\n' "$1" >&2; exit 1; }
  [[ "$running" == true ]] || { printf 'Conteneur arrete : %s\n' "$1" >&2; exit 1; }
}
case "${1:-}" in
  check)
    require_running "$api_container"
    docker exec "$api_container" /app/api --healthcheck
    printf '%s\n' 'API : prete (SQLite accessible).'
    require_running "$hub_container"
    docker exec "$hub_container" node -e "fetch('http://127.0.0.1:3000/api/v1/capabilities').then(r=>{if(!r.ok)throw Error('HTTP '+r.status);return r.json()}).then(c=>console.log('Hub -> API : OK',JSON.stringify(c))).catch(()=>{console.error('Hub -> API : echec');process.exit(1)})"
    if docker inspect "$bot_container" >/dev/null 2>&1; then
      require_running "$bot_container"
      docker exec "$bot_container" /app/bot --healthcheck
      printf '%s\n' 'Bot : HTTP pret. Tester ensuite /aide dans Discord.'
    else
      printf '%s\n' 'Bot : non installe (facultatif).'
    fi
    ;;
  register)
    require_running "$bot_container"
    trap 'unset DISCORD_BOT_TOKEN' EXIT
    IFS= read -r -s -p 'Token du bot Discord (saisie masquee) : ' DISCORD_BOT_TOKEN </dev/tty
    printf '\n'
    [[ -n "$DISCORD_BOT_TOKEN" ]] || { printf '%s\n' 'Token vide.' >&2; exit 1; }
    export DISCORD_BOT_TOKEN
    # Docker lit la variable appelante ; sa valeur n'apparait pas dans les arguments.
    docker exec -e DISCORD_BOT_TOKEN "$bot_container" /app/bot --register
    unset DISCORD_BOT_TOKEN
    printf '%s\n' 'Commandes enregistrees. Tester /aide dans le serveur Discord.'
    ;;
  link-seerr)
    [[ $# == 3 && "$2" =~ ^[0-9]{5,24}$ && "$3" =~ ^[1-9][0-9]*$ ]] || { printf '%s\n' 'Usage : truenas-admin.sh link-seerr ID_DISCORD ID_SEERR' >&2; exit 1; }
    require_running "$api_container"
    docker exec "$api_container" /app/manage link-seerr --discord "$2" --seerr "$3"
    ;;
  backup)
    require_running "$api_container"
    backup_name="backup-$(date -u +%Y%m%dT%H%M%SZ).db"
    docker exec "$api_container" /app/manage backup --destination "/app/data/$backup_name"
    printf 'Sauvegarde dans le dataset Concierge : %s\n' "$backup_name"
    ;;
  *)
    printf '%s\n' 'Usage : truenas-admin.sh check | register | link-seerr ID_DISCORD ID_SEERR | backup' >&2
    exit 1
    ;;
esac
