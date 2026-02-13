import json
import os
import re
from loguru import logger

from ..uploader import SpecialUploader
from config import SITES_DATA_FILE


class HdfansUploader(SpecialUploader):
    """
    HDFans站点特殊上传器

    特殊逻辑：
    1) 媒介细分：仅对原盘按 DIY 做区分（其余媒介沿用 YAML mappings.medium 的默认映射）
       - 17 UHD原盘 / 18 UHD DIY
       - 21 BD原盘 / 22 BD DIY
    2) 中英双语：仅“音轨双语”规则（中文音轨 + 英文音轨）添加 tag.中英双语
    3) 源站转发：用制作组去匹配 sites.group（去掉前缀 -），若命中的站点 nickname 与源站 nickname 相同则添加 tag.源站转发
    """

    def _map_parameters(self) -> dict:
        standardized_params = self.upload_data.get("standardized_params", {})
        if not standardized_params:
            logger.warning("未找到标准化参数，回退到重新解析")
            standardized_params = self._parse_source_data()

        mapped_params = self._map_standardized_params(standardized_params)

        enhanced_tags = self._build_enhanced_tags(standardized_params)
        mapped_params = self._rebuild_tag_fields(mapped_params, enhanced_tags)

        self._refine_medium(mapped_params, standardized_params, enhanced_tags)

        return mapped_params

    def _build_enhanced_tags(self, standardized_params: dict) -> set[str]:
        tags = set(self._collect_all_tags() or set())

        # --- 中英双语：仅音轨双语 ---
        cn_audio_tags = {"tag.国语", "tag.粤语", "tag.台配", "国语", "粤语", "台配"}
        en_audio_tags = {"tag.英语", "英语"}
        has_cn_audio = any(t in tags for t in cn_audio_tags)
        has_en_audio = any(t in tags for t in en_audio_tags)
        if has_cn_audio and has_en_audio:
            tags.add("tag.中英双语")

        # --- 源站转发 ---
        try:
            source_site_nickname = (self._get_source_site_nickname() or "").strip()
            if source_site_nickname:
                release_group = self._extract_release_group_raw(standardized_params)
                matched_site_nickname = self._find_site_nickname_by_group(release_group)
                if matched_site_nickname and matched_site_nickname.strip() == source_site_nickname:
                    tags.add("tag.源站转发")
        except Exception as e:
            logger.warning(f"HDFans 源站转发标签计算失败: {e}")

        return tags

    def _extract_release_group_raw(self, standardized_params: dict) -> str:
        # 1) 优先使用 title_components 的原始制作组
        for item in self.upload_data.get("title_components", []) or []:
            if item.get("key") == "制作组" and item.get("value"):
                value = item.get("value")
                if isinstance(value, list):
                    value = " ".join(map(str, value))
                return self._normalize_release_group(str(value))

        # 2) 其次使用 source_params 的制作组（在你这次日志中 source_params['制作组'] = LuckAni）
        source_params = self.upload_data.get("source_params", {}) or {}
        if source_params.get("制作组"):
            return self._normalize_release_group(str(source_params.get("制作组") or ""))

        # 3) 再尝试从 modified_torrent_path 文件名中解析尾部 -Group（如 xxx-LuckDIY.torrent）
        mtp = self.upload_data.get("modified_torrent_path") or ""
        if mtp:
            filename = os.path.basename(str(mtp))
            m = re.search(r"-([^-]+)\.torrent$", filename, re.IGNORECASE)
            if m:
                return self._normalize_release_group(m.group(1))

        # 2) 兜底使用 standardized team（可能是 team.xxx；但仍做一下清洗）
        return self._normalize_release_group(str(standardized_params.get("team") or ""))

    def _normalize_release_group(self, value: str) -> str:
        v = (value or "").strip()
        if not v:
            return ""

        # 合作制作组：使用 @ 后面
        if "@" in v:
            parts = v.split("@")
            if len(parts) >= 2 and parts[1].strip():
                v = parts[1].strip()

        # 去掉可能的连字符前缀
        v = v.lstrip("-").strip()
        return v

    def _find_site_nickname_by_group(self, release_group: str) -> str | None:
        release_group = self._normalize_release_group(release_group)
        if not release_group:
            return None

        # 优先：数据库 sites 表
        db_manager = self.upload_data.get("_db_manager")
        if db_manager:
            try:
                nickname = self._find_site_nickname_by_group_from_db(db_manager, release_group)
                if nickname:
                    return nickname
            except Exception as e:
                logger.warning(f"HDFans 从数据库查找 group 失败，回退到 JSON: {e}")

        # 回退：sites_data.json
        return self._find_site_nickname_by_group_from_json(release_group)

    def _find_site_nickname_by_group_from_db(self, db_manager, release_group: str) -> str | None:
        conn = db_manager._get_connection()
        cursor = db_manager._get_cursor(conn)
        try:
            group_col = "\"group\"" if db_manager.db_type == "postgresql" else "`group`"
            cursor.execute(f"SELECT nickname, {group_col} AS site_group FROM sites")
            rows = cursor.fetchall() or []
            for row in rows:
                row_dict = dict(row)
                nickname = row_dict.get("nickname")
                site_group = row_dict.get("site_group") or ""
                if self._group_matches(site_group, release_group):
                    return nickname
            return None
        finally:
            try:
                cursor.close()
            except Exception:
                pass
            try:
                conn.close()
            except Exception:
                pass

    def _find_site_nickname_by_group_from_json(self, release_group: str) -> str | None:
        try:
            candidate_paths = [SITES_DATA_FILE]
            repo_server_dir = os.path.dirname(
                os.path.dirname(os.path.dirname(os.path.dirname(__file__)))
            )
            candidate_paths.append(os.path.join(repo_server_dir, "sites_data.json"))

            sites_data_path = next((p for p in candidate_paths if p and os.path.exists(p)), None)
            if not sites_data_path:
                return None

            with open(sites_data_path, "r", encoding="utf-8") as f:
                data = json.load(f) or []
            for item in data:
                site_group = (item.get("group") or "").strip()
                if self._group_matches(site_group, release_group):
                    return item.get("nickname")
            return None
        except Exception:
            return None

    def _group_matches(self, site_group: str, release_group: str) -> bool:
        sg = (site_group or "").strip()
        rg = (release_group or "").strip()
        if not sg or not rg:
            return False

        # 支持 sites.group 类似 "-A/B" 或 "-A,-B,-C" 的情况（注意每个分段可能仍带 '-'）
        parts = [p for p in re.split(r"[\\/|,\\s]+", sg) if p]
        parts = [str(p).strip().lstrip("-").strip() for p in parts if str(p).strip()]
        return any(p.lower() == rg.lower() for p in parts)

    def _get_source_site_nickname(self) -> str:
        # 1) 优先使用发布入口注入（routes_migrate.py）
        injected = (self.upload_data.get("source_site_nickname") or "").strip()
        if injected:
            return injected

        # 2) 尝试通过站点 code 从 DB 或 sites_data.json 推断
        source_site_code = (self.upload_data.get("source_site_code") or "").strip()
        if not source_site_code:
            mtp = self.upload_data.get("modified_torrent_path") or ""
            if mtp:
                filename = os.path.basename(str(mtp))
                m = re.match(r"^([^-]+)-\d+-", filename)
                if m:
                    source_site_code = m.group(1).strip()

        if not source_site_code:
            return ""

        db_manager = self.upload_data.get("_db_manager")
        if db_manager:
            try:
                conn = db_manager._get_connection()
                cursor = db_manager._get_cursor(conn)
                try:
                    ph = db_manager.get_placeholder()
                    cursor.execute(f"SELECT nickname FROM sites WHERE site = {ph}", (source_site_code,))
                    row = cursor.fetchone()
                    if row:
                        return str(dict(row).get("nickname") or "").strip()
                finally:
                    try:
                        cursor.close()
                    except Exception:
                        pass
                    try:
                        conn.close()
                    except Exception:
                        pass
            except Exception:
                pass

        # JSON fallback
        try:
            candidate_paths = [SITES_DATA_FILE]
            repo_server_dir = os.path.dirname(
                os.path.dirname(os.path.dirname(os.path.dirname(__file__)))
            )
            candidate_paths.append(os.path.join(repo_server_dir, "sites_data.json"))
            sites_data_path = next((p for p in candidate_paths if p and os.path.exists(p)), None)
            if not sites_data_path:
                return ""
            with open(sites_data_path, "r", encoding="utf-8") as f:
                data = json.load(f) or []
            for item in data:
                if (item.get("site") or "").strip() == source_site_code:
                    return str(item.get("nickname") or "").strip()
        except Exception:
            pass

        return ""

    def _rebuild_tag_fields(self, mapped_params: dict, tags: set[str]) -> dict:
        # 移除基类可能生成的 tags 字段，避免残留
        cleaned = {k: v for k, v in mapped_params.items() if not str(k).startswith("tags[")}

        tag_mapping = self.mappings.get("tag", {}) or {}
        tag_ids = []
        for t in sorted(tags):
            tag_id = self._find_mapping(tag_mapping, t, mapping_type="tag")
            if tag_id is not None and tag_id != "":
                tag_ids.append(tag_id)

        for i, tag_id in enumerate(sorted(set(tag_ids), key=lambda x: str(x))):
            cleaned[f"tags[4][{i}]"] = tag_id

        return cleaned

    def _refine_medium(self, mapped_params: dict, standardized_params: dict, tags: set[str]) -> None:
        medium_field = self.config.get("form_fields", {}).get("medium", "medium_sel[4]")
        medium = str(standardized_params.get("medium") or "").strip()
        resolution = str(standardized_params.get("resolution") or "").strip()

        has_diy = ("tag.DIY" in tags) or any("DIY" in str(t).upper() for t in tags)

        # UHD / BD 原盘（仅 DIY 细分）
        if medium in {"medium.uhd_bluray", "medium.uhd_diy"}:
            mapped_params[medium_field] = "18" if has_diy else "17"
            return
        if medium in {"medium.bluray", "medium.bluray_diy"}:
            mapped_params[medium_field] = "22" if has_diy else "21"
            return

        # Encode：按分辨率细分（UHD=20 / 1080P/i=24 / 720P=25）
        if medium in {
            "medium.encode",
            "medium.encode_2160p",
            "medium.encode_1080p",
            "medium.encode_720p",
        }:
            if medium == "medium.encode_2160p":
                mapped_params[medium_field] = "20"
                return
            if medium == "medium.encode_720p":
                mapped_params[medium_field] = "25"
                return
            if medium == "medium.encode_1080p":
                mapped_params[medium_field] = "24"
                return

            # medium.encode: fallback by resolution
            if resolution == "resolution.r2160p":
                mapped_params[medium_field] = "20"
                return
            if resolution == "resolution.r720p":
                mapped_params[medium_field] = "25"
                return

            mapped_params[medium_field] = "24"
            return
