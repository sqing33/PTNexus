import atexit
import logging
import os
import signal
import sys
import threading
import time

CURRENT_DIR = os.path.dirname(os.path.abspath(__file__))
if CURRENT_DIR not in sys.path:
    sys.path.insert(0, CURRENT_DIR)

from app import (
    initialize_db_manager,
    run_database_migrations,
    run_startup_maintenance,
    run_startup_refresh_task,
    start_background_services,
    stop_background_services,
    init_bdinfo_manager,
    cleanup_bdinfo_manager,
)

logging.basicConfig(
    level=logging.DEBUG, format="%(asctime)s - [PID:%(process)d] - %(levelname)s - %(message)s"
)

shutdown_event = threading.Event()


def _handle_signal(signum, _frame):
    logging.info(f"收到退出信号: {signum}，准备停止 background_runner...")
    shutdown_event.set()


def main():
    signal.signal(signal.SIGTERM, _handle_signal)
    signal.signal(signal.SIGINT, _handle_signal)

    atexit.register(cleanup_bdinfo_manager)
    atexit.register(stop_background_services)

    logging.info("background_runner 启动：准备执行后台维护与线程服务")
    run_database_migrations()

    db_manager = initialize_db_manager()
    run_startup_maintenance(db_manager)
    start_background_services(db_manager)
    run_startup_refresh_task(db_manager)
    init_bdinfo_manager()

    logging.info("background_runner 已进入守护循环")
    while not shutdown_event.is_set():
        time.sleep(1)

    logging.info("background_runner 准备退出")


if __name__ == "__main__":
    main()
