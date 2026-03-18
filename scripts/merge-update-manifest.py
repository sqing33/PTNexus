#!/usr/bin/env python3
import argparse
import json
from pathlib import Path


def load_json(path: Path):
    return json.loads(path.read_text(encoding="utf-8"))


def normalize_desktop_installer(item: dict) -> dict:
    normalized = dict(item)
    kind = str(normalized.get("kind", "")).strip().lower()
    normalized["kind"] = "patch" if kind == "patch" else "full"
    return normalized


def desktop_installer_sort_key(item: dict):
    kind_rank = 0 if item.get("kind") == "patch" else 1
    return (
        str(item.get("platform", "")).strip(),
        str(item.get("arch", "")).strip(),
        kind_rank,
        str(item.get("file_name", "")).strip(),
    )


def build_base_manifest(changelog: dict, version: str) -> dict:
    history = changelog.get("history") or []
    latest_log = history[0] if history else {}

    latest = {
        "version": version,
        "artifacts": [],
    }
    if latest_log.get("date"):
        latest["date"] = latest_log["date"]
    if "force_update" in latest_log:
        latest["force_update"] = bool(latest_log.get("force_update"))
    if "disable_update" in latest_log:
        latest["disable_update"] = bool(latest_log.get("disable_update"))
    if latest_log.get("note"):
        latest["note"] = latest_log["note"]

    return {
        "schema": 2,
        "latest": latest,
        "history": history,
    }


def main() -> int:
    parser = argparse.ArgumentParser(description="Merge desktop installer metadata into UPDATE_MANIFEST.json")
    parser.add_argument("--changelog", required=True, help="CHANGELOG.json path")
    parser.add_argument("--version", required=True, help="Release version")
    parser.add_argument("--output", required=True, help="Target manifest path")
    parser.add_argument("--manifest", help="Existing manifest path", default="")
    parser.add_argument(
        "--desktop-installer",
        action="append",
        default=[],
        help="Desktop installer metadata JSON path, can be repeated",
    )
    args = parser.parse_args()

    changelog = load_json(Path(args.changelog).resolve())
    manifest = build_base_manifest(changelog, args.version.strip())

    manifest_path = Path(args.manifest).resolve() if args.manifest else None
    if manifest_path and manifest_path.is_file():
        existing = load_json(manifest_path)
        existing_latest = existing.get("latest") or {}
        manifest["schema"] = existing.get("schema") or manifest["schema"]
        manifest["latest"]["artifacts"] = existing_latest.get("artifacts") or []

    desktop_installers = []
    for raw_path in args.desktop_installer:
        path = Path(raw_path).resolve()
        if not path.is_file():
            raise SystemExit(f"desktop installer metadata not found: {path}")
        desktop_installers.append(normalize_desktop_installer(load_json(path)))

    if desktop_installers:
        manifest["latest"]["desktop_installers"] = sorted(desktop_installers, key=desktop_installer_sort_key)

    output_path = Path(args.output).resolve()
    output_path.parent.mkdir(parents=True, exist_ok=True)
    output_path.write_text(json.dumps(manifest, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
