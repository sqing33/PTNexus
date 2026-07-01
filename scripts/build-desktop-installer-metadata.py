#!/usr/bin/env python3
import argparse
import hashlib
import json
from pathlib import Path


DEFAULT_BASE_URL = "https://github.com/jadylc/PTNexus/releases/download/{version}"
DEFAULT_PLATFORM = "windows-desktop"


def sha256_of(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as fh:
        for chunk in iter(lambda: fh.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def load_urls(changelog_path: Path, version: str, filename: str, fallback_base_url: str) -> list[str]:
    data = json.loads(changelog_path.read_text(encoding="utf-8"))
    sources = data.get("artifact_sources") or []

    urls: list[str] = []
    for source in sources:
        template = ""
        if isinstance(source, dict):
            template = str(source.get("url", "")).strip()
        elif isinstance(source, str):
            template = source.strip()
        if not template:
            continue
        expanded = template.replace("{version}", version).replace("{filename}", filename)
        if expanded:
            urls.append(expanded)

    if not urls:
        fallback = fallback_base_url.rstrip("/")
        urls.append(f"{fallback}/{filename}")

    deduped: list[str] = []
    seen: set[str] = set()
    for item in urls:
        if not item or item in seen:
            continue
        seen.add(item)
        deduped.append(item)
    return deduped


def main() -> int:
    parser = argparse.ArgumentParser(description="Build desktop installer metadata for UPDATE_MANIFEST.json")
    parser.add_argument("--installer", required=True, help="Installer file path")
    parser.add_argument("--changelog", required=True, help="CHANGELOG.json path")
    parser.add_argument("--version", required=True, help="Release version")
    parser.add_argument("--kind", default="full", choices=["full", "patch"], help="Installer kind")
    parser.add_argument("--arch", default="amd64", help="Installer architecture")
    parser.add_argument("--platform", default=DEFAULT_PLATFORM, help="Installer platform key")
    parser.add_argument("--base-url", default="", help="Fallback release base URL")
    parser.add_argument("--output", required=True, help="Output JSON path")
    args = parser.parse_args()

    installer_path = Path(args.installer).resolve()
    changelog_path = Path(args.changelog).resolve()
    output_path = Path(args.output).resolve()

    if not installer_path.is_file():
        raise SystemExit(f"installer not found: {installer_path}")
    if not changelog_path.is_file():
        raise SystemExit(f"changelog not found: {changelog_path}")

    fallback_base_url = args.base_url.strip() or DEFAULT_BASE_URL.format(version=args.version.strip())
    urls = load_urls(
        changelog_path=changelog_path,
        version=args.version.strip(),
        filename=installer_path.name,
        fallback_base_url=fallback_base_url,
    )

    payload = {
        "platform": args.platform.strip() or DEFAULT_PLATFORM,
        "kind": args.kind.strip() or "full",
        "arch": args.arch.strip() or "amd64",
        "file_name": installer_path.name,
        "url": urls[0],
        "sha256": sha256_of(installer_path),
        "size": installer_path.stat().st_size,
    }
    if len(urls) > 1:
        payload["mirror_urls"] = urls[1:]

    output_path.parent.mkdir(parents=True, exist_ok=True)
    output_path.write_text(json.dumps(payload, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
