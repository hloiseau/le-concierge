import copy
from pathlib import Path
import unittest

from eris_lab_nas import docker_command


class ErisLabNASTest(unittest.TestCase):
    def setUp(self):
        self.bot = {
            'Image': 'sha256:synthetic-image',
            'Config': {'Env': [
                'ERIS_MODEL=test-model', 'ERIS_PROFILE=v2', 'ERIS_PROTOCOL=responses',
                'ERIS_API_KEY_FILE=/run/model/token', 'DISCORD_BOT_TOKEN=PRIVATE_DISCORD',
                'BOT_API_SECRET=PRIVATE_MEDIA',
            ]},
            'Mounts': [
                {'Source': '/synthetic/auth', 'Destination': '/run/model'},
                {'Source': '/synthetic/character', 'Destination': '/app/character'},
                {'Source': '/synthetic/data', 'Destination': '/app/data'},
            ],
        }

    def test_only_model_authentication_is_mounted_and_no_service_secret_is_forwarded(self):
        before = copy.deepcopy(self.bot)
        command = docker_command(self.bot, Path('/synthetic/lab'), 'a' * 40)
        self.assertEqual(self.bot, before)
        volumes = [command[i + 1] for i, part in enumerate(command) if part == '-v']
        self.assertEqual(volumes, [str(Path('/synthetic/lab')) + ':/lab/eris-lab:ro', '/synthetic/auth/token:/lab/model-token:ro'])
        environments = [command[i + 1] for i, part in enumerate(command) if part == '-e']
        self.assertEqual(environments, ['ERIS_MODEL=test-model', 'ERIS_API_KEY_FILE=/lab/model-token', 'ERIS_LAB_REVISION=' + 'a' * 40])
        self.assertNotIn('PRIVATE', ' '.join(command))
        self.assertIn('--read-only', command)
        self.assertEqual(command[-1], self.bot['Image'])

    def test_invalid_revision_or_configuration_does_not_produce_a_command(self):
        with self.assertRaises(ValueError):
            docker_command(self.bot, Path('/synthetic/lab'), 'main')
        self.bot['Config']['Env'][1] = 'ERIS_PROFILE=legacy'
        with self.assertRaises(ValueError):
            docker_command(self.bot, Path('/synthetic/lab'), 'a' * 40)

    def test_missing_authentication_mount_is_rejected(self):
        self.bot['Mounts'] = self.bot['Mounts'][1:]
        with self.assertRaises(ValueError):
            docker_command(self.bot, Path('/synthetic/lab'), 'a' * 40)


if __name__ == '__main__':
    unittest.main()
