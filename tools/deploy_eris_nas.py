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
import secrets
import sqlite3
import subprocess
import time
import urllib.request

REPOSITORY = 'ghcr.io/hloiseau/le-concierge'
CONTAINERS = ['le-concierge-api', 'le-concierge-bot', 'le-hub', 'eris-runtime', 'eris-auth']


def prepare(config, image, hub_image=None, web=None, ouicheur_origin=None):
    if not re.fullmatch(re.escape(REPOSITORY) + r'@sha256:[a-f0-9]{64}', image):
        raise ValueError('expected an immutable published image')
    updated = copy.deepcopy(config)
    for name in ('api', 'bot'):
        updated['services'][name]['image'] = image
    if hub_image:
        if not re.fullmatch(r'ghcr.io/hloiseau/le-hub@sha256:[a-f0-9]{64}', hub_image):
            raise ValueError('expected immutable Hub image')
        updated['services']['hub']['image'] = hub_image
    if web:
        if 'searxng' in updated['services'] or config['services']['api'].get('networks'):
            raise ValueError('web activation requires an unconfigured service on the default app network')
        updated['services']['searxng'] = copy.deepcopy(web)
        updated['services']['api'].setdefault('environment', {})['ERIS_WEB_URL'] = 'http://searxng:8080'
    if ouicheur_origin:
        from urllib.parse import urlsplit
        origin = urlsplit(ouicheur_origin)
        if origin.scheme != 'https' or not origin.hostname or origin.username or origin.password or origin.path or origin.query or origin.fragment:
            raise ValueError('expected HTTPS Ouicheur origin')
        updated['services']['api'].setdefault('environment', {}).update({
            'OUICHEUR_URL': ouicheur_origin,
            'OUICHEUR_REVISION': 'bbd143bc03201205a35b0a1009bf8476e110f16a',
        })
    return updated


def web_service(root, secret):
    template = Path(__file__).resolve().parent.parent / 'deploy/eris-web.override.yaml'
    service = json.loads(template.read_text())['services']['searxng']
    service['container_name'] = 'eris-web'
    service['environment']['SEARXNG_SECRET'] = secret
    service['volumes'] = [str(root / 'eris-web/settings.yml') + ':/etc/searxng/settings.yml:ro']
    return service


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


