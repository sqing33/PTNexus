import logging
import time
from threading import Thread, Event
from urllib.parse import urlparse

import requests

from qbittorrentapi import Client
from transmission_rpc import Client as TrClient

from core.services import _prepare_api_config
from utils import _extract_core_domain, _extract_url_from_comment, _parse_hostname_from_url


ratio_speed_limiter_thread = None


class RatioSpeedLimiter(Thread):
    """基于站点分享率阈值的出种限速线程。"""

    def __init__(self, db_manager, config_manager):
        super().__init__(daemon=True, name="RatioSpeedLimiter")
        self.db_manager = db_manager
        self.config_manager = config_manager
        self.interval = self._get_interval_seconds()  # 默认30分钟
        self.shutdown_event = Event()
        self.clients = {}
        self._warned_proxy_downloaders = set()

    def run(self):
        logging.info("RatioSpeedLimiter 线程已启动，检查间隔: %s 秒", self.interval)
        print(f"[RatioSpeedLimiter] 线程已启动，检查间隔: {self.interval} 秒")

        # 启动后先立即执行一次
        try:
            self._enforce_ratio_limits()
        except Exception as e:
            logging.error(f"RatioSpeedLimiter 启动后首次执行失败: {e}", exc_info=True)

        while not self.shutdown_event.wait(self.interval):
            try:
                self._enforce_ratio_limits()
            except Exception as e:
                logging.error(f"RatioSpeedLimiter 执行失败: {e}", exc_info=True)

    def stop(self):
        self.shutdown_event.set()

    def _get_interval_seconds(self):
        config = self.config_manager.get() or {}
        upload_settings = config.get("upload_settings", {})
        raw = upload_settings.get("ratio_limiter_interval_seconds", 1800)
        try:
            value = int(raw)
        except Exception:
            value = 1800
        # 最小10秒，最大86400秒（1天）
        return max(10, min(86400, value))

    def _enforce_ratio_limits(self):
        start_ts = time.time()
        print("[RatioSpeedLimiter] 开始执行分享率检测...")
        domain_rule_map = self._load_site_rules()
        if not domain_rule_map:
            logging.debug("[RatioSpeedLimiter] 未配置有效阈值，跳过本轮检查")
            print("[RatioSpeedLimiter] 未配置有效阈值，跳过本轮")
            return

        config = self.config_manager.get() or {}
        downloaders = [
            d for d in config.get("downloaders", [])
            if d.get("enabled") and d.get("enable_ratio_limiter", False)
        ]

        total = 0
        matched = 0
        limited = 0
        skipped = 0
        proxy_limited = 0

        for downloader in downloaders:
            downloader_id = downloader.get("id") or downloader.get("name") or "unknown"
            print(f"[RatioSpeedLimiter] 正在处理下载器: {downloader.get('name', downloader_id)}")

            try:
                torrents = self._fetch_torrents(downloader)
            except Exception as e:
                logging.error(f"下载器 {downloader_id} 获取种子失败: {e}")
                print(f"[RatioSpeedLimiter] 下载器 {downloader_id} 获取种子失败: {e}")
                skipped += 1
                continue

            total += len(torrents)
            if not torrents:
                print(f"[RatioSpeedLimiter] 下载器 {downloader_id} 未获取到种子")
                continue

            if downloader.get("use_proxy"):
                m, l = self._apply_for_proxy(downloader, torrents, domain_rule_map)
                proxy_limited += l
            elif downloader.get("type") == "qbittorrent":
                m, l = self._apply_for_qb(downloader, torrents, domain_rule_map)
            elif downloader.get("type") == "transmission":
                m, l = self._apply_for_tr(downloader, torrents, domain_rule_map)
            else:
                skipped += 1
                continue

            matched += m
            limited += l
            print(
                f"[RatioSpeedLimiter] 下载器 {downloader_id} 本轮匹配 {m} 个，执行限速 {l} 个"
            )

        elapsed = time.time() - start_ts
        logging.info(
            "[RatioSpeedLimiter] 本轮检查: 扫描 %s 个种子, 匹配 %s 个, 限速 %s 个, 跳过 %s 个, 耗时 %.2fs",
            total,
            matched,
            limited,
            skipped,
            elapsed,
        )
        print(
            f"[RatioSpeedLimiter] 本轮完成: 扫描 {total} 个, 匹配 {matched} 个, 限速 {limited} 个, 跳过 {skipped} 个, 耗时 {elapsed:.2f}s"
        )
        if proxy_limited > 0:
            logging.info("[RatioSpeedLimiter] 代理模式本轮限速种子数: %s", proxy_limited)
            print(f"[RatioSpeedLimiter] 代理模式本轮限速种子数: {proxy_limited}")

    def _load_site_rules(self):
        conn = self.db_manager._get_connection()
        cursor = self.db_manager._get_cursor(conn)
        try:
            if self.db_manager.db_type == "postgresql":
                cursor.execute(
                    'SELECT site, nickname, base_url, special_tracker_domain, ratio_threshold, seed_speed_limit FROM sites WHERE ratio_threshold IS NOT NULL AND ratio_threshold > 0 AND seed_speed_limit IS NOT NULL'
                )
            else:
                cursor.execute(
                    "SELECT site, nickname, base_url, special_tracker_domain, ratio_threshold, seed_speed_limit FROM sites WHERE ratio_threshold IS NOT NULL AND ratio_threshold > 0 AND seed_speed_limit IS NOT NULL"
                )

            rows = [dict(r) for r in cursor.fetchall()]
            domain_rule_map = {}
            for row in rows:
                rule = {
                    "site": row.get("site"),
                    "nickname": row.get("nickname"),
                    "ratio_threshold": max(0.1, float(row.get("ratio_threshold") or 3.0)),
                    "seed_speed_limit": int(row.get("seed_speed_limit") if row.get("seed_speed_limit") is not None else 5),
                }

                for host_like in (row.get("base_url"), row.get("special_tracker_domain")):
                    if not host_like:
                        continue
                    hostname = _parse_hostname_from_url(f"http://{host_like}")
                    if hostname:
                        domain_rule_map[_extract_core_domain(hostname)] = rule

            return domain_rule_map
        finally:
            cursor.close()
            conn.close()

    def _fetch_torrents(self, downloader):
        if downloader.get("use_proxy"):
            torrents = self._get_proxy_torrents(downloader)
            return torrents or []

        client = self._get_client(downloader)
        if downloader.get("type") == "qbittorrent":
            return client.torrents_info()
        if downloader.get("type") == "transmission":
            return client.get_torrents()
        return []

    def _get_client(self, downloader):
        downloader_id = downloader["id"]
        cached = self.clients.get(downloader_id)
        if cached:
            return cached

        api_config = _prepare_api_config(downloader)
        if downloader["type"] == "qbittorrent":
            client = Client(**api_config)
            client.auth_log_in()
        elif downloader["type"] == "transmission":
            client = TrClient(**api_config)
            client.get_session()
        else:
            raise ValueError(f"不支持的下载器类型: {downloader['type']}")

        self.clients[downloader_id] = client
        return client

    def _get_proxy_torrents(self, downloader):
        try:
            host_value = downloader["host"]
            parsed_url = urlparse(host_value if host_value.startswith(("http://", "https://")) else f"http://{host_value}")

            proxy_ip = parsed_url.hostname
            if not proxy_ip:
                return []

            proxy_port = downloader.get("proxy_port", 9090)
            proxy_base_url = f"http://{proxy_ip}:{proxy_port}"

            proxy_downloader_config = {
                "id": downloader["id"],
                "type": downloader["type"],
                "host": "http://127.0.0.1:" + str(parsed_url.port or 8080),
                "username": downloader.get("username", ""),
                "password": downloader.get("password", ""),
            }

            request_data = {
                "downloaders": [proxy_downloader_config],
                "include_comment": True,
                "include_trackers": True,
            }

            response = requests.post(
                f"{proxy_base_url}/api/torrents/all",
                json=request_data,
                timeout=600,
            )
            response.raise_for_status()
            return response.json() or []
        except Exception as e:
            logging.error(f"通过代理获取 '{downloader.get('name', downloader.get('id'))}' 种子信息失败: {e}")
            return []

    def _apply_for_proxy(self, downloader, torrents, domain_rule_map):
        matched = 0
        limited = 0
        ids_by_limit = {}

        for torrent in torrents:
            ratio = self._safe_float(
                torrent.get("ratio") if isinstance(torrent, dict) else getattr(torrent, "ratio", 0.0)
            )
            rule = self._match_site_rule(torrent, domain_rule_map)
            if not rule:
                continue

            matched += 1
            if ratio < rule["ratio_threshold"]:
                continue

            torrent_id = None
            if isinstance(torrent, dict):
                torrent_id = torrent.get("hash") or torrent.get("hashString") or torrent.get("hash_string")
            else:
                torrent_id = getattr(torrent, "hash", None) or getattr(torrent, "hashString", None) or getattr(torrent, "hash_string", None)

            if torrent_id:
                limit = int(rule["seed_speed_limit"])
                ids_by_limit.setdefault(limit, []).append(torrent_id)

        if not ids_by_limit:
            print(f"[RatioSpeedLimiter] 代理下载器 {downloader.get('id')} 无需限速")
            return matched, limited

        try:
            host_value = downloader["host"]
            parsed_url = urlparse(host_value if host_value.startswith(("http://", "https://")) else f"http://{host_value}")
            proxy_ip = parsed_url.hostname
            if not proxy_ip:
                return matched, limited

            proxy_port = downloader.get("proxy_port", 9090)
            proxy_base_url = f"http://{proxy_ip}:{proxy_port}"

            proxy_downloader = {
                "id": downloader["id"],
                "type": downloader["type"],
                "host": "http://127.0.0.1:" + str(parsed_url.port or 8080),
                "username": downloader.get("username", ""),
                "password": downloader.get("password", ""),
                "actions": [
                    {
                        "limit_mbps": limit,
                        "torrent_ids": torrent_ids,
                    }
                    for limit, torrent_ids in ids_by_limit.items()
                ],
            }

            resp = requests.post(
                f"{proxy_base_url}/api/torrents/upload-limit/batch",
                json={"downloaders": [proxy_downloader]},
                timeout=120,
            )
            resp.raise_for_status()
            payload = resp.json() or {}
            for result in payload.get("results", []):
                limited += int(result.get("applied_torrents", 0) or 0)
                print(
                    f"[RatioSpeedLimiter] 代理下载器 {downloader.get('id')} 已应用分组 {result.get('applied_groups', 0)}，限速种子 {result.get('applied_torrents', 0)}"
                )
                for err in result.get("errors", []):
                    logging.error("代理限速执行异常[%s]: %s", downloader.get("id"), err)
                    print(f"[RatioSpeedLimiter] 代理限速执行异常[{downloader.get('id')}]: {err}")

            return matched, limited
        except Exception as e:
            logging.error(f"代理下载器 {downloader.get('id')} 设置限速失败: {e}")
            print(f"[RatioSpeedLimiter] 代理下载器 {downloader.get('id')} 设置限速失败: {e}")
            return matched, limited

    def _apply_for_qb(self, downloader, torrents, domain_rule_map):
        matched = 0
        limited = 0
        hashes_by_limit = {}

        for torrent in torrents:
            ratio = self._safe_float(getattr(torrent, "ratio", 0.0))
            rule = self._match_site_rule(torrent, domain_rule_map)
            if not rule:
                continue

            matched += 1
            if ratio < rule["ratio_threshold"]:
                continue

            limit_bytes = self._convert_qb_limit(rule["seed_speed_limit"])
            hashes_by_limit.setdefault(limit_bytes, []).append(getattr(torrent, "hash", None))

        client = self._get_client(downloader)
        for limit_bytes, hashes in hashes_by_limit.items():
            valid_hashes = [h for h in hashes if h]
            if not valid_hashes:
                continue
            client.torrents_set_upload_limit(limit=limit_bytes, torrent_hashes=valid_hashes)
            limited += len(valid_hashes)

        return matched, limited

    def _apply_for_tr(self, downloader, torrents, domain_rule_map):
        matched = 0
        limited = 0
        ids_by_limit = {}

        for torrent in torrents:
            ratio = self._safe_float(getattr(torrent, "ratio", 0.0))
            rule = self._match_site_rule(torrent, domain_rule_map)
            if not rule:
                continue

            matched += 1
            if ratio < rule["ratio_threshold"]:
                continue

            limit = int(rule["seed_speed_limit"])
            ids_by_limit.setdefault(limit, []).append(
                getattr(torrent, "hashString", None) or getattr(torrent, "hash_string", None)
            )

        client = self._get_client(downloader)
        for limit_mbps, ids in ids_by_limit.items():
            valid_ids = [torrent_id for torrent_id in ids if torrent_id]
            if not valid_ids:
                continue

            if limit_mbps > 999:
                client.change_torrent(ids=valid_ids, upload_limited=False)
            else:
                client.change_torrent(
                    ids=valid_ids,
                    upload_limit=max(0, limit_mbps) * 1024,
                    upload_limited=True,
                )
            limited += len(valid_ids)

        return matched, limited

    def _match_site_rule(self, torrent, domain_rule_map):
        trackers = []

        tracker_attr = getattr(torrent, "trackers", None)
        if isinstance(torrent, dict):
            tracker_attr = torrent.get("trackers")
        if tracker_attr:
            try:
                for tracker in tracker_attr:
                    tracker_url = None
                    if isinstance(tracker, dict):
                        tracker_url = tracker.get("url") or tracker.get("announce")
                    else:
                        tracker_url = getattr(tracker, "url", None) or getattr(tracker, "announce", None)
                    if tracker_url:
                        trackers.append(tracker_url)
            except Exception:
                pass

        single_tracker = getattr(torrent, "tracker", None)
        if isinstance(torrent, dict):
            single_tracker = torrent.get("tracker")
        if single_tracker:
            trackers.append(single_tracker)

        for tracker_url in trackers:
            hostname = _parse_hostname_from_url(tracker_url)
            if not hostname:
                continue
            rule = domain_rule_map.get(_extract_core_domain(hostname))
            if rule:
                return rule

        comment = getattr(torrent, "comment", None)
        if isinstance(torrent, dict):
            comment = torrent.get("comment")
        comment_url = _extract_url_from_comment(comment)
        if comment_url:
            hostname = _parse_hostname_from_url(comment_url)
            if hostname:
                rule = domain_rule_map.get(_extract_core_domain(hostname))
                if rule:
                    return rule

        return None

    @staticmethod
    def _safe_float(val):
        try:
            return float(val)
        except Exception:
            return 0.0

    @staticmethod
    def _convert_qb_limit(limit_mbps):
        limit_int = int(limit_mbps)
        if limit_int > 999:
            return -1
        return max(0, limit_int) * 1024 * 1024


