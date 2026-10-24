"""Run the actual Go conversation/memory loop against Luna with synthetic messages.

Local loopback bridge only; no Discord or media calls. Reuses the isolated Codex
runner, never copies its credentials. CLI startup is included in timings.
"""
import argparse
import hashlib
from http.server import BaseHTTPRequestHandler, HTTPServer
import json
import os
from pathlib import Path
import subprocess
import sys
import threading
import time

from eris_eval import ROOT, codex_reply


def main():
    # Windows consoles may not support the emoji emitted by the model.
    sys.stdout.reconfigure(encoding='utf-8', errors='replace')
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--out', type=Path, required=True)
    args = parser.parse_args()
    report = {'model': 'gpt-6-luna', 'scope': 'Synthetic sequential dialogue through Go Chat and SQLite; simulated delivery, no Discord/media calls.',
              'character_source_sha256': hashlib.sha256((ROOT / 'internal/eris/character.go').read_bytes()).hexdigest(),
              'human_review': 'pending', 'turns': []}
    args.out.parent.mkdir(parents=True, exist_ok=True)

    class Handler(BaseHTTPRequestHandler):
        def log_message(self, *args):
            pass

        def do_POST(self):
            start = time.monotonic()
            size = int(self.headers.get('Content-Length', '0'))
            if self.path != '/chat/completions' or not 0 < size < 100_000:
                self.send_error(400)
                return
            messages = json.loads(self.rfile.read(size))['messages']
            row = {'messages': messages}
            try:
                raw = codex_reply(messages, 'gpt-6-luna', 45)
                row['raw'] = raw
                payload = json.dumps({'choices': [{'message': {'role': 'assistant', 'content': raw}}]}).encode()
                self.send_response(200)
                self.end_headers()
                self.wfile.write(payload)
            except (OSError, ValueError, RuntimeError, subprocess.TimeoutExpired) as error:
                row['error'] = type(error).__name__
                self.send_error(502)
            finally:
                row['seconds'] = round(time.monotonic() - start, 3)
                report['turns'].append(row)
                args.out.write_text(json.dumps(report, ensure_ascii=False, indent=2)+'\n', encoding='utf-8')
                print(f'Turn {len(report["turns"])}: {row["seconds"]}s', flush=True)

    server = HTTPServer(('127.0.0.1', 0), Handler)
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    env = dict(os.environ, ERIS_DIALOGUE_TEST_URL=f'http://127.0.0.1:{server.server_port}')
    try:
        result = subprocess.run(['go', 'test', './internal/eris', '-run', '^TestLunaDialogue$', '-count=1', '-v'], cwd=ROOT, env=env,
                                encoding='utf-8', errors='replace', capture_output=True, timeout=480)
        report['go_exit_code'] = result.returncode
        report['go_output'] = result.stdout
        args.out.write_text(json.dumps(report, ensure_ascii=False, indent=2)+'\n', encoding='utf-8')
        print(result.stdout)
        return result.returncode
    finally:
        server.shutdown()
        server.server_close()
        thread.join()


if __name__ == '__main__':
    raise SystemExit(main())
