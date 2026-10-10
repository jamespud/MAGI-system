#!/usr/bin/env python3
"""Exercise cutover ordering and failure containment without deployment access."""
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parent.parent


class CutoverScript(unittest.TestCase):
    def exercise(self, failed_stage=""):
        with tempfile.TemporaryDirectory(prefix="magi-t6-cutover-") as directory:
            root = Path(directory)
            scripts = root / "scripts"
            scripts.mkdir()
            shutil.copy(ROOT / "scripts/decision-cutover.sh", scripts)
            log = root / "actions"
            executable = """#!/usr/bin/env bash
set -eu
echo "$ACTION $*" >> "$ACTION_LOG"
if [[ "$ACTION" == admin ]]; then
  [[ "$1" != "$FAIL_STAGE" ]] || exit 1
  case "$1" in running-count) echo 0;; block) echo 7;; esac
fi
"""
            for action in ("admin", "docker", "freeze", "stop-all", "open", "backup"):
                path = root / action
                # Keep ACTION_LOG intact: only replace the standalone ACTION variable.
                path.write_text(executable.replace('"$ACTION $*"', f'"{action} $*"')
                                .replace('"$ACTION" == admin', f'"{action}" == admin'))
                path.chmod(0o700)
            shutil.copy(root / "backup", scripts / "backup.sh")
            environment = dict(os.environ, ACTION_LOG=str(log), FAIL_STAGE=failed_stage,
                               PATH=str(root) + ":" + os.environ["PATH"],
                               MAGI_DECISION_ADMIN=str(root / "admin"),
                               MAGI_ADMIN_DSN="private-test-admin",
                               MAGI_WRITER_DSN="private-test-writer",
                               MAGI_WRITER_DB_USER="new", MAGI_WRITER_DB_PASSWORD="private-test",
                               MAGI_WRITER_IMAGE="repo@sha256:" + "a" * 64,
                               MAGI_NEW_WRITER_ACCOUNT="new@%",
                               MAGI_LEGACY_WRITER_ACCOUNT="old@%",
                               MAGI_CUTOVER_FREEZE=str(root / "freeze"),
                               MAGI_CUTOVER_STOP_ALL=str(root / "stop-all"),
                               MAGI_CUTOVER_OPEN=str(root / "open"))
            result = subprocess.run(["bash", str(scripts / "decision-cutover.sh")],
                                    env=environment, text=True, capture_output=True)
            return result, log.read_text().splitlines()

    def test_success_order(self):
        result, actions = self.exercise()
        self.assertEqual(result.returncode, 0, result.stderr)
        key = [line for line in actions if not line.startswith("docker ")]
        self.assertEqual(key, [
            "freeze ", "admin status", "admin block", "admin running-count",
            "stop-all ", f"backup --output {self._backup_path(actions)}",
            "admin install-contract", "admin block",
            "admin configure --writer new@% --legacy old@%", "admin exclude-legacy",
            "admin verify-cutover", "admin verify-identity", "admin block",
            "admin enable --epoch 7", "admin verify-writer", "open ",
        ])

    @staticmethod
    def _backup_path(actions):
        return next(line.split("--output ", 1)[1] for line in actions if line.startswith("backup "))

    def test_failures_keep_writer_stopped(self):
        for stage in ("running-count", "install-contract", "configure", "exclude-legacy",
                      "verify-cutover", "verify-identity", "enable", "verify-writer"):
            with self.subTest(stage=stage):
                result, actions = self.exercise(stage)
                self.assertNotEqual(result.returncode, 0)
                self.assertFalse(any(line.startswith("open ") for line in actions))
                self.assertFalse(any(" up " in line for line in actions if line.startswith("docker ")))
                self.assertTrue(any(line.startswith("stop-all ") for line in actions))
                self.assertEqual(actions[-1], "admin block")

    def test_dry_run_needs_no_credentials_or_hooks(self):
        result = subprocess.run(["bash", str(ROOT / "scripts/decision-cutover.sh"), "--dry-run"],
                                env={"PATH": os.environ["PATH"]}, capture_output=True, text=True)
        self.assertEqual(result.returncode, 0)
        self.assertIn("Any failure keeps ingress frozen", result.stdout)


if __name__ == "__main__":
    unittest.main()
