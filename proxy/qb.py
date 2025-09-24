# simple_proxy_test.py - 简化版代理测试脚本
import requests
import json
import time
from datetime import datetime

# --- 配置部分 ---
PROXY_BASE_URL = "http://152.53.189.117:9090"

downloader_configs = [
    {
        "id": "a1b2c3d4-e5f6-7890-1234-567890abcdef",
        "type": "qbittorrent",
        "host": "http://127.0.0.1:8080",
        "username": "admin",
        "password": "qaqaa123.....",
    }
]

def test_complete_data():
    """获取完整的种子信息并保存到JSON文件"""
    print("=" * 60)
    print("qBittorrent 代理完整数据测试")
    print("=" * 60)
    print(f"测试时间: {datetime.now().strftime('%Y-%m-%d %H:%M:%S')}")
    print(f"代理地址: {PROXY_BASE_URL}")

    # 测试健康检查
    print("\n1. 健康检查...")
    try:
        response = requests.get(f"{PROXY_BASE_URL}/api/health", timeout=10)
        response.raise_for_status()
        health_data = response.json()
        print(f"   ✅ 代理状态: {health_data.get('status')}")
    except Exception as e:
        print(f"   ❌ 健康检查失败: {e}")
        return

    # 测试服务器统计
    print("\n2. 获取服务器统计信息...")
    try:
        start_time = time.time()
        response = requests.post(f"{PROXY_BASE_URL}/api/stats/server",
                               json=downloader_configs, timeout=30)
        response.raise_for_status()
        stats_data = response.json()

        with open("server_stats_complete.json", "w", encoding="utf-8") as f:
            json.dump(stats_data, f, ensure_ascii=False, indent=2)

        print(f"   ✅ 耗时: {time.time() - start_time:.2f}秒")
        print(f"   📄 统计信息已保存到: server_stats_complete.json")

    except Exception as e:
        print(f"   ❌ 统计信息获取失败: {e}")

    # 测试完整种子信息
    print("\n3. 获取完整种子信息（包含comment和trackers）...")
    try:
        start_time = time.time()

        request_data = {
            "downloaders": downloader_configs,
            "include_comment": True,
            "include_trackers": True
        }

        response = requests.post(f"{PROXY_BASE_URL}/api/torrents/all",
                               json=request_data, timeout=120)
        response.raise_for_status()
        torrents_data = response.json()

        # 保存完整数据
        with open("torrents_complete.json", "w", encoding="utf-8") as f:
            json.dump(torrents_data, f, ensure_ascii=False, indent=2)

        # 统计信息
        total_torrents = len(torrents_data)
        with_comment = sum(1 for t in torrents_data if t.get('comment'))
        with_trackers = sum(1 for t in torrents_data if t.get('trackers'))

        print(f"   ✅ 耗时: {time.time() - start_time:.2f}秒")
        print(f"   📄 完整种子数据已保存到: torrents_complete.json")
        print(f"   📊 数据统计:")
        print(f"      - 总种子数: {total_torrents}")
        print(f"      - 包含comment: {with_comment}")
        print(f"      - 包含trackers: {with_trackers}")

        # 显示前3个种子的详细信息
        print(f"\n   📋 前3个种子信息预览:")
        for i, torrent in enumerate(torrents_data[:3]):
            print(f"   {i+1}. {torrent.get('name', '')[:50]}...")
            print(f"      Hash: {torrent.get('hash', '')[:16]}...")
            print(f"      Comment: {'有' if torrent.get('comment') else '无'}")
            print(f"      Trackers: {len(torrent.get('trackers', []))}个")
            if torrent.get('trackers'):
                print(f"      主Tracker: {torrent['trackers'][0].get('url', '')[:50]}...")

    except Exception as e:
        print(f"   ❌ 种子信息获取失败: {e}")

    print(f"\n测试完成时间: {datetime.now().strftime('%Y-%m-%d %H:%M:%S')}")
    print("=" * 60)

if __name__ == "__main__":
    test_complete_data()