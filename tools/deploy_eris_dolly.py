"""Render and optionally apply the Dolly 2 cutover on TrueNAS."""

import argparse
from contextlib import closing
import datetime
import json
import os
from pathlib import Path
import re
import sqlite3
import stat
import subprocess
import time

from deploy_eris_nas import (
    REPOSITORY, backup_database, database_status, discord_ready, inspect,
    prepare, private_file,
)

APP = "media-hub"
ROOT = Path("/mnt/Main/tank/concierge")
KEY_DIRECTORY = Path("/mnt/Main/tank/concierge-secrets/zro-eris")
KEY_TARGET = "/run/eris-zro"
SETTINGS = {
    "ERIS_PROFILE": "v2",
    "ERIS_PROTOCOL": "chat-completions",
    "ERIS_MODEL": "dolly2-security",
    "ERIS_API_URL": "https://zro.moonmath.ai/v1",
    "ERIS_API_KEY_FILE": KEY_TARGET + "/api-key",
    "ERIS_MEMORY_PATH": "/app/eris-data/character.db",
}


def mount_target(mount):
    return mount["target"] if isinstance(mount, dict) else mount.split(":")[1]


def apply_profile(config, image):
    updated = prepare(config, image)
    services = updated["services"]
    bot = services["bot"]
    env = bot["environment"]
    env.pop("ERIS_API_KEY", None)
    env.update(SETTINGS)
    mounts = bot["volumes"]
    if "/app/eris-data" not in {mount_target(m) for m in mounts}:
        raise ValueError("the existing Eris memory mount is required")
    stale = {"/run/eris-auth", "/run/secrets/eris_api_key", KEY_TARGET}
    bot["volumes"] = [m for m in mounts if mount_target(m) not in stale]
    bot["volumes"].append({
        "type": "bind", "source": str(KEY_DIRECTORY), "target": KEY_TARGET,
        "read_only": True, "bind": {"create_host_path": False},
    })
    bot["depends_on"].pop("eris-auth", None)
    if any("eris-auth" in value.get("depends_on", ()) for name, value in services.items() if name != "eris-auth"):
        raise ValueError("a second service requires eris-auth")
    services.pop("eris-auth", None)
    return updated


def validate_compose(config):
    subprocess.run(
        ["docker", "compose", "-f", "-", "config", "--quiet"],
        input=json.dumps(config), text=True, capture_output=True, check=True,
    )


def require_key():
    for path, mode in ((KEY_DIRECTORY, 0o710), (KEY_DIRECTORY / "api-key", 0o440)):
        info = path.lstat()
        expected_type = stat.S_IFDIR if path == KEY_DIRECTORY else stat.S_IFREG
        if (stat.S_IFMT(info.st_mode), stat.S_IMODE(info.st_mode), info.st_uid, info.st_gid) != (expected_type, mode, 0, 65532):
            raise PermissionError("invalid credential permissions")


def rollback_profile(current, previous_bot):
    # The running bot uses a temporary Compose override that cannot be replayed
    # from disk. Reconstruct that specific, file-based configuration.
    from copy import deepcopy

    env = dict(value.split("=", 1) for value in previous_bot["Config"]["Env"])
    if env.get("ERIS_API_KEY") or env["ERIS_API_KEY_FILE"] != "/run/secrets/eris_api_key":
        raise ValueError("unexpected previous bot configuration")
    fallback = deepcopy(current)
    bot = fallback["services"]["bot"]
    bot["environment"].pop("ERIS_API_KEY", None)
    for field in SETTINGS:
        bot["environment"][field] = env[field]
    bot["depends_on"].pop("eris-auth", None)
    mount = next(m for m in previous_bot["Mounts"] if m["Destination"] == env["ERIS_API_KEY_FILE"])
    if mount["Type"] != "bind" or mount["RW"]:
        raise ValueError("unexpected previous key mount")
    bot["volumes"] = [mount["Source"] + ":" + mount["Destination"] + ":ro"]
    return fallback


