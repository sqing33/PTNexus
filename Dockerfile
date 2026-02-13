# 阶段 1: 构建 Vue 前端（Go 版本）
FROM node:20-alpine AS webui-go-builder

WORKDIR /app/webui-go

RUN npm install -g bun

COPY ./webui-go/package.json ./webui-go/bun.lock ./
RUN bun install --frozen-lockfile

COPY ./webui-go ./
RUN bun run build


# 阶段 2: 构建 Go 更新服务
FROM golang:1.23-bookworm AS updater-builder

WORKDIR /src/updater

COPY ./updater/go.mod ./
RUN go mod download

COPY ./updater/updater.go ./
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/updater updater.go


# 阶段 3: 构建 Go 后端（server-go）
# 注意：server-go 使用 sqlite（mattn/go-sqlite3），需要开启 CGO。
FROM golang:1.23-bookworm AS server-go-builder

WORKDIR /src/server-go

RUN apt-get update && \
    apt-get install -y --no-install-recommends gcc g++ libc6-dev && \
    rm -rf /var/lib/apt/lists/*

COPY ./server-go/go.mod ./server-go/go.sum ./
RUN go mod download

COPY ./server-go/cmd ./cmd
COPY ./server-go/internal ./internal

RUN CGO_ENABLED=1 GOOS=linux go build -ldflags="-s -w" -o /out/server-go ./cmd/server


# 阶段 4: 最终运行环境（与 Python 原版镜像一致：updater 作为入口，反代到 server-go）
FROM python:3.12-slim

WORKDIR /app

# 确保容器内对 localhost 和 127.0.0.1 的请求直接连接，不通过代理
ENV no_proxy="localhost,127.0.0.1,::1"
ENV NO_PROXY="localhost,127.0.0.1,::1"

# 禁用 updater 的定时自动更新（Go 版容器避免被 Python 版 mappings 覆盖）
ENV SCHEDULE_ENABLED="false"

# server-go 运行目录与数据目录（对齐 updater/batch 使用的 /app/data）
ENV PTNEXUS_BASE_DIR="/app/server-go"
ENV PTNEXUS_DATA_DIR="/app/data"

RUN apt-get update && \
    apt-get install -y --no-install-recommends \
    ca-certificates \
    ffmpeg \
    mpv \
    mediainfo \
    fonts-noto-cjk \
    supervisor \
    && apt-get clean \
    && rm -rf /var/lib/apt/lists/*

# --- Go 版后端文件 ---
RUN mkdir -p /app/server-go
COPY --from=server-go-builder /out/server-go /app/server-go/server-go
RUN chmod +x /app/server-go/server-go

# 配置与站点数据（server-go 默认从 baseDir 下读取）
COPY ./server-go/configs /app/server-go/configs
COPY ./server-go/sites_data.json /app/server-go/sites_data.json

# --- Go 版前端产物：放入 server-go 的静态目录 ---
COPY --from=webui-go-builder /app/webui-go/dist /app/server-go/dist

# --- BDInfo（仅复制 Linux 工具，避免打入 Windows 版本）---
COPY ./server-go/bdinfo/linux/ /app/bdinfo/
RUN chmod +x /app/bdinfo/BDInfo /app/bdinfo/BDInfoDataSubstractor

# --- updater ---
COPY --from=updater-builder /out/updater /app/updater
RUN chmod +x /app/updater

# 复制版本文件（updater 默认读取 /app/CHANGELOG.json）
COPY ./CHANGELOG.json /app/CHANGELOG.json

# Supervisor + 启动脚本（Go 版）
COPY ./supervisord-go.conf /app/supervisord.conf
COPY ./start-services-go.sh /app/start-services.sh
RUN chmod +x /app/start-services.sh

# 创建数据目录，用于持久化存储（对齐原版镜像路径）
RUN mkdir -p /app/data /app/data/tmp
VOLUME /app/data

# 对外只暴露 updater 端口（入口反代）
EXPOSE 5274

CMD ["./start-services.sh"]
