import os

bind = "0.0.0.0:5275"
worker_class = "gthread"
workers = int(os.getenv("PTNEXUS_GUNICORN_WORKERS", "1"))
threads = int(os.getenv("PTNEXUS_GUNICORN_THREADS", "12"))
timeout = int(os.getenv("PTNEXUS_GUNICORN_TIMEOUT", "180"))
graceful_timeout = 30
keepalive = 5
max_requests = int(os.getenv("PTNEXUS_GUNICORN_MAX_REQUESTS", "2000"))
max_requests_jitter = int(os.getenv("PTNEXUS_GUNICORN_MAX_REQUESTS_JITTER", "200"))
worker_tmp_dir = "/dev/shm"
accesslog = "-"
errorlog = "-"
loglevel = "info"
capture_output = True
