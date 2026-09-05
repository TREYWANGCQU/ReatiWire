# collaboration_tool_solution/team_collab/deploy/scripts/bootstrap.sh
#!/usr/bin/env bash
set -euo pipefail

echo "=== [ReatiWire] 20人轻量私有协同服务端初始化脚本 ==="

# 1. 检查必要依赖
command -v docker >/dev/null 2>&1 || { echo "错误: 请先安装 Docker"; exit 1; }
command -v docker compose >/dev/null 2>&1 || { echo "错误: 请先安装 Docker Compose v2"; exit 1; }

# 2. 配置主机防火墙端口放行 (UFW 规则)
if command -v ufw >/dev/null 2>&1; then
    echo "[+] 配置 UFW 端口放行规则..."
    ufw allow 7880/tcp comment "LiveKit SFU HTTP & Signaling"
    ufw allow 7881/tcp comment "LiveKit SFU WebRTC TCP Fallback"
    ufw allow 50000:50050/udp comment "LiveKit SFU WebRTC Media Pool"
    ufw allow 443/tcp comment "Headscale & DERP HTTPS"
    ufw allow 3478/udp comment "STUN NAT Discovery"
    ufw reload
    echo "[+] UFW 规则配置完成"
fi

# 3. 启动 LiveKit SFU 与 Redis 容器组
echo "[+] 启动 LiveKit 与 Redis 服务..."
cd "$(dirname "$0")/.."
docker compose up -d

echo "[+] 服务状态检查:"
docker compose ps

echo "=== 服务端组件初始化完毕 ==="
