import copy
from contextlib import closing
from pathlib import Path
import sqlite3
import tempfile
import unittest

from deploy_eris_nas import backup_database, prepare


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