def start_ratio_speed_limiter(db_manager, config_manager):
    """初始化并启动全局 RatioSpeedLimiter 线程实例。"""
    global ratio_speed_limiter_thread
    import os

    debug_enabled = os.getenv("FLASK_DEBUG", "false").lower() == "true"
    if debug_enabled and os.environ.get("WERKZEUG_RUN_MAIN") != "true":
        logging.info("检测到调试监控进程，跳过RatioSpeedLimiter线程启动。")
        return ratio_speed_limiter_thread

    if ratio_speed_limiter_thread is None or not ratio_speed_limiter_thread.is_alive():
        ratio_speed_limiter_thread = RatioSpeedLimiter(db_manager, config_manager)
        ratio_speed_limiter_thread.start()
        logging.info("已创建并启动新的 RatioSpeedLimiter 实例。")
    return ratio_speed_limiter_thread


def stop_ratio_speed_limiter():
    """停止并清理当前的 RatioSpeedLimiter 线程实例。"""
    global ratio_speed_limiter_thread
    if ratio_speed_limiter_thread and ratio_speed_limiter_thread.is_alive():
        ratio_speed_limiter_thread.stop()
        ratio_speed_limiter_thread.join(timeout=2)
        if ratio_speed_limiter_thread.is_alive():
            logging.warning("RatioSpeedLimiter 线程仍在运行，但将强制清理引用")
        else:
            logging.info("RatioSpeedLimiter 线程已优雅停止。")
    ratio_speed_limiter_thread = None


def restart_ratio_speed_limiter(db_manager, config_manager):
    """重启 RatioSpeedLimiter 线程，用于配置变更后重新初始化。"""
    logging.info("正在重启 RatioSpeedLimiter 线程...")
    print("[RatioSpeedLimiter] 正在重启线程...")
    stop_ratio_speed_limiter()
    return start_ratio_speed_limiter(db_manager, config_manager)
