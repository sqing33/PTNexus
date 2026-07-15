import importlib.util
import json
import subprocess
import tempfile
import unittest
from pathlib import Path
from unittest import mock


SCRIPT_PATH = Path(__file__).with_name("publish-gitee-release.py")
SPEC = importlib.util.spec_from_file_location("publish_gitee_release", SCRIPT_PATH)
if SPEC is None or SPEC.loader is None:
    raise RuntimeError(f"cannot load {SCRIPT_PATH}")
MODULE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(MODULE)


class CollectAssetPathsTest(unittest.TestCase):
    def test_manifest_is_required_and_uploaded_first(self) -> None:
        with tempfile.TemporaryDirectory() as temp_dir:
            assets_dir = Path(temp_dir)
            manifest = assets_dir / MODULE.MANIFEST_NAME
            arm64 = assets_dir / MODULE.RUNTIME_NAMES[1]
            amd64 = assets_dir / MODULE.RUNTIME_NAMES[0]
            manifest.write_text("{}", encoding="utf-8")
            arm64.write_bytes(b"a" * 10)
            amd64.write_bytes(b"b" * 20)

            selected, skipped = MODULE.collect_asset_paths(assets_dir, 100)

            self.assertEqual(
                [path.name for path in selected],
                [manifest.name, arm64.name, amd64.name],
            )
            self.assertEqual(skipped, [])

    def test_missing_manifest_fails_before_release_upload(self) -> None:
        with tempfile.TemporaryDirectory() as temp_dir:
            assets_dir = Path(temp_dir)
            (assets_dir / MODULE.RUNTIME_NAMES[0]).write_bytes(b"runtime")

            with self.assertRaisesRegex(RuntimeError, MODULE.MANIFEST_NAME):
                MODULE.collect_asset_paths(assets_dir, 100)


class UploadReleaseAssetTest(unittest.TestCase):
    @mock.patch.object(MODULE.subprocess, "run")
    def test_curl_uses_official_multipart_fields_and_validates_response(
        self, run: mock.Mock
    ) -> None:
        run.return_value = subprocess.CompletedProcess(
            args=[],
            returncode=0,
            stdout=json.dumps({"id": 123, "name": MODULE.MANIFEST_NAME}),
            stderr="",
        )
        file_path = Path("/tmp") / MODULE.MANIFEST_NAME

        result = MODULE.upload_release_asset("owner", "repo", 7, "secret", file_path)

        self.assertEqual(result["id"], 123)
        command = run.call_args.args[0]
        self.assertNotIn("--retry", command)
        self.assertNotIn("--location", command)
        self.assertEqual(command[command.index("--max-time") + 1], "60")
        self.assertIn("access_token=secret", command)
        self.assertIn(f"file=@{file_path}", command)
        self.assertNotIn("Content-Type: multipart/form-data", command)
        self.assertNotIn("owner=owner", command)
        self.assertNotIn("repo=repo", command)
        self.assertNotIn("release_id=7", command)

    @mock.patch.object(MODULE.subprocess, "run")
    def test_invalid_success_response_is_rejected(self, run: mock.Mock) -> None:
        run.return_value = subprocess.CompletedProcess(
            args=[], returncode=0, stdout="{}", stderr=""
        )

        with self.assertRaisesRegex(RuntimeError, "unexpected response"):
            MODULE.upload_release_asset(
                "owner", "repo", 7, "secret", Path("asset.bin")
            )

    @mock.patch.object(MODULE.subprocess, "run")
    def test_runtime_upload_has_bounded_time(self, run: mock.Mock) -> None:
        run.return_value = subprocess.CompletedProcess(
            args=[], returncode=0, stdout=json.dumps({"id": 456}), stderr=""
        )

        MODULE.upload_release_asset(
            "owner", "repo", 7, "secret", Path(MODULE.RUNTIME_NAMES[0])
        )

        command = run.call_args.args[0]
        self.assertEqual(command[command.index("--max-time") + 1], "180")

    @mock.patch.object(MODULE, "list_release_assets")
    @mock.patch.object(MODULE.subprocess, "run")
    def test_failed_response_is_accepted_when_asset_exists(
        self, run: mock.Mock, list_assets: mock.Mock
    ) -> None:
        run.return_value = subprocess.CompletedProcess(
            args=[], returncode=22, stdout="bad gateway", stderr="HTTP 502"
        )
        list_assets.return_value = [{"id": 789, "name": MODULE.MANIFEST_NAME}]

        result = MODULE.upload_release_asset(
            "owner", "repo", 7, "secret", Path(MODULE.MANIFEST_NAME)
        )

        self.assertEqual(result["id"], 789)
        self.assertEqual(run.call_count, 1)

    @mock.patch.object(MODULE.time, "sleep")
    @mock.patch.object(MODULE, "list_release_assets", return_value=[])
    @mock.patch.object(MODULE.subprocess, "run")
    def test_http_502_is_retried_after_confirming_asset_missing(
        self, run: mock.Mock, _list_assets: mock.Mock, sleep: mock.Mock
    ) -> None:
        run.side_effect = [
            subprocess.CompletedProcess(
                args=[], returncode=22, stdout="bad gateway", stderr="HTTP 502"
            ),
            subprocess.CompletedProcess(
                args=[], returncode=0, stdout=json.dumps({"id": 321}), stderr=""
            ),
        ]

        result = MODULE.upload_release_asset(
            "owner", "repo", 7, "secret", Path(MODULE.RUNTIME_NAMES[0])
        )

        self.assertEqual(result["id"], 321)
        self.assertEqual(run.call_count, 2)
        sleep.assert_called_once_with(2)


class UploadReleaseAssetsTest(unittest.TestCase):
    @mock.patch.object(MODULE, "upload_release_asset")
    def test_runtime_mirror_failure_is_warning_after_manifest_success(
        self, upload: mock.Mock
    ) -> None:
        manifest = Path(MODULE.MANIFEST_NAME)
        runtime = Path(MODULE.RUNTIME_NAMES[0])
        upload.side_effect = [{"id": 1}, RuntimeError("HTTP 502")]

        warnings = MODULE.upload_release_assets(
            "owner", "repo", 7, "secret", [manifest, runtime], {}
        )

        self.assertEqual(len(warnings), 1)
        self.assertIn(runtime.name, warnings[0])

    @mock.patch.object(MODULE, "upload_release_asset")
    def test_manifest_upload_failure_remains_fatal(self, upload: mock.Mock) -> None:
        upload.side_effect = RuntimeError("HTTP 400")

        with self.assertRaisesRegex(RuntimeError, "HTTP 400"):
            MODULE.upload_release_assets(
                "owner", "repo", 7, "secret", [Path(MODULE.MANIFEST_NAME)], {}
            )

    @mock.patch.object(MODULE, "upload_release_asset")
    def test_existing_asset_is_kept_without_reupload(self, upload: mock.Mock) -> None:
        manifest = Path(MODULE.MANIFEST_NAME)

        warnings = MODULE.upload_release_assets(
            "owner", "repo", 7, "secret", [manifest], {manifest.name: 99}
        )

        self.assertEqual(warnings, [])
        upload.assert_not_called()


if __name__ == "__main__":
    unittest.main()
