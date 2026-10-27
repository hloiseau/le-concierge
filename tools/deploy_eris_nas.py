"""Pin API/bot to a published revision, with private SQLite backups and rollback.

Run on the TrueNAS host. Dry-run by default; --apply performs the image update.
Configuration, credentials and database contents are never printed.
"""
import argparse
import copy
from contextlib import closing
import datetime
import json
import os
from pathlib import Path
import re
import sqlite3
import subprocess
import time
import urllib.request

REPOSITORY = 'ghcr.io/hloiseau/le-concierge'
CONTAINERS = ['le-concierge-api', 'le-concierge-bot', 'le-hub', 'eris-runtime', 'eris-auth']


def prepare(config, image):
    if not re.fullmatch(re.escape(REPOSITORY) + r'@sha256:[a-f0-9]{64}', image):
        raise ValueError('expected an immutable published image')
    updated = copy.deepcopy(config)
    for name in ('api', 'bot'):
        updated['services'][name]['image'] = image
    return updated


def inspect(name):
    result = subprocess.run(['docker', 'inspect', name], capture_output=True, text=True, check=True)
    return json.loads(result.stdout)[0]


def private_file(path):
    return os.fdopen(os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | getattr(os, 'O_NOFOLLOW', 0), 0o600), 'w')


def backup_database(source, destination):
    if not source.is_file() or source.is_symlink():
        raise ValueError('expected an existing database file')
    with private_file(destination):
        pass
    with closing(sqlite3.connect(source.as_uri() + '?mode=ro', uri=True)) as src, closing(sqlite3.connect(destination)) as dst:
        src.backup(dst)
        if dst.execute('PRAGMA quick_check').fetchone()[0] != 'ok':
            raise RuntimeError('invalid SQLite backup')


def healthy():
    deadline = time.monotonic() + 180
    while time.monotonic() < deadline:
        try:
            items = [inspect(name) for name in CONTAINERS]
            if all(x['State']['Running'] and not x['State']['OOMKilled'] and x['State'].get('Health', {}).get('Status') == 'healthy' for x in items):
                return
        except subprocess.SubprocessError:
            pass
        time.sleep(2)
    raise RuntimeError('container health deadline')


def database_status(root):
    with closing(sqlite3.connect((root / 'concierge.db').as_uri() + '?mode=ro', uri=True)) as db:
        version = db.execute('SELECT max(version) FROM schema_migrations').fetchone()[0]
        assert db.execute('PRAGMA quick_check').fetchone()[0] == 'ok'
    with closing(sqlite3.connect((root / 'eris-character/character.db').as_uri() + '?mode=ro', uri=True)) as db:
        assert db.execute('PRAGMA quick_check').fetchone()[0] == 'ok'
        tables = {row[0] for row in db.execute("SELECT name FROM sqlite_master WHERE type='table'")}
        assert {'eris_turns', 'eris_notes', 'eris_summaries', 'eris_participation'} <= tables
        channels = db.execute('SELECT count(*) FROM eris_participation').fetchone()[0]
    return {'main_schema': version, 'participation_channels': channels}


def discord_ready(bot):
    deadline = time.monotonic() + 60
    required = {'Discord mentions ready', 'Discord guild commands updated'}
    while time.monotonic() < deadline:
        logs = subprocess.run(['docker', 'logs', '--since', bot['State']['StartedAt'], 'le-concierge-bot'], capture_output=True, text=True, check=True)
        observed = set()
        for line in (logs.stdout + '\n' + logs.stderr).splitlines():
            try:
                observed.add(json.loads(line).get('msg'))
            except (ValueError, AttributeError):
                pass
        if required <= observed:
            break
        time.sleep(2)
    else:
        raise RuntimeError('Discord readiness deadline')
    env = dict(value.split('=', 1) for value in bot['Config']['Env'])
    token = env.get('DISCORD_BOT_TOKEN', '')
    if not token:
        target = env['DISCORD_BOT_TOKEN_FILE']
        mount = max((m for m in bot['Mounts'] if target == m['Destination'] or target.startswith(m['Destination'].rstrip('/') + '/')), key=lambda m: len(m['Destination']))
        token = (Path(mount['Source']) / Path(target).relative_to(mount['Destination'])).read_text().strip()
    url = 'https://discord.com/api/v10/applications/' + env['DISCORD_CLIENT_ID'] + '/guilds/' + env['DISCORD_GUILD_ID'] + '/commands'
    request = urllib.request.Request(url, headers={'Authorization': 'Bot ' + token, 'User-Agent': 'ConciergeReleaseCheck/1.0'})
    with urllib.request.urlopen(request, timeout=15) as response:
        commands = json.load(response)
    command = next(item for item in commands if item['name'] == 'eris-salon')
    assert command['default_member_permissions'] == '8'
    option = next(item for item in command['options'] if item['name'] == 'participation')
    assert {item['value'] for item in option['choices']} == {'activer', 'desactiver', 'etat'}
    return {'gateway_ready': True, 'channel_command_registered': True}


