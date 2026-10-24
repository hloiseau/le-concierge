"""Exercise the edge policy against a real local Git remote, without publishing."""
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

SCRIPT = Path(__file__).with_name("image-edge.sh").resolve()
BASH = str(Path("C:/Program Files/Git/bin/bash.exe")) if os.name == "nt" else "bash"


class ImageEdgeTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.temp = tempfile.TemporaryDirectory(prefix="image-edge-")
        cls.root = Path(cls.temp.name).resolve()
        assert cls.root.parent == Path(tempfile.gettempdir()).resolve()
        cls.work = cls.root / "work"
        cls.env = dict(os.environ, GIT_CONFIG_GLOBAL=os.devnull,
                       GIT_CONFIG_NOSYSTEM="1", GIT_AUTHOR_NAME="Fixture",
                       GIT_AUTHOR_EMAIL="fixture@example.invalid",
                       GIT_COMMITTER_NAME="Fixture",
                       GIT_COMMITTER_EMAIL="fixture@example.invalid")
        subprocess.run(["git", "init", "-b", "main", str(cls.work)], env=cls.env,
                       check=True, capture_output=True)
        cls.git("commit", "--allow-empty", "--no-gpg-sign", "-m", "old")
        cls.old = cls.git("rev-parse", "HEAD")
        cls.git("commit", "--allow-empty", "--no-gpg-sign", "-m", "new")
        cls.new = cls.git("rev-parse", "HEAD")
        cls.git("clone", "--bare", str(cls.work), str(cls.root / "remote"))
        cls.git("remote", "add", "origin", str(cls.root / "remote"))

    @classmethod
    def tearDownClass(cls):
        cls.temp.cleanup()

    @classmethod
    def git(cls, *args):
        return subprocess.check_output(["git", *args], cwd=cls.work,
                                       env=cls.env, stderr=subprocess.PIPE,
                                       text=True).strip()

    def policy(self, sha, event="push", ref="refs/heads/main", cwd=None):
        env = dict(self.env, GITHUB_SHA=sha, GITHUB_EVENT_NAME=event, GITHUB_REF=ref)
        return subprocess.run([BASH, str(SCRIPT)], cwd=cwd or self.work,
                              env=env, text=True, capture_output=True)

    def test_current_main_publishes_edge(self):
        result = self.policy(self.new)
        self.assertEqual((result.returncode, result.stdout.strip()), (0, "edge=true"))

    def test_late_old_build_cannot_regress_edge(self):
        published = None
        for revision in [self.new, self.old]:
            result = self.policy(revision)
            self.assertEqual(result.returncode, 0, result.stderr)
            if result.stdout.strip() == "edge=true":
                published = revision
        self.assertEqual(published, self.new)

    def test_other_refs_never_publish_edge(self):
        for event, ref in [("pull_request", "refs/pull/1/merge"),
                           ("push", "refs/tags/v1.2.3"), ("push", "refs/heads/topic")]:
            with self.subTest(event=event, ref=ref):
                result = self.policy(self.new, event, ref)
                self.assertEqual((result.returncode, result.stdout.strip()), (0, "edge=false"))

    def test_missing_remote_fails_closed(self):
        result = self.policy(self.new, cwd=self.root)
        self.assertNotEqual(result.returncode, 0)
        self.assertNotIn("edge=true", result.stdout)


if __name__ == "__main__":
    unittest.main()
