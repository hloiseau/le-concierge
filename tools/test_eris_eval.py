import json
import os
import threading
import unittest
from http.server import BaseHTTPRequestHandler, HTTPServer
from unittest.mock import patch
from urllib.error import HTTPError
from eris_eval import ROOT, check, go_constant, http_reply, load_cases


class EvalTests(unittest.TestCase):
    def test_cases_and_actual_prompts(self):
        cases = load_cases(ROOT / "evals/eris/cases.json", "all")
        self.assertEqual(len(cases), 24)
        self.assertEqual(len(load_cases(ROOT / "evals/eris/cases.json", "holdout")), 12)
        self.assertIn("Tu es Eris", go_constant(ROOT / "internal/eris/chat.go", "Persona"))
        self.assertIn('"action":"prepare"', go_constant(ROOT / "internal/eris/tools.go", "ToolInstructions"))

    def test_catches_false_success_and_unsolicited_filters(self):
        case = {"id": "clarify", "expect": {"action": "chat"}, "required_any": ["titre"], "forbidden": ["ne peux pas"]}
        self.assertEqual(check(case, '{"action":"chat","text":"Quel titre ?"}'), [])
        for raw in ('[]', '{}', 'not JSON', '{"action":"chat","text":42}',
                    '{"action":"chat","text":"Je ne peux pas demander ce titre."}',
                    '{"action":"chat","text":"Quel titre ?","unwatched":true}',
                    '{"action":"chat","text":"Quel titre ?","execute":"delete"}'):
            self.assertTrue(check(case, raw), raw)

    def test_exact_season_and_reference(self):
        case = {"id": "season", "expect": {"action": "prepare", "query": "The Bear", "seasons": [3]}}
        self.assertFalse(check(case, json.dumps(case["expect"])))
        self.assertTrue(check(case, '{"action":"prepare","query":"The Bear","seasons":[1,2,3]}'))

    def test_http_contract_and_no_redirect(self):
        requests = []

        class Handler(BaseHTTPRequestHandler):
            def log_message(self, *args):
                pass

            def do_POST(self):
                requests.append((self.path, self.headers.get("Authorization"), json.loads(self.rfile.read(int(self.headers["Content-Length"])))))
                if len(requests) == 2:
                    self.send_response(307)
                    self.send_header("Location", "/private-redirect")
                    self.end_headers()
                    return
                self.send_response(200)
                self.end_headers()
                self.wfile.write(b'{"choices":[{"message":{"role":"assistant","content":"{\\"action\\":\\"chat\\",\\"text\\":\\"Salut.\\"}"}}]}')

        server = HTTPServer(("127.0.0.1", 0), Handler)
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        try:
            with patch.dict(os.environ, {"ERIS_EVAL_URL": f"http://127.0.0.1:{server.server_port}/v1", "ERIS_EVAL_KEY": "fixture", "ERIS_EVAL_KEY_FILE": ""}):
                self.assertEqual(json.loads(http_reply([], "fixture-model", 2))["text"], "Salut.")
                with self.assertRaises(HTTPError):
                    http_reply([], "fixture-model", 2)
            self.assertEqual(len(requests), 2)
            path, auth, payload = requests[0]
            self.assertEqual((path, auth), ("/v1/chat/completions", "Bearer fixture"))
            self.assertEqual(payload["response_format"], {"type": "json_object"})
            self.assertEqual((payload["temperature"], payload["max_tokens"]), (0.45, 160))
        finally:
            server.shutdown()
            server.server_close()
            thread.join()


if __name__ == "__main__":
    unittest.main()
