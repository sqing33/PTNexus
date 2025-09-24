# remote_media_caller.py (这是一个示例文件，演示如何在 Python 中调用新的 Go 代理接口)

import requests
import json

# 假设你的 Go 代理运行在这台服务器上
PROXY_BASE_URL = "http://152.53.189.117:9090"  # 替换成你代理的实际地址


def get_remote_screenshots(remote_video_path: str):
    """
    通过 Go 代理获取远程视频的截图。
    
    :param remote_video_path: 视频文件在远程服务器上的绝对路径。
    :return: 包含截图 BBCode 的字符串，如果失败则返回错误信息。
    """
    api_url = f"{PROXY_BASE_URL}/api/media/screenshot"
    payload = {"remote_path": remote_video_path}

    print(f"向代理请求截图: {remote_video_path}")
    try:
        response = requests.post(api_url, json=payload,
                                 timeout=180)  # 截图和上传可能需要较长时间
        response.raise_for_status()

        data = response.json()
        if data.get("success"):
            print("代理成功返回截图 BBCode。")
            return data.get("bbcode", "")
        else:
            print(f"代理返回错误: {data.get('message')}")
            return f"错误: {data.get('message')}"

    except requests.exceptions.RequestException as e:
        print(f"调用代理时发生网络错误: {e}")
        return f"错误: 调用代理失败 - {e}"


def get_remote_mediainfo(remote_video_path: str):
    """
    通过 Go 代理获取远程视频的 MediaInfo。
    
    :param remote_video_path: 视频文件在远程服务器上的绝对路径。
    :return: MediaInfo 文本，如果失败则返回错误信息。
    """
    api_url = f"{PROXY_BASE_URL}/api/media/mediainfo"
    payload = {"remote_path": remote_video_path}

    print(f"向代理请求 MediaInfo: {remote_video_path}")
    try:
        response = requests.post(api_url, json=payload, timeout=60)
        response.raise_for_status()

        data = response.json()
        if data.get("success"):
            print("代理成功返回 MediaInfo。")
            return data.get("mediainfo_text", "")
        else:
            print(f"代理返回错误: {data.get('message')}")
            return f"错误: {data.get('message')}"

    except requests.exceptions.RequestException as e:
        print(f"调用代理时发生网络错误: {e}")
        return f"错误: 调用代理失败 - {e}"


# --- 使用示例 ---
if __name__ == "__main__":
    # 替换成你远程服务器上的一个实际视频文件路径
    REMOTE_FILE_PATH = "/home/admin/qbittorrent/Downloads/The.Apothecary.Diaries.S01.2023.1080p.BluRay.x265.10bit.FLAC.2.0-ADE"

    print("--- 测试截图功能 ---")
    screenshots_bbcode = get_remote_screenshots(REMOTE_FILE_PATH)
    print("\n返回的截图 BBCode:\n" + "=" * 20)
    print(screenshots_bbcode)
    print("=" * 20 + "\n")

    # print("--- 测试 MediaInfo 功能 ---")
    # mediainfo_text = get_remote_mediainfo(REMOTE_FILE_PATH)
    # print("\n返回的 MediaInfo:\n" + "=" * 20)
    # print(mediainfo_text)
    # print("=" * 20)
