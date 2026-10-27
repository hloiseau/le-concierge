"""Run eris-lab on the NAS with isolated memory and existing model credentials.

Only reads the configured bot. No Discord/media credentials or production data
volumes are passed to the lab. JSON lines flow directly through stdin/stdout.
"""
import argparse
import json
from pathlib import Path, PurePosixPath
import re
import subprocess
import sys


def docker_command(bot, binary, revision):
    if not re.fullmatch(r'[a-f0-9]{40}', revision):
        raise ValueError('expected a complete source revision')
    env = dict(item.split('=', 1) for item in bot['Config']['Env'])
    if env.get('ERIS_PROFILE') != 'v2' or env.get('ERIS_PROTOCOL') != 'responses':
        raise ValueError('expected v2 Responses configuration')
    key = env['ERIS_API_KEY_FILE']
    mounts = [m for m in bot['Mounts']
              if key == m['Destination'] or key.startswith(m['Destination'].rstrip('/') + '/')]
    mount = max(mounts, key=lambda m: len(m['Destination']))
    token = PurePosixPath(mount['Source']) / PurePosixPath(key).relative_to(mount['Destination'])
    # shortcut: file bind retains one inode; restart the lab after token replacement.
    return [
        'docker', 'run', '--rm', '-i', '--name', 'eris-lab-79',
        '--read-only', '--network', 'host', '--user', '65532:65532',
        '--tmpfs', '/tmp:rw,nosuid,nodev,mode=1777',
        '--entrypoint', '/lab/eris-lab',
        '-v', str(binary) + ':/lab/eris-lab:ro',
        '-v', str(token) + ':/lab/model-token:ro',
        '-e', 'ERIS_MODEL=' + env['ERIS_MODEL'],
        '-e', 'ERIS_API_KEY_FILE=/lab/model-token',
        '-e', 'ERIS_LAB_REVISION=' + revision,
        bot['Image'],
    ]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--binary', type=Path, required=True)
    parser.add_argument('--revision', required=True)
    args = parser.parse_args()
    binary = args.binary.resolve(strict=True)
    if not binary.is_file():
        raise ValueError('expected a compiled laboratory binary')
    inspected = subprocess.run(['docker', 'inspect', 'le-concierge-bot'],
                               capture_output=True, check=True, text=True)
    bot = json.loads(inspected.stdout)[0]
    return subprocess.call(docker_command(bot, binary, args.revision))


if __name__ == '__main__':
    try:
        raise SystemExit(main())
    except (OSError, ValueError, KeyError, subprocess.SubprocessError):
        print('Unable to start isolated Eris lab; check configuration.', file=sys.stderr)
        raise SystemExit(1)
