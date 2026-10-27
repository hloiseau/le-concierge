from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch
import urllib.error

from export_eris_discord import Discord, clean_message, export_channel, transcript, write_private


class FakeDiscord:
    def __init__(self, pages):
        self.pages = iter(pages)
        self.calls = []

    def get(self, path, params=None):
        self.calls.append((path, params))
        if path == "/users/@me":
            return {"id": "90000", "bot": True}
        if path == "/channels/30000":
            return {"id": "30000", "name": "test", "guild_id": "10000"}
        return next(self.pages)


def message(mid, **extra):
    return {"id": str(mid), "channel_id": "30000", "content": "message " + str(mid),
            "author": {"id": "20000", "global_name": "Alice"}, **extra}


class ExportTests(unittest.TestCase):
    def test_chronological_range_and_reference_preserved(self):
        api = FakeDiscord([[message(40003, message_reference={"message_id": "40002"},
                                   embeds=[{"url": "https://klipy.com/gifs/example"}]),
                            message(40002), message(40001)]])
        data = export_channel(api, "30000", limit=100, after="40001")
        self.assertEqual([m["id"] for m in data["messages"]], ["40002", "40003"])
        self.assertTrue(data["range"]["reached_boundary"])
        self.assertIn("Alice (20000)", transcript(data))
        self.assertIn("Répond à 40002", transcript(data))
        self.assertIn("https://klipy.com/gifs/example", transcript(data))

    def test_pagination_and_limit(self):
        api = FakeDiscord([[message(i) for i in range(40200, 40100, -1)], [message(40100)]])
        data = export_channel(api, "30000", limit=101)
        self.assertEqual(len(data["messages"]), 101)
        self.assertEqual(api.calls[-1][1], {"limit": 1, "before": "40101"})
        self.assertFalse(data["range"]["reached_boundary"])

    def test_refuse_identity_channel_or_pagination_changes(self):
        for page in [[message(40001), message(40001)], [message(40001, channel_id="30001")]]:
            with self.assertRaises(ValueError):
                export_channel(FakeDiscord([page]), "30000", 2)
        with self.assertRaises(ValueError):
            export_channel(FakeDiscord([]), "../channels/30000")
        api = FakeDiscord([])
        original = api.get
        api.get = lambda path, params=None: {"bot": False} if path == "/users/@me" else original(path, params)
        with self.assertRaisesRegex(ValueError, "bot token"):
            export_channel(api, "30000")

    def test_nested_reference_bounded(self):
        data = message(40003, referenced_message=message(40002, referenced_message=message(40001)))
        result = clean_message(data)
        self.assertNotIn("referenced_message", result["referenced_message"])

    def test_files_private_and_existing_export_not_overwritten(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "export.json"
            write_private(path, "private")
            self.assertEqual(path.stat().st_mode & 0o777, 0o600)
            with self.assertRaises(FileExistsError):
                write_private(path, "replacement")
            self.assertEqual(path.read_text(), "private")

    def test_requests_only_get_to_discord_and_errors_hide_token(self):
        api = Discord("synthetic-secret")
        class Response:
            def __enter__(self): return self
            def __exit__(self, *args): pass
            def read(self, size): return b'{"bot":true}'
        with patch.object(api._opener, "open", return_value=Response()) as call:
            self.assertTrue(api.get("/users/@me")["bot"])
            req = call.call_args.args[0]
            self.assertEqual(req.get_method(), "GET")
            self.assertEqual(req.full_url, "https://discord.com/api/v10/users/@me")
            self.assertEqual(req.headers["Authorization"], "Bot synthetic-secret")
        with self.assertRaises(ValueError):
            api.get("https://example.com")
        error = urllib.error.HTTPError("https://discord.com", 403, "synthetic-secret", {}, None)
        with patch.object(api._opener, "open", side_effect=error):
            with self.assertRaises(ValueError) as raised:
                api.get("/users/@me")
            self.assertNotIn("synthetic-secret", str(raised.exception))

    def test_rate_limit_bounded(self):
        from io import BytesIO
        api = Discord("synthetic-secret")
        error = urllib.error.HTTPError("https://discord.com", 429, "limited", {}, BytesIO(b'{"retry_after": 999}'))
        with patch.object(api._opener, "open", side_effect=error), patch("export_eris_discord.time.sleep") as sleep:
            with self.assertRaises(ValueError):
                api.get("/users/@me")
            sleep.assert_not_called()


if __name__ == "__main__":
    unittest.main()
