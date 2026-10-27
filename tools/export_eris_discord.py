"""Export one Discord channel with a bot token; GET requests only, no media downloads.

DISCORD_BOT_TOKEN=... python3 tools/export_eris_discord.py --channel ID --out /tmp/eris-discussion.json
Prefer --token-file pointing to your existing bot-token secret. Never pass a user token.
Exports are private data: keep them outside the repository.
"""
import argparse
from datetime import datetime, timezone
import json
import os
from pathlib import Path
import re
import time
import urllib.error
import urllib.parse
import urllib.request


def snowflake(value):
    if not isinstance(value, str) or not re.fullmatch(r"[0-9]{5,24}", value):
        raise ValueError("Expected a Discord ID (enable Developer Mode, Copy Channel ID)")
    return value


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


class Discord:
    def __init__(self, token):
        token = token.strip()
        if not token or any(c.isspace() for c in token):
            raise ValueError("Missing or invalid bot token")
        self._token = token
        self._opener = urllib.request.build_opener(NoRedirect)

    def get(self, path, params=None):
        if not re.fullmatch(r"/(users/@me|channels/[0-9]{5,24}(/messages)?)", path):
            raise ValueError("Unsupported read endpoint")
        url = "https://discord.com/api/v10" + path
        if params:
            url += "?" + urllib.parse.urlencode(params)
        for attempt in range(5):
            request = urllib.request.Request(url, headers={
                "Authorization": "Bot " + self._token,
                "User-Agent": "ErisDiscussionExport/1.0 (read-only)",
            }, method="GET")
            try:
                with self._opener.open(request, timeout=20) as response:
                    raw = response.read(4 * 1024 * 1024 + 1)
                if len(raw) > 4 * 1024 * 1024:
                    raise ValueError("Discord response exceeds the export budget")
                return json.loads(raw)
            except urllib.error.HTTPError as error:
                if error.code == 429 and attempt < 4:
                    try:
                        delay = float(json.loads(error.read(4096)).get("retry_after", 1))
                    except (ValueError, TypeError, AttributeError):
                        raise ValueError("Invalid Discord rate-limit response") from None
                    if not 0 <= delay <= 30:
                        raise ValueError("Discord rate limit is too long; retry later") from None
                    time.sleep(delay + 0.1)
                    continue
                if error.code in (401, 403):
                    raise ValueError("Discord refused the bot token or channel access; check View Channel and Read Message History") from None
                raise ValueError("Discord read failed (HTTP " + str(error.code) + ")") from None
            except urllib.error.URLError:
                raise ValueError("Unable to reach Discord") from None
        raise ValueError("Discord rate limit exceeded")


def clean_message(message, nested=False):
    # Preserve evidence and visual URLs without claiming to have seen the GIF.
    keys = ("id", "channel_id", "guild_id", "type", "timestamp", "edited_timestamp",
            "content", "author", "member", "mentions", "mention_roles", "attachments",
            "embeds", "sticker_items", "reactions", "message_reference", "poll")
    result = {key: message[key] for key in keys if key in message}
    reference = message.get("referenced_message")
    if not nested and isinstance(reference, dict):
        result["referenced_message"] = clean_message(reference, nested=True)
    return result


