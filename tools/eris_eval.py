"""Synthetic Eris evaluations only: never calls Discord, Plex or Seerr."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import statistics
import subprocess
import tempfile
import time
import urllib.error
import urllib.request

ROOT = Path(__file__).resolve().parents[1]


def go_constant(path, name):
    match = re.search(r"const " + re.escape(name) + r" = `([^`]*)`", path.read_text(encoding="utf-8"))
    if not match:
        raise ValueError(f"Cannot read {name}; update the evaluation prompt loader")
    return match.group(1)


def load_cases(path, split):
    cases = json.loads(path.read_text(encoding="utf-8"))
    ids = set()
    for case in cases:
        if case["id"] in ids or case["split"] not in ("dev", "holdout"):
            raise ValueError("Duplicate ID or invalid split")
        ids.add(case["id"])
        if not case["messages"] or case["messages"][-1]["role"] != "user":
            raise ValueError("Each case must end with a user message")
        for message in case["messages"]:
            if message["role"] not in ("user", "assistant") or not isinstance(message["content"], str):
                raise ValueError("Invalid synthetic message")
        if not case["review"] or "action" not in case["expect"]:
            raise ValueError("Missing acceptance criteria")
    return [case for case in cases if split == "all" or case["split"] == split]


def check(case, raw):
    """Mechanical checks, not a humor score or a replacement for Go ParsePlan."""
    try:
        plan = json.loads(raw)
    except (ValueError, TypeError):
        return ["invalid JSON"]
    if not isinstance(plan, dict):
        return ["not an object"]
    failures = [f"{key}: expected {value!r}" for key, value in case["expect"].items() if plan.get(key) != value]
    known = {"action", "query", "relation", "source", "kind", "genre", "maxMinutes", "unwatched", "seasons", "text"}
    if set(plan) - known:
        failures.append("unknown fields")
    if plan.get("unwatched") and not case["expect"].get("unwatched"):
        failures.append("unsolicited unwatched filter")
    text = plan.get("text", "")
    if not isinstance(text, str):
        return failures + ["invalid text"]
    if plan.get("action") == "chat" and not text.strip():
        failures.append("empty chat")
    if len(text) > 600:
        failures.append("over 600 characters")
    if "creator" not in case["id"] and any(name in text.casefold() for name in ("hugo", "vedal")):
        failures.append("unsolicited creator")
    if any(word.casefold() in text.casefold() for word in case.get("forbidden", [])):
        failures.append("forbidden wording")
    required = case.get("required_any", [])
    if required and not any(word.casefold() in text.casefold() for word in required):
        failures.append("missing clarification/identity wording")
    return failures


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


def http_reply(messages, model, timeout):
    base = os.environ.get("ERIS_EVAL_URL", "").rstrip("/")
    if not base or not model:
        raise ValueError("HTTP requires ERIS_EVAL_URL and --model")
    headers = {"Content-Type": "application/json"}
    key = os.environ.get("ERIS_EVAL_KEY", "")
    if os.environ.get("ERIS_EVAL_KEY_FILE"):
        key = Path(os.environ["ERIS_EVAL_KEY_FILE"]).read_text().strip()
    if key:
        headers["Authorization"] = "Bearer " + key
    payload = {"model": model, "messages": messages, "max_tokens": 160, "temperature": 0.45,
               "stream": False, "response_format": {"type": "json_object"}}
    req = urllib.request.Request(base + "/chat/completions", json.dumps(payload).encode(), headers)
    with urllib.request.build_opener(NoRedirect).open(req, timeout=timeout) as response:
        data = response.read(16385)
    if len(data) > 16384:
        raise ValueError("Oversized provider reply")
    result = json.loads(data)
    if len(result["choices"]) != 1 or result["choices"][0]["message"]["role"] != "assistant":
        raise ValueError("Invalid provider envelope")
    return result["choices"][0]["message"]["content"]


class CodexCallError(RuntimeError):
    """Only a fixed diagnostic category may leave the provider subprocess."""
    def __init__(self, returncode, diagnostics):
        lower = diagnostics.casefold()
        self.category = "unknown"
        for category, markers in (
            ("approval", ("unacceptable risk", "approval rejected", "action was rejected")),
            ("rate_limit", ("rate limit", "rate_limit", "too many requests", "429")),
            ("authentication", ("unauthorized", "authentication", "token expired", "401")),
            ("quota", ("quota", "usage limit")),
            ("connection", ("connection", "network", "stream disconnected")),
        ):
            if any(marker in lower for marker in markers):
                self.category = category
                break
        super().__init__(f"Codex failed (exit {returncode}; {self.category})")


def codex_reply(messages, model, timeout, image_paths=()):
    executable = shutil.which("codex")
    if not executable:
        raise ValueError("Codex CLI is not installed")
    # Only synthetic fixtures cross this boundary. No repo context or service keys.
    with tempfile.TemporaryDirectory(prefix="eris-eval-") as directory:
        temp = Path(directory)
        instructions = temp / "persona.txt"
        instructions.write_text(messages[0]["content"], encoding="utf-8")
        output = temp / "reply.txt"
        args = [executable, "exec", "--ignore-user-config", "--ephemeral", "--skip-git-repo-check",
                "--sandbox", "read-only", "--color", "never", "-C", directory,
                "-c", "approval_policy=\"never\"", "-c", "web_search=\"disabled\"",
                "-c", "project_doc_max_bytes=0", "-c", "model_reasoning_effort=\"low\"",
                "-c", "model_instructions_file=" + json.dumps(str(instructions)),
                "-o", str(output)]
        for feature in ("shell_tool", "unified_exec", "apps", "plugins", "multi_agent", "browser_use",
                        "computer_use", "image_generation", "view_image", "memories", "hooks"):
            args.extend(["--disable", feature])
        if model:
            args.extend(["--model", model])
        for path in image_paths:
            args.extend(["--image", str(path)])
        args.append("-")
        prompt = "Réponds au dernier message utilisateur de cette conversation synthétique. Un seul objet JSON, sans exécuter d'outil.\n" + json.dumps(messages[1:], ensure_ascii=False)
        # Do not forward service credentials inherited from a developer shell.
        env = {k: v for k, v in os.environ.items() if not k.startswith(("ERIS_", "DISCORD_", "PLEX_", "SEERR_", "OPENAI_API_KEY"))}
        result = subprocess.run(args, input=prompt, encoding="utf-8", errors="replace", capture_output=True,
                                timeout=timeout, cwd=directory, env=env)
        if result.returncode or not output.exists():
            # Provider stderr may include account context; keep it out of reports.
            raise CodexCallError(result.returncode, result.stderr + result.stdout)
        return output.read_text(encoding="utf-8").strip()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--backend", choices=("codex", "http"), required=True)
    parser.add_argument("--persona", choices=("baseline", "candidate"), default="candidate")
    parser.add_argument("--split", choices=("dev", "holdout", "all"), default="dev")
    parser.add_argument("--model")
    parser.add_argument("--limit", type=int)
    parser.add_argument("--timeout", type=float, default=90)
    parser.add_argument("--out", type=Path, required=True)
    args = parser.parse_args()
    if args.timeout <= 0 or (args.limit is not None and args.limit <= 0):
        parser.error("timeout and limit must be positive")
    baseline = go_constant(ROOT / "internal/eris/chat.go", "Persona")
    persona = baseline if args.persona == "baseline" else (ROOT / "evals/eris/persona.txt").read_text(encoding="utf-8").strip()
    system = persona + go_constant(ROOT / "internal/eris/tools.go", "ToolInstructions")
    if args.backend == "http" and len(system) > 2200:
        parser.error("The local Eris protocol allows 2200 prompt characters; use baseline or validate a shorter candidate first")
    cases_path = ROOT / "evals/eris/cases.json"
    cases = load_cases(cases_path, args.split)
    if args.limit:
        cases = cases[:args.limit]
    report = {"backend": args.backend, "model_requested": args.model or "CLI default (not pinned)",
              "persona": args.persona, "prompt_sha256": hashlib.sha256(system.encode()).hexdigest(),
              "cases_sha256": hashlib.sha256(cases_path.read_bytes()).hexdigest(),
              "started_utc": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
              "scope": "Raw synthetic model responses. No Discord/media tools; human style review required.", "results": []}
    args.out.parent.mkdir(parents=True, exist_ok=True)
    for case in cases:
        start = time.monotonic()
        row = {"id": case["id"], "messages": case["messages"], "review": case["review"], "human_score": None}
        try:
            messages = [{"role": "system", "content": system}] + case["messages"]
            raw = (codex_reply if args.backend == "codex" else http_reply)(messages, args.model, args.timeout)
            row.update(raw=raw, failures=check(case, raw))
        except (OSError, ValueError, RuntimeError, KeyError, IndexError, subprocess.TimeoutExpired) as error:
            row.update(raw=None, failures=["provider error: " + type(error).__name__])
        row["seconds"] = round(time.monotonic() - start, 3)
        report["results"].append(row)
        # Incremental evidence survives an interruption. Never store credentials or real conversations.
        args.out.write_text(json.dumps(report, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
        print(f"{case['id']}: {'FAIL' if row['failures'] else 'PASS'} ({row['seconds']}s)", flush=True)
    durations = [row["seconds"] for row in report["results"] if row["raw"] is not None]
    report["summary"] = {"mechanical_pass": sum(not r["failures"] for r in report["results"]), "total": len(cases),
                         "median_seconds": round(statistics.median(durations), 3) if durations else None,
                         "human_review": "pending"}
    args.out.write_text(json.dumps(report, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    print(json.dumps(report["summary"]))
    return 1 if any(row["failures"] for row in report["results"]) else 0


if __name__ == "__main__":
    raise SystemExit(main())
