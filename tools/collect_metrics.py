#!/usr/bin/env python3
"""Opt-in read-only Linux collector. Writes one atomic, bounded snapshot file.

No network listener, Docker socket, package installation or permission changes.
Run separately from the Hub/API; see docs/monitoring.md for host permissions.
"""
import argparse
import datetime
import json
import math
import os
from pathlib import Path
import re
import signal
import subprocess
import tempfile
import time


UNITS = {
    "cpu_percent": "percent", "cpu_temperature": "celsius", "load_1m": "load",
    "memory_used_bytes": "bytes", "memory_total_bytes": "bytes",
    "gpu_video_percent": "percent", "gpu_render_percent": "percent",
    "gpu_temperature": "celsius", "gpu_power_watts": "watts", "gpu_frequency_mhz": "mhz",
    "disk_read_bytes_per_second": "bytes_per_second", "disk_write_bytes_per_second": "bytes_per_second",
    "network_receive_bytes_per_second": "bytes_per_second", "network_send_bytes_per_second": "bytes_per_second",
    "storage_used_bytes": "bytes", "storage_total_bytes": "bytes",
}


def metric(key, value=None, status="unavailable"):
    if value is not None:
        if not isinstance(value, (int, float)) or isinstance(value, bool) or not math.isfinite(value):
            raise ValueError("invalid metric")
        if value < 0 and UNITS[key] != "celsius" or value > 1e15:
            raise ValueError("out of range metric")
        if UNITS[key] == "percent" and value > 100:
            raise ValueError("invalid percentage")
        if UNITS[key] == "celsius" and not -273.15 <= value <= 1000:
            raise ValueError("invalid temperature")
        status = "available"
    return {"value": value, "status": status, "unit": UNITS[key]}


def text(path):
    with open(path, encoding="utf-8") as source:
        value = source.read(1_048_577)
    if len(value) > 1_048_576:
        raise ValueError("oversized source")
    return value


def counters(proc):
    cpu = None
    try:
        cpu = [int(v) for v in text(proc / "stat").splitlines()[0].split()[1:9]]
        if len(cpu) != 8:
            raise ValueError("incomplete CPU counters")
    except (OSError, ValueError):
        cpu = None
    network, disks = {}, {}
    try:
        for line in text(proc / "net/dev").splitlines()[2:]:
            name, data = line.split(":", 1)
            fields = [int(v) for v in data.split()]
            network[name.strip()] = (fields[0], fields[8])
    except (OSError, ValueError, IndexError):
        network = None
    try:
        for line in text(proc / "diskstats").splitlines():
            f = line.split()
            # Linux diskstats sectors are always 512 bytes, independently of
            # the device logical/physical block size. Explicit whole disks only.
            disks[f[2]] = (int(f[5]) * 512, int(f[9]) * 512)
    except (OSError, ValueError, IndexError):
        disks = None
    return {"cpu": cpu, "network": network, "disks": disks, "at": time.monotonic()}


def delta(before, after, seconds):
    if seconds <= 0 or before is None or after is None or any(b < a for a, b in zip(before, after)):
        return None  # reboot, counter reset, missing device, or invalid interval
    return tuple((b - a) / seconds for a, b in zip(before, after))


def gpu_json(raw):
    """Only complete samples >= 500 ms; startup near-zero samples are rejected."""
    decoder, samples, offset = json.JSONDecoder(), [], 0
    while offset < len(raw):
        start = raw.find("{", offset)
        if start < 0:
            break
        try:
            item, length = decoder.raw_decode(raw[start:])
        except ValueError:
            break
        offset = start + length
        period = item.get("period", {})
        if period.get("unit") == "ms" and isinstance(period.get("duration"), (int, float)) and 500 <= period["duration"] <= 10000:
            samples.append(item)
    if not samples:
        raise ValueError("no complete GPU observation")
    item = samples[-1]
    values = {}
    for prefix, key in [("Video", "gpu_video_percent"), ("Render/3D", "gpu_render_percent")]:
        engines = [v.get("busy") for name, v in item.get("engines", {}).items()
                   if (name == prefix or name.startswith(prefix + "/")) and v.get("unit") == "%"]
        if engines and all(isinstance(v, (int, float)) and math.isfinite(v) and 0 <= v <= 100 for v in engines):
            values[key] = metric(key, max(engines))  # busiest matching engine, not summed load
    for group, field, unit, key in [("frequency", "actual", "MHz", "gpu_frequency_mhz"), ("power", "GPU", "W", "gpu_power_watts")]:
        block = item.get(group, {})
        if block.get("unit") == unit and isinstance(block.get(field), (int, float)):
            values[key] = metric(key, block[field])
    return values