def export_channel(api, channel_id, limit=1000, before=None, after=None):
    snowflake(channel_id)
    if not 1 <= limit <= 20000:
        raise ValueError("limit must be between 1 and 20000")
    if before:
        snowflake(before)
    if after:
        snowflake(after)
    if before and after and int(before) <= int(after):
        raise ValueError("before must be later than after")
    identity = api.get("/users/@me")
    if not isinstance(identity, dict) or identity.get("bot") is not True:
        raise ValueError("Only a Discord bot token is supported")
    channel = api.get("/channels/" + channel_id)
    if not isinstance(channel, dict) or channel.get("id") != channel_id:
        raise ValueError("Unexpected Discord channel")
    records, seen = [], set()
    cursor, complete = before, False
    # Pagination is bounded even if a proxy returns duplicate or malformed pages.
    while len(records) < limit:
        params = {"limit": min(100, limit - len(records))}
        if cursor:
            params["before"] = cursor
        page = api.get("/channels/" + channel_id + "/messages", params)
        if not isinstance(page, list) or len(page) > params["limit"]:
            raise ValueError("Invalid Discord page")
        if not page:
            complete = True
            break
        for message in page:
            if not isinstance(message, dict):
                raise ValueError("Invalid Discord message")
            mid = snowflake(message.get("id"))
            if message.get("channel_id") != channel_id or (cursor and int(mid) >= int(cursor)) or mid in seen:
                raise ValueError("Discord pagination did not advance")
            seen.add(mid)
            if after and int(mid) <= int(after):
                complete = True
                continue
            records.append(clean_message(message))
        next_cursor = min(seen, key=int)
        if complete or len(page) < params["limit"]:
            complete = True
            break
        cursor = next_cursor
    records.sort(key=lambda m: int(m["id"]))
    return {
        "schema": "eris-discord-export/v1",
        "exported_at": datetime.now(timezone.utc).isoformat(),
        "channel": {key: channel[key] for key in ("id", "guild_id", "name", "type", "parent_id") if key in channel},
        "range": {"before": before, "after": after, "limit": limit, "reached_boundary": complete},
        "messages": records,
        "notes": "Only this channel/thread. Attachments and embeds are URLs, not downloaded or interpreted. Missing content may require the Message Content intent. No messages are sent and nothing is imported into Eris memory.",
    }


def transcript(data):
    lines = []
    for message in data["messages"]:
        author = message.get("author") or {}
        member = message.get("member") or {}
        name = member.get("nick") or author.get("global_name") or author.get("username") or "?"
        marker = " [BOT]" if author.get("bot") else ""
        lines.append(f'{message.get("timestamp", "")} | {name} ({author.get("id", "?")}){marker} | {message["id"]}')
        reference = message.get("message_reference") or {}
        if reference.get("message_id"):
            lines.append("  Répond à " + reference["message_id"])
        lines.append(message.get("content", ""))
        for attachment in message.get("attachments", []):
            lines.append("  Pièce jointe : " + attachment.get("url", ""))
        for embed in message.get("embeds", []):
            for part in (embed, embed.get("image") or {}, embed.get("thumbnail") or {}, embed.get("video") or {}):
                if part.get("url"):
                    lines.append("  Aperçu / média : " + part["url"])
        lines.append("")
    return "\n".join(lines)


def write_private(path, value):
    path = Path(path)
    path.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
    # Exclusive create avoids overwriting an earlier export or following a symlink.
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(fd, "w", encoding="utf-8") as output:
        output.write(value)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--channel", required=True, help="Channel/thread ID, not server ID")
    parser.add_argument("--out", type=Path, required=True, help="Private JSON output, outside git")
    parser.add_argument("--limit", type=int, default=1000)
    parser.add_argument("--before", help="Exclusive upper message ID")
    parser.add_argument("--after", help="Exclusive lower message ID")
    parser.add_argument("--token-file", type=Path, help="Existing file containing only the bot token")
    args = parser.parse_args()
    text_path = args.out.with_suffix(".txt")
    if args.out.suffix != ".json" or args.out.exists() or text_path.exists():
        parser.error("Choose a new .json output name; a matching .txt will also be written")
    try:
        token = args.token_file.read_text().strip() if args.token_file else os.environ.get("DISCORD_BOT_TOKEN", "").strip()
        data = export_channel(Discord(token), args.channel, args.limit, args.before, args.after)
        write_private(args.out, json.dumps(data, ensure_ascii=False, indent=2) + "\n")
        write_private(text_path, transcript(data))
    except (OSError, ValueError) as error:
        # Never print HTTP bodies, headers or the supplied token.
        print("Export failed: " + str(error))
        return 1
    print(f'Exported {len(data["messages"])} messages to {args.out} and {text_path}')
    if data["messages"] and not any(m.get("content") for m in data["messages"]):
        print("Warning: all message bodies are empty; check the Message Content intent.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