def main():
    from truenas_api_client import Client
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--root', type=Path, required=True)
    parser.add_argument('--revision', required=True)
    parser.add_argument('--previous-revision', required=True)
    parser.add_argument('--apply', action='store_true')
    args = parser.parse_args()
    assert all(re.fullmatch(r'[a-f0-9]{40}', value) for value in (args.revision, args.previous_revision))
    root = args.root.resolve(strict=True)
    with Client() as client:
        current = client.call('app.config', 'media-hub')
        before = {name: inspect(name) for name in CONTAINERS}
        previous_model = env_model(before['le-concierge-bot'])
        for service, name in zip(('api', 'bot'), CONTAINERS[:2]):
            assert before[name]['Config']['Labels']['org.opencontainers.image.revision'] == args.previous_revision
            assert before[name]['Config']['Image'] == current['services'][service]['image']
        previous_status = database_status(root)
        print(json.dumps({'phase': 'preflight', 'previous_revision': args.previous_revision, 'target_revision': args.revision, **previous_status}), flush=True)
        if not args.apply:
            return
        healthy()
        tag = REPOSITORY + ':sha-' + args.revision
        subprocess.run(['docker', 'pull', tag], capture_output=True, check=True)
        image = inspect(tag)
        assert image['Config']['Labels']['org.opencontainers.image.revision'] == args.revision
        pinned = next(value for value in image['RepoDigests'] if value.startswith(REPOSITORY + '@sha256:'))
        updated = prepare(current, pinned)
        subprocess.run(['docker', 'compose', '-f', '-', 'config', '--quiet'], input=json.dumps(updated), text=True, capture_output=True, check=True)
        backups = root / 'ops-backups'
        assert backups.stat().st_mode & 0o777 == 0o700
        bundle = backups / ('before-eris-reconciled-' + datetime.datetime.now(datetime.timezone.utc).strftime('%Y%m%dT%H%M%SZ'))
        bundle.mkdir(mode=0o700)
        def permissions(path, mode):
            client.call('filesystem.setperm', {'path': str(path), 'mode': mode, 'options': {'stripacl': True, 'recursive': False, 'traverse': False}}, job=True)
            assert client.call('filesystem.getacl', str(path))['trivial']
        permissions(bundle, '700')
        with private_file(bundle / 'config-before.json') as stream:
            json.dump(current, stream)
            stream.flush()
            os.fsync(stream.fileno())
        backup_database(root / 'concierge.db', bundle / 'concierge.db')
        backup_database(root / 'eris-character/character.db', bundle / 'character.db')
        for name in ('config-before.json', 'concierge.db', 'character.db'):
            permissions(bundle / name, '600')
        assert client.call('app.config', 'media-hub') == current, 'configuration changed during preparation'
        print(json.dumps({'phase': 'backups_ready', 'private_backup_name': bundle.name, 'image': pinned, 'image_id': image['Id']}), flush=True)
        try:
            client.call('app.update', 'media-hub', {'custom_compose_config': updated}, job=True)
            healthy()
            for name in CONTAINERS[:2]:
                actual = inspect(name)
                assert actual['Image'] == image['Id']
                assert actual['Config']['Labels']['org.opencontainers.image.revision'] == args.revision
            for name in CONTAINERS[2:]:
                assert inspect(name)['Image'] == before[name]['Image']
            for name, binary in zip(CONTAINERS[:2], ('api', 'bot')):
                subprocess.run(['docker', 'exec', name, '/app/' + binary, '--healthcheck'], capture_output=True, check=True)
            status = database_status(root)
            assert status['main_schema'] == previous_status['main_schema']
            assert env_model(inspect('le-concierge-bot')) == previous_model
            discord = discord_ready(inspect('le-concierge-bot'))
        except Exception:
            client.call('app.update', 'media-hub', {'custom_compose_config': current}, job=True)
            healthy()
            assert all(inspect(name)['Image'] == before[name]['Image'] for name in CONTAINERS)
            print(json.dumps({'phase': 'rolled_back', 'databases_restored': False}), flush=True)
            raise
        receipt = {'phase': 'deployed', 'revision': args.revision, 'image': pinned, 'image_id': image['Id'], 'services': ['api', 'bot'], 'model': env_model(inspect('le-concierge-bot')), **status, **discord}
        with private_file(bundle / 'receipt.json') as stream:
            json.dump(receipt, stream, indent=2)
        permissions(bundle / 'receipt.json', '600')
        print(json.dumps(receipt), flush=True)


def env_model(bot):
    env = dict(value.split('=', 1) for value in bot['Config']['Env'])
    assert env['ERIS_PROFILE'] == 'v2' and env['ERIS_PROTOCOL'] == 'responses'
    return env['ERIS_MODEL']


if __name__ == '__main__':
    try:
        main()
    except Exception as error:
        print(json.dumps({'phase': 'failed', 'type': type(error).__name__}), flush=True)
        raise SystemExit(1)
