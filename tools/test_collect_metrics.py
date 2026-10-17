import argparse
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

import collect_metrics as collector


class CollectorTest(unittest.TestCase):
    def test_gpu_units_startup_and_engine_selection(self):
        startup = {"period": {"duration": 0.01, "unit": "ms"}, "engines": {"Video": {"busy": 100, "unit": "%"}}}
        observed = {"period": {"duration": 1000, "unit": "ms"}, "engines": {"Video/0": {"busy": 20, "unit": "%"}, "Video/1": {"busy": 30, "unit": "%"}, "VideoEnhance/0": {"busy": 90, "unit": "%"}, "Render/3D/0": {"busy": 0, "unit": "%"}}, "frequency": {"actual": 550, "unit": "MHz"}, "power": {"GPU": 10, "unit": "unknown"}}
        result = collector.gpu_json("[" + json.dumps(startup) + "," + json.dumps(observed) + ",{")
        self.assertEqual(result["gpu_video_percent"]["value"], 30)
        self.assertEqual(result["gpu_render_percent"]["value"], 0)
        self.assertEqual(result["gpu_frequency_mhz"]["value"], 550)
        self.assertNotIn("gpu_power_watts", result)
        with self.assertRaises(ValueError):
            collector.gpu_json(json.dumps(startup))

    def test_counter_reset_and_units(self):
        self.assertEqual(collector.delta((10, 20), (14, 28), 2), (2, 4))
        self.assertIsNone(collector.delta((10,), (2,), 2))
        self.assertIsNone(collector.delta(None, (2,), 2))
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder)
            (root / "net").mkdir()
            (root / "stat").write_text("cpu 10 0 2 80 3 0 0 0 100 100\n")
            (root / "net/dev").write_text("header\nheader\neth0: 1024 0 0 0 0 0 0 0 2048 0 0 0 0 0 0 0\n")
            (root / "diskstats").write_text("8 0 sda 1 0 2 0 1 0 4 0 0 0 0\n")
            got = collector.counters(root)
            self.assertEqual(got["cpu"], [10, 0, 2, 80, 3, 0, 0, 0])
            self.assertEqual(got["network"]["eth0"], (1024, 2048))
            self.assertEqual(got["disks"]["sda"], (1024, 2048))

    def test_atomic_snapshot_budget_and_unknown_sensors(self):
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder)
            (root / "loadavg").write_text("0.5 0 0 1/1 1")
            (root / "meminfo").write_text("MemTotal: 1024 kB\nMemAvailable: 256 kB\n")
            args = argparse.Namespace(proc_root=root, sys_root=root, interface=[], disk=[], pool=[], cpu_temperature=None, gpu_temperature=None, gpu_device=None)
            with patch.object(collector.time, "sleep"):
                got = collector.sample(args)
            self.assertEqual(got["values"]["memory_used_bytes"]["value"], 768 * 1024)
            self.assertEqual(got["values"]["cpu_percent"]["status"], "error")
            self.assertEqual(got["values"]["cpu_temperature"]["status"], "unavailable")
            self.assertEqual(got["values"]["gpu_video_percent"]["value"], None)
            output = root / "snapshot.json"
            collector.write_snapshot(output, got)
            self.assertEqual(json.loads(output.read_text())["version"], 1)
            before = output.read_bytes()
            with self.assertRaises(ValueError):
                collector.write_snapshot(output, {"oversized": "x" * 32768})
            self.assertEqual(output.read_bytes(), before)
            self.assertFalse(list(root.glob(".metrics-*")))


if __name__ == "__main__":
    unittest.main()
