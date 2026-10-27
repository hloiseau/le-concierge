import json
from pathlib import Path
import tempfile
import unittest

from eris_voice_eval import ROOT, check_reply, load_scenarios
from eris_eval import CodexCallError


class VoiceReplayTests(unittest.TestCase):
    def test_deliberate_silence_requires_exact_action(self):
        self.assertEqual(check_reply('{"action":"silent"}', {"expect_silent": True}), ('', []))
        for raw in ('{"action":"silent","text":"one more punchline"}', '{"action":"chat","text":"Merci."}'):
            self.assertTrue(check_reply(raw, {"expect_silent": True})[1])
    def test_provider_diagnostic_does_not_echo_provider_content(self):
        error = CodexCallError(1, 'rate limit; Authorization: SECRET-EXAMPLE')
        self.assertEqual(error.category, 'rate_limit')
        self.assertNotIn('SECRET', str(error))
        self.assertEqual(CodexCallError(1, 'arbitrary provider details').category, 'unknown')

    def test_chat_and_case_insensitive_regression(self):
        reply, failures = check_reply('{"action":"chat","text":"Tu as RAISON. Désolée."}',
                                     {"expect_chat": True, "forbidden": ["tu as raison", "désolée"]})
        self.assertEqual(len(failures), 2)
        self.assertIsNotNone(reply)

    def test_no_demand_for_violence_or_profanity(self):
        reply, failures = check_reply('{"action":"chat","text":"D’accord, j’arrête."}', {"expect_chat": True})
        self.assertEqual(failures, [])
        self.assertEqual(reply, 'D’accord, j’arrête.')

    def test_invalid_output_and_unexpected_tool(self):
        for raw in ('oops', '[]', 'null', '{"action":"chat"}', '{"action":"chat","text":42}'):
            with self.subTest(raw=raw):
                self.assertTrue(check_reply(raw, {"expect_chat": True})[1])
        self.assertIn('expected chat', check_reply('{"action":"search","text":"Vérification."}', {"expect_chat": True})[1])

    def test_fixtures_are_anonymous_synthetic_inputs(self):
        for filename in ('voice-dev.json', 'voice-reserve.json'):
            scenarios = load_scenarios(ROOT / 'evals/eris/dialogues' / filename)
            self.assertTrue(scenarios)
            self.assertTrue(all(scenario['expect_chat'] for scenario in scenarios))

    def test_bad_scenarios_rejected_before_provider_call(self):
        valid = {'id': 'one', 'inputs': ['Salut'], 'review': 'Natural French'}
        for cases in ([], [valid, valid], [{**valid, 'inputs': []}], [{**valid, 'inputs': ['x' * 501]}], [{**valid, 'forbidden': ['']}], [{**valid, 'review': ''}]):
            with self.subTest(cases=cases), tempfile.TemporaryDirectory() as directory:
                path = Path(directory) / 'cases.json'
                path.write_text(json.dumps(cases))
                with self.assertRaises(ValueError):
                    load_scenarios(path)


if __name__ == '__main__':
    unittest.main()
