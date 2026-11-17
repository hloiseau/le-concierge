import unittest

import deploy_eris_dolly as rollout


class DollyDeploymentTests(unittest.TestCase):
    def setUp(self):
        self.config = {"services": {
            "api": {"image": "previous-api"},
            "bot": {
                "image": "previous-bot",
                "environment": {"ERIS_API_KEY": "fixture", "ERIS_MODEL": "gpt-6.1-sol"},
                "depends_on": {"api": {"condition": "service_healthy"}, "eris-auth": {"condition": "service_healthy"}},
                "volumes": ["./existing-memory:/app/eris-data", "./old-tokens:/run/eris-auth:ro", "./another:/keep:ro"],
            },
            "eris-auth": {"image": "previous-auth"},
            "hub": {"image": "previous-hub"},
        }}
        self.image = rollout.REPOSITORY + "@sha256:" + "1" * 64

    def test_cutover_keeps_memory_and_disables_oauth_dependency(self):
        updated = rollout.apply_profile(self.config, self.image)
        services = updated["services"]
        bot = services["bot"]
        self.assertEqual(services["api"]["image"], self.image)
        self.assertEqual(bot["image"], self.image)
        self.assertEqual(bot["environment"], rollout.SETTINGS)
        self.assertEqual(list(bot["depends_on"]), ["api"])
        self.assertNotIn("eris-auth", services)
        self.assertEqual(bot["volumes"][:2], ["./existing-memory:/app/eris-data", "./another:/keep:ro"])
        self.assertEqual(bot["volumes"][2]["target"], "/run/eris-zro")
        self.assertTrue(bot["volumes"][2]["read_only"])
        self.assertFalse(bot["volumes"][2]["bind"]["create_host_path"])
        self.assertEqual(services["hub"], self.config["services"]["hub"])
        self.assertIn("ERIS_API_KEY", self.config["services"]["bot"]["environment"])

    def test_second_auth_consumer_blocks_removal(self):
        self.config["services"]["hub"]["depends_on"] = {"eris-auth": {"condition": "service_started"}}
        with self.assertRaises(ValueError):
            rollout.apply_profile(self.config, self.image)