def read_gpu(device, sysroot):
    vendor = text(sysroot / "class/drm" / Path(device).name / "device/vendor").strip()
    if vendor != "0x8086":
        raise ValueError("selected GPU is not Intel")
    # Capture to a temporary file to avoid an unbounded pipe buffer. The process
    # only exists for this sampling window and receives no stdin or shell input.
    with tempfile.TemporaryFile() as output:
        process = subprocess.Popen(["intel_gpu_top", "-J", "-s", "1000", "-d", "drm:" + device, "-o", "-"],
                                   stdin=subprocess.DEVNULL, stdout=output, stderr=subprocess.DEVNULL)
        try:
            process.wait(timeout=3.2)
        except subprocess.TimeoutExpired:
            process.send_signal(signal.SIGINT)
            try:
                process.wait(timeout=1)
            except subprocess.TimeoutExpired:
                process.kill()
                process.wait()
        output.seek(0)
        raw = output.read(1_048_577)
    if len(raw) > 1_048_576:
        raise ValueError("oversized GPU observation")
    return gpu_json(raw.decode("utf-8"))


def sample(args):
    values = {key: metric(key) for key in UNITS}
    details = []
    before = counters(args.proc_root)
    time.sleep(1)
    after = counters(args.proc_root)
    elapsed = after["at"] - before["at"]
    cpu = delta(before["cpu"], after["cpu"], elapsed)
    if cpu and sum(cpu) > 0:
        values["cpu_percent"] = metric("cpu_percent", 100 * (sum(cpu) - cpu[3] - cpu[4]) / sum(cpu))
    elif before["cpu"] is None or after["cpu"] is None:
        values["cpu_percent"] = metric("cpu_percent", status="error")
    try:
        values["load_1m"] = metric("load_1m", float(text(args.proc_root / "loadavg").split()[0]))
    except (OSError, ValueError, IndexError):
        values["load_1m"] = metric("load_1m", status="error")
    try:
        memory = {line.split(":")[0]: int(line.split()[1]) * 1024 for line in text(args.proc_root / "meminfo").splitlines() if line.startswith(("MemTotal:", "MemAvailable:"))}
        total, available = memory["MemTotal"], memory["MemAvailable"]
        if total <= 0 or available > total:
            raise ValueError("invalid memory observation")
        values["memory_total_bytes"] = metric("memory_total_bytes", total)
        values["memory_used_bytes"] = metric("memory_used_bytes", total - available)
    except (OSError, ValueError, KeyError, IndexError):
        for key in ("memory_total_bytes", "memory_used_bytes"):
            values[key] = metric(key, status="error")
    for path, key in [(args.cpu_temperature, "cpu_temperature"), (args.gpu_temperature, "gpu_temperature")]:
        if path:
            try:
                values[key] = metric(key, float(text(path).strip()) / 1000)
            except (OSError, ValueError):
                values[key] = metric(key, status="error")
    for section, names, kind, keys in [("network", args.interface, "interface", ("network_receive_bytes_per_second", "network_send_bytes_per_second")), ("disks", args.disk, "disk", ("disk_read_bytes_per_second", "disk_write_bytes_per_second"))]:
        sums = [0.0, 0.0]
        complete = bool(names)
        failed = before[section] is None or after[section] is None
        for name in names:
            rates = None if failed else delta(before[section].get(name), after[section].get(name), elapsed)
            complete &= rates is not None
            per_device = {key: metric(key, rates[i] if rates is not None else None, "error" if failed else "unavailable") for i, key in enumerate(keys)}
            details.append({"kind": kind, "name": name, "status": "error" if failed else "available" if rates is not None else "unavailable", "values": per_device})
            if rates:
                sums = [sums[i] + rates[i] for i in range(2)]
        for i, key in enumerate(keys):
            values[key] = metric(key, sums[i] if complete else None, "error" if failed and names else "unavailable")
    if args.pool:
        try:
            result = subprocess.run(["zpool", "list", "-H", "-p", "-o", "name,size,alloc,health", *args.pool], check=True, capture_output=True, text=True, timeout=3)
            seen, total, used = set(), 0, 0
            for line in result.stdout.splitlines():
                name, size, allocated, health = line.split()
                if name not in args.pool or name in seen or health not in ("ONLINE", "DEGRADED", "FAULTED", "OFFLINE", "UNAVAIL", "REMOVED", "SUSPENDED"):
                    raise ValueError("unexpected pool result")
                seen.add(name)
                capacity, allocation = int(size), int(allocated)
                if allocation > capacity or min(capacity, allocation) < 0:
                    raise ValueError("invalid pool capacity")
                total += capacity
                used += allocation
                details.append({"kind": "pool", "name": name + " (" + health + ")", "status": "available" if health == "ONLINE" else "error", "values": {"storage_total_bytes": metric("storage_total_bytes", capacity), "storage_used_bytes": metric("storage_used_bytes", allocation)}})
            if seen != set(args.pool):
                raise ValueError("incomplete pool result")
            values["storage_total_bytes"] = metric("storage_total_bytes", total)
            values["storage_used_bytes"] = metric("storage_used_bytes", used)
        except (OSError, ValueError, subprocess.SubprocessError):
            values["storage_total_bytes"] = metric("storage_total_bytes", status="error")
            values["storage_used_bytes"] = metric("storage_used_bytes", status="error")
    if args.gpu_device:
        try:
            gpu = read_gpu(args.gpu_device, args.sys_root)
            values.update(gpu)
            details.append({"kind": "gpu", "name": Path(args.gpu_device).name, "status": "available" if gpu else "unavailable", "values": gpu})
        except (OSError, ValueError, subprocess.SubprocessError):
            for key in ("gpu_video_percent", "gpu_render_percent", "gpu_power_watts", "gpu_frequency_mhz"):
                values[key] = metric(key, status="error")
    try:
        version = text(Path("/etc/version")).strip()[:100]
    except OSError:
        version = ""
    return {"version": 1, "collectedAt": datetime.datetime.now(datetime.timezone.utc).isoformat(), "hostVersion": version, "values": values, "details": details}


