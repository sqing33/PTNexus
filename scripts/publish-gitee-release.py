#!/usr/bin/env python3
import argparse
import json
import subprocess
import sys
import urllib.error
import urllib.parse
import urllib.request
from pathlib import Path
from typing import Any


API_BASE = "https://gitee.com/api/v5"
DEFAULT_MAX_BYTES = 100 * 1024 * 1024
MANIFEST_NAME = "UPDATE_MANIFEST.json"


def add_query(url: str, **params: str) -> str:
    parsed = urllib.parse.urlsplit(url)
    query = urllib.parse.parse_qsl(parsed.query, keep_blank_values=True)
    for key, value in params.items():
        if value == "":
            continue
        query.append((key, value))
    return urllib.parse.urlunsplit(
        (
            parsed.scheme,
            parsed.netloc,
            parsed.path,
            urllib.parse.urlencode(query),
            parsed.fragment,
        )
    )


def encode_form_payload(payload: dict[str, Any]) -> bytes:
    form_items: list[tuple[str, str]] = []
    for key, value in payload.items():
        if value is None:
            continue
        if isinstance(value, bool):
            value = "true" if value else "false"
        form_items.append((key, str(value)))
    return urllib.parse.urlencode(form_items).encode("utf-8")


def api_request(
    method: str,
    url: str,
    token: str,
    payload: dict[str, Any] | None = None,
    *,
    form_encoded: bool = False,
) -> Any:
    request_url = add_query(url, access_token=token)
    data = None
    headers = {
        "Accept": "application/json",
        "User-Agent": "ptnexus-release-sync",
    }
    if payload is not None:
        if form_encoded:
            data = encode_form_payload(payload)
            headers["Content-Type"] = "application/x-www-form-urlencoded"
        else:
            data = json.dumps(payload, ensure_ascii=False).encode("utf-8")
            headers["Content-Type"] = "application/json"

    request = urllib.request.Request(request_url, data=data, headers=headers, method=method)
    try:
        with urllib.request.urlopen(request, timeout=30) as response:
            raw = response.read()
    except urllib.error.HTTPError as exc:
        body = exc.read().decode("utf-8", errors="replace").strip()
        raise RuntimeError(f"{method} {url} failed with HTTP {exc.code}: {body}") from exc
    except urllib.error.URLError as exc:
        raise RuntimeError(f"{method} {url} failed: {exc}") from exc

    if not raw:
        return None
    return json.loads(raw.decode("utf-8"))


def get_release_by_tag(owner: str, repo: str, tag: str, token: str) -> dict[str, Any] | None:
    url = f"{API_BASE}/repos/{urllib.parse.quote(owner)}/{urllib.parse.quote(repo)}/releases/tags/{urllib.parse.quote(tag)}"
    request_url = add_query(url, access_token=token)
    request = urllib.request.Request(
        request_url,
        headers={"Accept": "application/json", "User-Agent": "ptnexus-release-sync"},
        method="GET",
    )
    try:
        with urllib.request.urlopen(request, timeout=30) as response:
            return json.loads(response.read().decode("utf-8"))
    except urllib.error.HTTPError as exc:
        if exc.code == 404:
            return None
        body = exc.read().decode("utf-8", errors="replace").strip()
        raise RuntimeError(f"GET {url} failed with HTTP {exc.code}: {body}") from exc
    except urllib.error.URLError as exc:
        raise RuntimeError(f"GET {url} failed: {exc}") from exc


def create_release(
    owner: str,
    repo: str,
    tag: str,
    target_commitish: str,
    name: str,
    body: str,
    token: str,
) -> dict[str, Any]:
    url = f"{API_BASE}/repos/{urllib.parse.quote(owner)}/{urllib.parse.quote(repo)}/releases"
    payload = {
        "tag_name": tag,
        "target_commitish": target_commitish,
        "name": name,
        "body": body,
        "prerelease": False,
    }
    result = api_request("POST", url, token, payload=payload, form_encoded=True)
    if not isinstance(result, dict) or not result.get("id"):
        raise RuntimeError(f"unexpected create release response: {result!r}")
    return result


def update_release(
    owner: str,
    repo: str,
    release_id: int,
    tag: str,
    target_commitish: str,
    name: str,
    body: str,
    token: str,
) -> dict[str, Any]:
    url = f"{API_BASE}/repos/{urllib.parse.quote(owner)}/{urllib.parse.quote(repo)}/releases/{release_id}"
    payload = {
        "tag_name": tag,
        "target_commitish": target_commitish,
        "name": name,
        "body": body,
        "prerelease": False,
    }
    result = api_request("PATCH", url, token, payload=payload, form_encoded=True)
    if not isinstance(result, dict):
        raise RuntimeError(f"unexpected update release response: {result!r}")
    return result


def list_release_assets(owner: str, repo: str, release_id: int, token: str) -> list[dict[str, Any]]:
    url = f"{API_BASE}/repos/{urllib.parse.quote(owner)}/{urllib.parse.quote(repo)}/releases/{release_id}/attach_files"
    result = api_request("GET", url, token)
    if result is None:
        return []
    if not isinstance(result, list):
        raise RuntimeError(f"unexpected list assets response: {result!r}")
    return result