def check_running(image, unchanged):
    deadline = time.monotonic() + 100
    while time.monotonic() < deadline:
        current = {name: inspect(name) for name in ("le-concierge-api", "le-concierge-bot", *unchanged)}
        if all(unit["State"].get("Health", {}).get("Status") == "healthy" for unit in current.values()):
            break
        time.sleep(2)
    else:
        raise RuntimeError("container health deadline")
    assert all(current[name]["Image"] == previous for name, previous in unchanged.items())
    assert all(current[name]["Image"] == image["Id"] for name in ("le-concierge-api", "le-concierge-bot"))
    running = current["le-concierge-bot"]
    env = dict(pair.split("=", 1) for pair in running["Config"]["Env"])
    assert not env.get("ERIS_API_KEY") and all(env.get(k) == v for k, v in SETTINGS.items())
    mount = next(m for m in running["Mounts"] if m["Destination"] == KEY_TARGET)
    assert mount["Type"] == "bind" and not mount["RW"] and mount["Source"] == str(KEY_DIRECTORY)
    return discord_ready(running)


def main():
    from truenas_api_client import Client

    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--revision", required=True)
    parser.add_argument("--previous-api", required=True)
    parser.add_argument("--previous-bot", required=True)
    parser.add_argument("--apply", action="store_true")
    args = parser.parse_args()
    assert all(re.fullmatch(r"[a-f0-9]{40}", value) for value in (args.revision, args.previous_api, args.previous_bot))
    endpoint = "ws+unix:///var/run/middleware/middlewared-internal.sock"
    with Client(uri=endpoint) as client:
        current = client.call("app.config", APP)
        previous_bot = inspect("le-concierge-bot")
        assert previous_bot["Config"]["Labels"]["org.opencontainers.image.revision"] == args.previous_bot
        assert inspect("le-concierge-api")["Config"]["Labels"]["org.opencontainers.image.revision"] == args.previous_api
        validate_compose(apply_profile(current, REPOSITORY + "@sha256:" + "0" * 64))
        rollback = rollback_profile(current, previous_bot)
        validate_compose(rollback)
        before = database_status(ROOT)
        print(json.dumps({"phase": "preflight_ok", **before}), flush=True)
        if not args.apply:
            return

        require_key()
        tag = REPOSITORY + ":sha-" + args.revision
        subprocess.run(["docker", "pull", tag], capture_output=True, check=True)
        image = inspect(tag)
        assert image["Config"]["Labels"]["org.opencontainers.image.revision"] == args.revision
        pinned = next(value for value in image["RepoDigests"] if value.startswith(REPOSITORY + "@sha256:"))
        updated = apply_profile(current, pinned)
        validate_compose(updated)
        unchanged = {name: inspect(name)["Image"] for name in ("le-hub", "eris-runtime", "eris-web")}

        backup_root = ROOT / "ops-backups"
        info = backup_root.lstat()
        assert stat.S_ISDIR(info.st_mode) and info.st_uid == 0 and stat.S_IMODE(info.st_mode) == 0o700
        bundle = backup_root / ("before-eris-dolly-" + datetime.datetime.now(datetime.timezone.utc).strftime("%Y%m%dT%H%M%SZ"))
        bundle.mkdir(mode=0o700)
        for name, content in (("config-before.json", current), ("rollback.json", rollback)):
            with private_file(bundle / name) as stream:
                json.dump(content, stream)
                stream.flush()
                os.fsync(stream.fileno())
        backup_database(ROOT / "concierge.db", bundle / "concierge.db")
        backup_database(ROOT / "eris-character/character.db", bundle / "character.db")
        assert client.call("app.config", APP) == current
        print(json.dumps({"phase": "private_backup_ready", "bundle": bundle.name, "image": pinned}), flush=True)

        client.call("app.update", APP, {"custom_compose_config": updated}, job=True)
        ready = check_running(image, unchanged)
        status = database_status(ROOT)
        assert before["main_schema"] == status["main_schema"]
        with closing(sqlite3.connect((ROOT / "concierge.db").as_uri() + "?mode=ro", uri=True)) as db:
            assert db.execute("SELECT COUNT(*) FROM eris_seerr_attempts").fetchone()[0] >= 0
        app = client.call("app.get_instance", APP)
        assert app["state"] == "RUNNING"
        print(json.dumps({"phase": "deployed", "model": SETTINGS["ERIS_MODEL"], "revision": args.revision, "app": app["state"], **status, **ready}), flush=True)


if __name__ == "__main__":
    try:
        main()
    except Exception as error:
        print(json.dumps({"phase": "failed", "type": type(error).__name__}), flush=True)
        raise SystemExit(1)
