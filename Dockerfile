# 阶段 1: 构建 Vue 前端（Go 版本）
FROM node:20-alpine AS webui-builder

WORKDIR /app/webui

RUN corepack enable

COPY ./webui/package.json ./
RUN pnpm install --no-frozen-lockfile

COPY ./webui ./
RUN pnpm run build


# 阶段 2: 构建 Go 更新服务
FROM golang:1.23-bookworm AS updater-builder

WORKDIR /src/updater

COPY ./updater/go.mod ./
RUN go mod download

COPY ./updater/*.go ./
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/updater .


# 阶段 3: 构建 Go 后端（server）
# 注意：server 使用 sqlite（mattn/go-sqlite3），需要开启 CGO。
FROM golang:1.23-bookworm AS server-builder

WORKDIR /src/server

RUN apt-get update && \
    apt-get install -y --no-install-recommends gcc g++ libc6-dev && \
    rm -rf /var/lib/apt/lists/*

COPY ./server/go.mod ./server/go.sum ./
RUN go mod download

COPY ./server/cmd ./cmd
COPY ./server/internal ./internal

RUN CGO_ENABLED=1 GOOS=linux go build -ldflags="-s -w" -o /out/server ./cmd/server


# 阶段 4: 最终运行环境（与 Python 原版镜像一致：updater 作为入口，反代到 server）
FROM python:3.12-slim

WORKDIR /app

# 确保容器内对 localhost 和 127.0.0.1 的请求直接连接，不通过代理
ENV no_proxy="localhost,127.0.0.1,::1"
ENV NO_PROXY="localhost,127.0.0.1,::1"

# 禁用 updater 的定时自动更新（Go 版容器避免被 Python 版 mappings 覆盖）
ENV SCHEDULE_ENABLED="false"

# server 运行目录与数据目录（对齐 updater/batch 使用的 /app/data）
ENV PTNEXUS_BASE_DIR="/app/server"
ENV PTNEXUS_DATA_DIR="/app/data"
ENV PTNEXUS_BDINFO_DIR="/app/bdinfo/linux"

RUN apt-get update && \
    apt-get install -y --no-install-recommends \
    ca-certificates \
    ffmpeg \
    mpv \
    mediainfo \
    util-linux \
    fonts-noto-cjk \
    supervisor \
    && apt-get clean \
    && rm -rf /var/lib/apt/lists/*

# --- Go 版后端文件 ---
RUN mkdir -p /app/server
COPY --from=server-builder /out/server /app/server/server
RUN chmod +x /app/server/server

# 配置与站点数据（server 默认从 baseDir 下读取）
COPY ./server/configs /app/server/configs
COPY ./server/sites_data.json /app/server/sites_data.json

# --- Go 版前端产物：放入 server 的静态目录 ---
COPY --from=webui-builder /app/webui/dist /app/server/dist

# --- BDInfo（仅复制 Linux 工具，避免打入 Windows 版本）---
RUN mkdir -p /app/bdinfo/linux
COPY ./server/bdinfo/linux/ /app/bdinfo/linux/
RUN chmod +x /app/bdinfo/linux/BDInfo /app/bdinfo/linux/BDInfoDataSubstractor

# --- updater ---
COPY --from=updater-builder /out/updater /app/updater
RUN chmod +x /app/updater

# 复制版本文件（updater 默认读取 /app/CHANGELOG.json）
COPY ./CHANGELOG.json /app/CHANGELOG.json

# Supervisor + 启动脚本（Go 版）
COPY ./supervisord.conf /app/supervisord.conf
COPY ./start-services.sh /app/start-services.sh
RUN chmod +x /app/start-services.sh

# 创建数据目录，用于持久化存储（对齐原版镜像路径）
RUN mkdir -p /app/data /app/data/tmp
VOLUME /app/data

# 对外只暴露 updater 端口（入口反代）
EXPOSE 5274

CMD ["./start-services.sh"]
