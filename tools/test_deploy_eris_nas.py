import copy
from contextlib import closing
from pathlib import Path, PurePosixPath
import sqlite3
import tempfile
import unittest

from deploy_eris_nas import backup_database, prepare, web_service


class DeployErisNASTest(unittest.TestCase):
    def test_only_api_and_bot_images_change(self):
        config = {'services': {name: {'image': 'previous-' + name, 'environment': {'PRIVATE': 'keep'}, 'volumes': ['keep']} for name in ('api', 'bot', 'hub', 'eris', 'eris-auth')}, 'networks': {'keep': {}}}
        original = copy.deepcopy(config)
        pinned = 'ghcr.io/hloiseau/le-concierge@sha256:' + 'a' * 64
        updated = prepare(config, pinned)
        self.assertEqual(config, original)
        for name in ('api', 'bot'):
            self.assertEqual(updated['services'][name]['image'], pinned)
            updated['services'][name]['image'] = config['services'][name]['image']
        self.assertEqual(updated, original)

    def test_mutable_or_unrelated_image_is_rejected(self):
        for image in ('ghcr.io/hloiseau/le-concierge:edge', 'ghcr.io/example/other@sha256:' + 'a' * 64):
            with self.assertRaises(ValueError):
                prepare({'services': {}}, image)

    def test_explicit_tools_preserve_other_services_and_private_configuration(self):
        config={'services':{name:{'image':'old-'+name,'environment':{'PRIVATE':'keep'},'volumes':['keep']} for name in ('api','bot','hub','eris','eris-auth')}}
        original=copy.deepcopy(config)
        web=web_service(PurePosixPath('/mnt/task'), 'synthetic-secret')
        updated=prepare(config,'ghcr.io/hloiseau/le-concierge@sha256:'+'a'*64,'ghcr.io/hloiseau/le-hub@sha256:'+'b'*64,web,'https://gifts.example')
        self.assertEqual(config,original)
        self.assertEqual(updated['services']['eris'],original['services']['eris'])
        self.assertEqual(updated['services']['eris-auth'],original['services']['eris-auth'])
        self.assertEqual(updated['services']['bot']['environment'],original['services']['bot']['environment'])
        self.assertEqual(updated['services']['api']['environment']['PRIVATE'],'keep')
        self.assertEqual(updated['services']['api']['environment']['OUICHEUR_URL'],'https://gifts.example')
        self.assertEqual(updated['services']['api']['environment']['ERIS_WEB_URL'],'http://searxng:8080')
        service=updated['services']['searxng']
        self.assertTrue(service['read_only'])
        self.assertNotIn('ports',service)
        self.assertEqual(service['environment']['SEARXNG_SECRET'],'synthetic-secret')
        self.assertEqual(service['volumes'],['/mnt/task/eris-web/settings.yml:/etc/searxng/settings.yml:ro'])
        with self.assertRaises(ValueError):prepare(updated,'ghcr.io/hloiseau/le-concierge@sha256:'+'a'*64,web=web)
        with self.assertRaises(ValueError):prepare(config,'ghcr.io/hloiseau/le-concierge@sha256:'+'a'*64,hub_image='ghcr.io/hloiseau/le-hub:edge')
        for origin in ['http://gifts.example','https://user:secret@gifts.example','https://gifts.example/path','https://gifts.example?q=x']:
            with self.assertRaises(ValueError):prepare(config,'ghcr.io/hloiseau/le-concierge@sha256:'+'a'*64,ouicheur_origin=origin)

    def test_sqlite_backup_includes_committed_wal_and_refuses_overwrite(self):
        with tempfile.TemporaryDirectory() as directory:
            source = Path(directory).resolve() / 'source.db'
            destination = source.with_name('backup.db')
            with closing(sqlite3.connect(source)) as db:
                db.execute('PRAGMA journal_mode=WAL')
                db.execute('CREATE TABLE sample(value TEXT)')
                db.execute("INSERT INTO sample VALUES('synthetic')")
                db.commit()
                backup_database(source, destination)
                with closing(sqlite3.connect(destination)) as copied:
                    self.assertEqual(copied.execute('SELECT value FROM sample').fetchone()[0], 'synthetic')
                with self.assertRaises(FileExistsError):
                    backup_database(source, destination)


if __name__ == '__main__':
    unittest.main()