def write_snapshot(output, data):
    encoded = json.dumps(data, allow_nan=False, separators=(",", ":")).encode()
    if len(encoded) > 32768:
        raise ValueError("snapshot budget exceeded")
    fd, pending = tempfile.mkstemp(prefix=".metrics-", dir=output.parent)
    try:
        os.chmod(pending, 0o640)
        with os.fdopen(fd, "wb") as target:
            target.write(encoded)
            target.flush()
            os.fsync(target.fileno())
        os.replace(pending, output)
    finally:
        if os.path.exists(pending):
            os.unlink(pending)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--proc-root", type=Path, default=Path("/proc"))
    parser.add_argument("--sys-root", type=Path, default=Path("/sys"))
    parser.add_argument("--interface", action="append", default=[])
    parser.add_argument("--disk", action="append", default=[])
    parser.add_argument("--pool", action="append", default=[])
    parser.add_argument("--cpu-temperature", type=Path)
    parser.add_argument("--gpu-temperature", type=Path)
    parser.add_argument("--gpu-device")
    parser.add_argument("--interval", type=int, default=60)
    parser.add_argument("--once", action="store_true")
    args = parser.parse_args()
    if not args.output.is_absolute() or not args.output.parent.is_dir() or not 60 <= args.interval <= 3600:
        parser.error("existing absolute output directory and interval 60..3600 required")
    if sum(map(len, (args.interface, args.disk, args.pool))) > 31:
        parser.error("at most 31 selected interfaces, disks and pools")
    for names in (args.interface, args.disk, args.pool):
        if len(set(names)) != len(names) or any(not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9_.:-]{0,79}", n) for n in names):
            parser.error("invalid or duplicate device/pool selection")
    if args.gpu_device and not re.fullmatch(r"/dev/dri/(card[0-9]+|renderD[0-9]+)", args.gpu_device):
        parser.error("select one explicit DRM node")
    while True:
        started = time.monotonic()
        write_snapshot(args.output, sample(args))
        if args.once:
            return
        time.sleep(max(0, args.interval - (time.monotonic() - started)))


if __name__ == "__main__":
    main()