def healthy(extra=()):
    deadline = time.monotonic() + 180
    while time.monotonic() < deadline:
        try:
            items = [inspect(name) for name in [*CONTAINERS, *extra]]
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
    parser.add_argument('--hub-revision')
    parser.add_argument('--previous-hub-revision')
    parser.add_argument('--enable-web', action='store_true')
    parser.add_argument('--ouicheur-origin')
    args = parser.parse_args()
    assert all(re.fullmatch(r'[a-f0-9]{40}', value) for value in (args.revision, args.previous_revision))
    assert bool(args.hub_revision) == bool(args.previous_hub_revision)
    if args.hub_revision:
        assert all(re.fullmatch(r'[a-f0-9]{40}', value) for value in (args.hub_revision, args.previous_hub_revision))
    root = args.root.resolve(strict=True)
    with Client() as client:
        current = client.call('app.config', 'media-hub')
        before = {name: inspect(name) for name in CONTAINERS}
        previous_model = env_model(before['le-concierge-bot'])
        if args.hub_revision:
            assert before['le-hub']['Config']['Labels']['org.opencontainers.image.revision'] == args.previous_hub_revision
        if args.ouicheur_origin:
            installed = inspect('ix-ouicheur-ouicheur-1')
            assert installed['Config']['Labels']['org.opencontainers.image.revision'] == 'bbd143bc03201205a35b0a1009bf8476e110f16a'
            assert dict(v.split('=', 1) for v in installed['Config']['Env']).get('APP_ORIGIN') == args.ouicheur_origin
        web = web_service(root, secrets.token_hex(32)) if args.enable_web else None
        prepare(current, REPOSITORY+'@sha256:'+'0'*64, web=web, ouicheur_origin=args.ouicheur_origin)
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
        hub_image = None
        hub_pinned = None
        if args.hub_revision:
            tag = 'ghcr.io/hloiseau/le-hub:sha-' + args.hub_revision
            subprocess.run(['docker', 'pull', tag], capture_output=True, check=True)
            hub_image = inspect(tag)
            assert hub_image['Config']['Labels']['org.opencontainers.image.revision'] == args.hub_revision
            hub_pinned = next(value for value in hub_image['RepoDigests'] if value.startswith('ghcr.io/hloiseau/le-hub@sha256:'))
        updated = prepare(current, pinned, hub_pinned, web, args.ouicheur_origin)
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
        if web:
            directory = root / 'eris-web'
            assert not directory.is_symlink()
            if directory.exists():
                assert {entry.name for entry in directory.iterdir()} <= {'settings.yml'}
            directory.mkdir(mode=0o755, exist_ok=True)
            settings = directory / 'settings.yml'
            expected = (Path(__file__).resolve().parent.parent / 'deploy/searxng/settings.yml').read_text()
            if settings.exists():
                assert not settings.is_symlink() and settings.read_text() == expected
            else:
                with private_file(settings) as stream:
                    stream.write(expected)
            permissions(directory, '755')
            permissions(settings, '644')
        assert client.call('app.config', 'media-hub') == current, 'configuration changed during preparation'
        print(json.dumps({'phase': 'backups_ready', 'private_backup_name': bundle.name, 'image': pinned, 'image_id': image['Id']}), flush=True)
        try:
            client.call('app.update', 'media-hub', {'custom_compose_config': updated}, job=True)
            healthy(['eris-web'] if web else [])
            for name in CONTAINERS[:2]:
                actual = inspect(name)
                assert actual['Image'] == image['Id']
                assert actual['Config']['Labels']['org.opencontainers.image.revision'] == args.revision
            for name in CONTAINERS[2:]:
                expected_image = hub_image['Id'] if name == 'le-hub' and hub_image else before[name]['Image']
                assert inspect(name)['Image'] == expected_image
            for name, binary in zip(CONTAINERS[:2], ('api', 'bot')):
                subprocess.run(['docker', 'exec', name, '/app/' + binary, '--healthcheck'], capture_output=True, check=True)
            status = database_status(root)
            assert status['main_schema'] == previous_status['main_schema']
            assert env_model(inspect('le-concierge-bot')) == previous_model
            discord = discord_ready(inspect('le-concierge-bot'))
            if web:
                probe = "import json,urllib.request,urllib.parse;u='http://127.0.0.1:8080/search?'+urllib.parse.urlencode({'q':'Blender official website','format':'json'});d=json.load(urllib.request.urlopen(u,timeout=20));assert len(d.get('results',[]))>0"
                subprocess.run(['docker', 'exec', 'eris-web', '/usr/local/searxng/.venv/bin/python', '-c', probe], capture_output=True, check=True)
        except Exception:
            client.call('app.update', 'media-hub', {'custom_compose_config': current}, job=True)
            healthy()
            assert all(inspect(name)['Image'] == before[name]['Image'] for name in CONTAINERS)
            print(json.dumps({'phase': 'rolled_back', 'databases_restored': False}), flush=True)
            raise
        receipt = {'phase': 'deployed', 'revision': args.revision, 'image': pinned, 'image_id': image['Id'], 'services': ['api', 'bot'], 'model': env_model(inspect('le-concierge-bot')), **status, **discord}
        if hub_image:
            receipt.update({'hub_revision': args.hub_revision, 'hub_image': hub_pinned})
            receipt['services'].append('hub')
        if web:
            receipt['services'].append('searxng')
            receipt['web_search_verified'] = True
        receipt['ouicheur_enabled'] = bool(args.ouicheur_origin)
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