def delete_release_asset(owner: str, repo: str, release_id: int, attach_file_id: int, token: str) -> None:
    url = (
        f"{API_BASE}/repos/{urllib.parse.quote(owner)}/{urllib.parse.quote(repo)}/releases/"
        f"{release_id}/attach_files/{attach_file_id}"
    )
    api_request("DELETE", url, token)


def upload_release_asset(owner: str, repo: str, release_id: int, token: str, file_path: Path) -> None:
    url = f"{API_BASE}/repos/{owner}/{repo}/releases/{release_id}/attach_files"
    command = [
        "curl",
        "-fsS",
        "-X",
        "POST",
        "-H",
        "Content-Type: multipart/form-data",
        "-F",
        f"access_token={token}",
        "-F",
        f"owner={owner}",
        "-F",
        f"repo={repo}",
        "-F",
        f"release_id={release_id}",
        "-F",
        f"file=@{file_path}",
        url,
    ]
    completed = subprocess.run(command, capture_output=True, text=True)
    if completed.returncode != 0:
        stderr = completed.stderr.strip()
        stdout = completed.stdout.strip()
        detail = stderr or stdout or "unknown curl error"
        raise RuntimeError(f"upload {file_path.name} failed: {detail}")


def collect_asset_paths(assets_dir: Path, max_bytes: int) -> tuple[list[Path], list[str]]:
    selected: list[Path] = []
    skipped: list[str] = []
    for path in sorted(p for p in assets_dir.iterdir() if p.is_file()):
        size = path.stat().st_size
        if path.name != MANIFEST_NAME and size > max_bytes:
            skipped.append(f"{path.name}: skipped ({size} bytes > {max_bytes})")
            continue
        selected.append(path)

    selected.sort(key=lambda path: (path.name == MANIFEST_NAME, path.name))
    return selected, skipped


def read_body(body_file: Path) -> str:
    if not body_file.is_file():
        raise RuntimeError(f"release body file not found: {body_file}")
    return body_file.read_text(encoding="utf-8").strip()


def main() -> int:
    parser = argparse.ArgumentParser(description="Publish release assets to Gitee Release.")
    parser.add_argument("--owner", required=True, help="Gitee owner")
    parser.add_argument("--repo", required=True, help="Gitee repo")
    parser.add_argument("--tag", required=True, help="Release tag")
    parser.add_argument("--target-commitish", required=True, help="Branch or commitish used by Gitee when creating the tag")
    parser.add_argument("--name", required=True, help="Release name")
    parser.add_argument("--body-file", required=True, help="Release body markdown file")
    parser.add_argument("--assets-dir", required=True, help="Directory containing release assets")
    parser.add_argument("--token", required=True, help="Gitee access token")
    parser.add_argument("--max-bytes", type=int, default=DEFAULT_MAX_BYTES, help="Max upload size per asset")
    parser.add_argument("--dry-run", action="store_true", help="Print selected files without calling Gitee API")
    args = parser.parse_args()

    body = read_body(Path(args.body_file).resolve())
    assets_dir = Path(args.assets_dir).resolve()
    if not assets_dir.is_dir():
        raise RuntimeError(f"assets directory not found: {assets_dir}")

    selected_paths, skipped = collect_asset_paths(assets_dir, args.max_bytes)
    if not selected_paths:
        raise RuntimeError(f"no assets selected from {assets_dir}")

    print(
        f"[gitee-release] repo={args.owner}/{args.repo} tag={args.tag} target_commitish={args.target_commitish}"
    )
    for message in skipped:
        print(f"[gitee-release] {message}")
    for path in selected_paths:
        print(f"[gitee-release] upload {path.name}")

    if args.dry_run:
        print("[gitee-release] dry run complete")
        return 0

    release = get_release_by_tag(args.owner, args.repo, args.tag, args.token)
    if release is None:
        print(f"[gitee-release] creating release {args.tag}")
        release = create_release(
            args.owner,
            args.repo,
            args.tag,
            args.target_commitish,
            args.name,
            body,
            args.token,
        )
    else:
        print(f"[gitee-release] updating release {args.tag} (id={release.get('id')})")
        release = update_release(
            args.owner,
            args.repo,
            int(release["id"]),
            args.tag,
            args.target_commitish,
            args.name,
            body,
            args.token,
        )

    release_id = int(release["id"])
    existing_assets = list_release_assets(args.owner, args.repo, release_id, args.token)
    existing_by_name = {
        str(item.get("name", "")).strip(): int(item["id"])
        for item in existing_assets
        if str(item.get("name", "")).strip() and item.get("id") is not None
    }

    for path in selected_paths:
        attach_id = existing_by_name.get(path.name)
        if attach_id is not None:
            print(f"[gitee-release] replacing {path.name}")
            delete_release_asset(args.owner, args.repo, release_id, attach_id, args.token)
        upload_release_asset(args.owner, args.repo, release_id, args.token, path)

    print(f"[gitee-release] release ready: {release.get('html_url', '')}")
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except RuntimeError as exc:
        print(f"[gitee-release] {exc}", file=sys.stderr)
        raise SystemExit(1)
