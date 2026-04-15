#!/usr/bin/env python3
"""
更新日志同步脚本
从 CHANGELOG.json 读取更新日志，自动同步到 readme.md、wiki/docs/index.md、UPDATE_MANIFEST.json
"""

import json
import re
import sys
import io
from pathlib import Path

# 强制使用 UTF-8 编码输出（解决 Windows 控制台 GBK 编码问题）
if sys.platform == "win32":
    sys.stdout = io.TextIOWrapper(sys.stdout.buffer, encoding="utf-8")
    sys.stderr = io.TextIOWrapper(sys.stderr.buffer, encoding="utf-8", errors="replace")


def load_changelog():
    """加载 CHANGELOG.json"""
    with open("CHANGELOG.json", "r", encoding="utf-8") as f:
        return json.load(f)


def generate_markdown_changelog(changelog):
    """生成 Markdown 格式的更新日志"""
    lines = ["# 更新日志\n"]

    for version in changelog["history"]:
        lines.append(f"### {version['version']}（{version['date']}）\n")
        if "note" in version:
            lines.append(f"> **{version['note']}**\n")
        for change in version["changes"]:
            lines.append(f"- {change}")
        lines.append("")

    return "\n".join(lines)


def update_readme(changelog_md):
    """更新 readme.md 中的更新日志部分"""
    readme_path = Path("readme.md")
    content = readme_path.read_text(encoding="utf-8")

    # 匹配更新日志部分直到下一个一级标题或文件结尾
    pattern = r"(# 更新日志\n)(.*?)(?=\n# |\Z)"
    match = re.search(pattern, content, re.DOTALL)

    if match:
        # 替换更新日志部分
        before_log = content[: match.start()]
        after_log = content[match.end() :]
        new_content = before_log + changelog_md + after_log
    else:
        # 如果没有找到，添加到文件末尾
        new_content = content + "\n\n" + changelog_md

    readme_path.write_text(new_content, encoding="utf-8")
    print("✓ readme.md 已更新")


def update_wiki_docs(changelog_md):
    """更新 wiki/docs/index.md 中的更新日志部分"""
    wiki_path = Path("wiki/docs/index.md")
    content = wiki_path.read_text(encoding="utf-8")

    # 查找更新日志部分直到文件结尾
    pattern = r"(# 更新日志\n)(.*?)(?=\n---|\Z)"
    match = re.search(pattern, content, re.DOTALL)

    if match:
        # 替换更新日志部分
        before_log = content[: match.start()]
        after_log = content[match.end() :]
        new_content = before_log + changelog_md + after_log
    else:
        # 如果没有找到，添加到文件末尾
        new_content = content + "\n\n---\n\n" + changelog_md

    wiki_path.write_text(new_content, encoding="utf-8")
    print("✓ wiki/docs/index.md 已更新")


def update_update_manifest(changelog):
    """根据 CHANGELOG.json 重写根目录 UPDATE_MANIFEST.json。

    与 scripts/build-update-artifacts.sh 生成的结构一致；sha256 留空由 Release 工作流补全。
    产物 size 优先沿用旧 manifest 同架构数值，避免无本地构建时丢失占位信息。
    """
    history = changelog.get("history") or []
    if not history:
        raise ValueError("CHANGELOG.json 缺少 history")
    version = str(history[0].get("version", "")).strip()
    if not version:
        raise ValueError("CHANGELOG.json 缺少 history[0].version")

    latest_log = history[0]
    sources = changelog.get("artifact_sources") or []

    def expand_urls(filename: str) -> list:
        urls = []
        seen = set()
        for source in sources:
            if isinstance(source, dict):
                template = str(source.get("url", "")).strip()
            else:
                template = str(source).strip()
            if not template:
                continue
            expanded = template.replace("{version}", version).replace("{filename}", filename)
            if expanded and expanded not in seen:
                seen.add(expanded)
                urls.append(expanded)
        return urls

    old_sizes = {}
    manifest_path = Path("UPDATE_MANIFEST.json")
    if manifest_path.exists():
        try:
            old = json.loads(manifest_path.read_text(encoding="utf-8"))
            for art in old.get("latest", {}).get("artifacts") or []:
                arch = art.get("arch")
                if arch and art.get("size"):
                    old_sizes[str(arch)] = int(art["size"])
        except (json.JSONDecodeError, OSError, TypeError, ValueError):
            pass

    default_sizes = {"amd64": 11163781, "arm64": 10104449}
    artifacts = []
    for arch in ("amd64", "arm64"):
        filename = f"ptnexus-runtime-linux-{arch}.tar.gz"
        urls = expand_urls(filename)
        if not urls:
            raise ValueError(
                "CHANGELOG.json 缺少 artifact_sources，无法生成 UPDATE_MANIFEST 下载地址"
            )
        entry = {
            "os": "linux",
            "arch": arch,
            "url": urls[0],
            "sha256": "",
            "size": old_sizes.get(arch, default_sizes[arch]),
            "format": "tar.gz",
        }
        if len(urls) > 1:
            entry["mirror_urls"] = urls[1:]
        artifacts.append(entry)

    latest = {
        "version": version,
        "artifacts": artifacts,
    }
    date = str(latest_log.get("date", "")).strip()
    if date:
        latest["date"] = date
    if "force_update" in latest_log:
        latest["force_update"] = bool(latest_log.get("force_update"))
    if "disable_update" in latest_log:
        latest["disable_update"] = bool(latest_log.get("disable_update"))
    note = latest_log.get("note")
    if note:
        latest["note"] = str(note).strip()

    manifest = {
        "schema": 2,
        "latest": latest,
        "history": history,
    }

    manifest_path.write_text(
        json.dumps(manifest, ensure_ascii=False, indent=2) + "\n",
        encoding="utf-8",
    )
    print("✓ UPDATE_MANIFEST.json 已更新")


def main():
    """主函数"""
    print("🔄 开始同步更新日志...\n")

    try:
        # 加载 CHANGELOG.json
        changelog = load_changelog()

        # 生成 Markdown 格式的更新日志
        changelog_md = generate_markdown_changelog(changelog)

        # 更新各个文件
        update_readme(changelog_md)
        update_wiki_docs(changelog_md)
        update_update_manifest(changelog)

        print(f"\n✅ 更新日志同步完成！当前版本: {changelog['history'][0]['version']}")

    except Exception as e:
        print(f"\n❌ 同步失败: {e}")
        exit(1)


if __name__ == "__main__":
    main()
