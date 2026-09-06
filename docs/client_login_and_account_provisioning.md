<!-- docs/client_login_and_account_provisioning.md -->
# ReatiWire 客户端登录配置与服务端账号凭据分发规范

## 1. 架构拓扑与三面解耦信任模型

ReatiWire 面向 20 人规模研发团队的私有协同通信与内网服务直连场景，采用控制面、数据面、媒体面三面解耦的零信任分层接入架构：

```text
+-----------------------------------------------------------------------------------+
|                                 公网控制与中继节点 (VPS)                             |
|  - Headscale 控制端 (8018/TCP 或 443/TLS): 负责节点注册、密钥认证、网络拓扑与 ACL 分发 |
|  - DERP 快速中继服务 (443/TCP & UDP): 3000ms 打洞超时后的兜底转发通道                 |
|  - LiveKit SFU (7880/TCP, 50000-50050/UDP): 多人音视频与屏幕共享媒体转发单元          |
+-----------------------------------------------------------------------------------+
                                          ^
                                          | 控制面与鉴权注册 (TLS / 短期 JWT)
                                          v
+-----------------------------------------------------------------------------------+
|                           客户端进程 (ReatiWire Client)                           |
|                                                                                   |
|  +---------------------------+            +------------------------------------+  |
|  | 前端表现层 (frontend)     |            | 用户态协议栈核心 (tsnet & proxy)   |  |
|  | - 独立 GUI 控制台 (Wails) | <--(内存)-- | - WireGuard 虚拟 IP (100.64.0.x)   |  |
|  | - 节点状态/配置只读自查   |            | - 本地 SOCKS5 代理 (127.0.0.1:1055)|  |
|  | - 成员拓扑与目标开发机渲染|            | - 3000ms 硬超时打洞与熔断状态机    |  |
|  +---------------------------+            +------------------------------------+  |
+-----------------------------------------------------------------------------------+
```

### 1.1 三面鉴权凭据职责与持有边界

各网络平面的鉴权凭据设计有独立的生命周期与信任边界，防止单点凭据外溢造成全网越权：

| 协议平面 | 负责组件 | 客户端持有凭据 | 服务端分配与校验机制 | 物理隔离边界 |
| :--- | :--- | :--- | :--- | :--- |
| **控制面 (Control Plane)** | Headscale 控制端 | `ControlURL` + `AuthKey` (一次性 Pre-Auth Key) | 基于用户名 (`User`) 验证预授权密钥，动态分配 Tailnet 虚拟 IP (`100.64.0.x`) 并下发全网节点拓扑 | 密钥初次握手纳管后即刻失效，节点以私钥公钥对续期会话 |
| **数据面 (Data Plane)** | 用户态 `tsnet` WireGuard 引擎 | 节点私钥 (内存生成) + 节点公钥 (上报控制端) | 节点间基于 WireGuard 公钥对与 STUN 端点直连；受 [deploy/headscale/acl.hujson](file:///d:/offices/Github/ReatiWire/deploy/headscale/acl.hujson) 强访问控制策略约束 | 私钥驻留客户端进程内存，不落地明文文件 |
| **媒体面 (Media Plane)** | LiveKit SFU 转发单元 | 临时会议 JWT Token (携带 VideoGrant 载荷) | 服务端依据节点身份动态签发短期 Token (默认 4~6 小时有效期)；`api_key` 与 `api_secret` 物理留存于公网服务端，严禁分发至端侧 | 客户端零私钥持有，阻断根密钥逆向提取风险 |

---

## 2. 客户端服务登录地址与参数配置机制

### 2.1 前端表现层 (`frontend`) 数据绑定与自查设计

前端静态资源由 Wails 原生桌面内核嵌入托管（零外部 Web 端口暴露，杜绝 Localhost CSRF 风险），前端不持有公网控制端的长效特权密钥，仅通过进程内内存直接路由与后端网络核心交互：

1. **网络状态指示横栏 (System Ribbon)**：
   - 虚拟 IP：通过 `#val-virtual-ip` 呈现（初始值 `100.64.0.5`）。
   - 用户态协议栈状态：通过 `#val-tsnet-state` 显示连通状态。
   - 本地 SOCKS5 网关监听：通过 `#val-socks5-state` 实时指示（如 `运行中 (127.0.0.1:1055)`）。
   - 打洞熔断基线：常驻硬超时阈值（`T_timeout ≤ 3000ms`）。

2. **用户配置自查面板 (Config Inspection)**：
   - 客户端自查字段：呈现 `hostname`、`state_dir`、`ephemeral`、`socks5_listen` 以及 GUI 内存桥接状态。
   - 预授权凭证防护：`auth_key` 默认进行局部掩码遮蔽（`hs_key_••••••••••••••••••••••••`），支持按需一键切换显隐。
   - 服务端接入自查：仅展示 `control_url` 与 `livekit_url`，杜绝任何密钥凭据。
   - 配置 JSON 导出：支持一键完整复制脱敏后的运行配置结构体，便于排障审计。

3. **只读自查防线 (Read-Only Policy)**：
   前端面板提供完整的网络配置审计功能，支持掩码显示、JSON 复制与状态重新拉取，但不提供表单修改与在线保存功能，防止因前端随意变更导致控制面断联或网段漂移。

### 2.2 客户端配置服务登录地址的实现方式

#### 方式 A：配置文件驱动（推荐生产与静默启动环境）

在客户端根目录或用户配置目录中部署 [config.json](file:///d:/offices/Github/ReatiWire/config.json)：

```json
// config.json
{
  "server": {
    "control_url": "https://headscale.reati-wire.local:8018",
    "livekit_url": "https://sfu.reati-wire.local:7880"
  },
  "client": {
    "hostname": "reati-dev-zhangsan",
    "auth_key": "hs_key_9f83a04bc61244e8bc1a4c49d8e3b5e1",
    "state_dir": "./data/tsnet_state",
    "ephemeral": false,
    "socks5_listen": "127.0.0.1:1055",
    "web_listen": "内置内存桥接 (零端口暴露)"
  }
}
```

> **安全架构规范**：
> `api_key` 与 `api_secret` 为 LiveKit SFU 媒体服务根私钥，**全面从客户端配置文件中移除**。客户端仅配置 `control_url` 与 `livekit_url` 服务端点，会议凭证统一通过服务端单向签发的短期 JWT 临时令牌进行接入，杜绝端侧私钥泄露。

#### 方式 B：环境变量重载（适用于容器化或自动化脚本）

在拉起二进制程序前通过环境变量覆盖默认参数：

```bash
# 环境变量配置声明
export REATI_CONTROL_URL="https://headscale.reati-wire.local:8018"
export REATI_AUTH_KEY="hs_key_9f83a04bc61244e8bc1a4c49d8e3b5e1"
export REATI_HOSTNAME="reati-dev-zhangsan"
export REATI_SOCKS5_PORT="1055"
# 表现层通过进程内内存直接桥接，宿主机无外部控制面 WEB 端口暴露
```

#### 方式 C：前端交互式引导接入

针对首次启动未检测到有效 `auth_key` 或 `control_url` 的客户端，前端通过交互式引导输入必要连接参数，并提交至后端网络核心：

```text
[ 客户端引导配置面板 ]
--------------------------------------------------------------
* Headscale 控制端地址 : [ https://headscale.example.com:8018 ]
* 客户端节点标识 (Node): [ dev-pc-zhangsan                   ]
* 节点预授权凭证 (Key) : [ hs_key_************************** ]
* LiveKit 媒体接入点   : [ https://sfu.example.com:7880       ]
--------------------------------------------------------------
[ 保存并连接网络 ] -> 触发 Go 端 NodeManager 重载
```

---

## 3. Headscale 服务端账号与角色权限分发

ReatiWire 服务端基于 Headscale 实现命名空间划分，配合 [deploy/headscale/acl.hujson](file:///d:/offices/Github/ReatiWire/deploy/headscale/acl.hujson) 实施基于标签与角色的细粒度访问控制。

### 3.1 账号命名空间规范 (Username Conventions)

Headscale 自 v0.22 起以用户 (`User`) 作为唯一的网络命名空间与身份主体。ReatiWire 统一采用规范化邮箱格式或角色短前缀格式进行命名：

| 账号类型 | 命名范式 | 示例 | 归属权限组 | 对应业务实体 |
| :--- | :--- | :--- | :--- | :--- |
| **网络超级管理员** | `admin@{domain}` | `admin@team-collab.local` | `group:admin` | 基础设施及架构负责人，具备分配 `tag:server` 与 `tag:dev` 的权限 |
| **研发工程师** | `{pinyin/en}.dev@{domain}` 或 `{name}-dev` | `zhangsan.dev@team-collab.local` | `group:devs` | 团队研发人员，设备打上 `tag:dev` 标签，允许穿透访问开发机敏感端口 |
| **协同普通成员** | `{pinyin/en}@{domain}` 或 `{name}` | `wangwu@team-collab.local` | `group:members` | 产品、UI、测试与运营，仅具备成员间 P2P IM、文件直传及在线会议权限 |
| **受保护服务器资产** | `srv-{service}` | `srv-dev-linux`, `srv-qa-db` | `tag:server` | 部署在机房或内网的目标开发机，仅对持 `tag:dev` 凭证节点开放端口 |

### 3.2 角色与 ACL 访问控制矩阵

在 [deploy/headscale/acl.hujson](file:///d:/offices/Github/ReatiWire/deploy/headscale/acl.hujson) 中的安全策略定义如下：

```json
// deploy/headscale/acl.hujson
{
  "tagOwners": {
    "tag:server": ["group:admin"],
    "tag:dev": ["group:admin"]
  },
  "groups": {
    "group:admin": ["admin@team-collab.local"],
    "group:devs": ["engineer@team-collab.local"],
    "group:members": ["member@team-collab.local"]
  },
  "acls": [
    // 1. 全体团队成员内部虚拟网段互通 (100.64.0.0/10)
    {
      "action": "accept",
      "src": ["100.64.0.0/10"],
      "dst": ["100.64.0.0/10:*"]
    },
    // 2. 研发人员 (tag:dev) 直连开发服务器资产 (tag:server) 敏感端口
    {
      "action": "accept",
      "src": ["tag:dev"],
      "dst": [
        "tag:server:22",   // OpenSSH / VS Code Remote
        "tag:server:3306", // MySQL 数据库
        "tag:server:8080", // 内部微服务与 API
        "tag:server:6379"  // 开发 Redis 缓存
      ]
    }
  ]
}
```

### 3.3 服务端分配账号与密钥操作流程 (CLI 操作手册)

服务端管理员在部署了 Headscale 的公网 VPS 上执行标准化流程：

#### 步骤 1：创建团队成员账号 (User)

```bash
# 创建研发工程师账号
headscale users create zhangsan.dev@team-collab.local

# 创建普通协同成员账号
headscale users create wangwu@team-collab.local

# 查看当前已存在的全部用户
headscale users list
```

#### 步骤 2：为成员节点签发预授权密钥 (Pre-Auth Key)

每个客户端初次加入网络需持有服务端签发的单次有效预授权密钥：

```bash
# 1. 针对研发人员 (注入 tag:dev 标签，使其获得开发服务器访问权限)
headscale preauthkeys create \
  --user zhangsan.dev@team-collab.local \
  --reusable=false \
  --expiration 24h \
  --tags tag:dev

# 控制台将输出类似字符串凭据：
# hs_key_a8d79f041b3a88c7e4912903feadbc10

# 2. 针对普通成员 (不赋予 tag:dev，仅作为普通协同成员接入)
headscale preauthkeys create \
  --user wangwu@team-collab.local \
  --reusable=false \
  --expiration 24h

# 3. 针对受保护目标开发服务器 (绑定 tag:server 资产标签)
headscale preauthkeys create \
  --user admin@team-collab.local \
  --reusable=false \
  --expiration 720h \
  --tags tag:server
```

> **密钥管理规则**：
> 1. `--reusable=false`：严格采用单次认证密钥，客户端设备注册成功后密钥作废，阻断未授权设备二次复用。
> 2. `--expiration 24h`：密钥生命周期不超过 24 小时，超时未激活自动失效。
> 3. 研发标签显式指定 `--tags tag:dev`，否则 ACL 将拒绝该客户端经由 SOCKS5 代理直连目标服务器。

#### 步骤 3：分配与绑定虚拟 IP (Tailnet IPAM)

Headscale 在网段 `100.64.0.0/10` 自动按序分配虚拟 IP：

| IP 地址规划 | 分配对象 | 标识/主机名示例 | 访问能力说明 |
| :--- | :--- | :--- | :--- |
| `100.64.0.1` | Headscale & DERP 中继网关 | `gw-derp-cqq-901` | 集中控制与 STUN 打洞探测中继 |
| `100.64.0.2 ~ 100.64.0.9` | 团队研发工程师客户端 | `dev-zhang`, `dev-li` | 具备全网互通 + 开发机特权直连 |
| `100.64.0.10` | 核心研发测试服务器 | `dev-linux-primary` | 暴露端口 22, 3306, 8080，限 `tag:dev` 访问 |
| `100.64.0.11` | 测试集群数据库节点 | `qa-database-instance`| 暴露端口 5432, 6379，限 `tag:dev` 访问 |
| `100.64.0.12 ~ 100.64.0.25`| 普通团队成员与协作工位 | `collab-wang`, `collab-zhao`| 具备 1v1 IM、文件直传与在线会议权限 |

---

## 4. LiveKit 媒体服务凭据 (API Key / Secret) 初始化与生命周期管理

LiveKit SFU 负责承载团队 20 人的低延迟音频、视频与屏幕共享。与 Headscale 基于预授权密钥的认证体系不同，LiveKit 依赖对称密钥签名机制完成入会控制。

### 4.1 密钥定义与初始化源头

`api_key` 与 `api_secret` 的权威源头建立在服务端容器编排环境中，不在客户端存储或配置。

1. **配置文件静态定义**：
   在 [deploy/livekit.yaml](file:///d:/offices/Github/ReatiWire/deploy/livekit.yaml) 中进行声明：
   ```yaml
   # deploy/livekit.yaml
   port: 7880
   bind_addresses:
     - ""
   rtc:
     tcp_port: 7881
     port_range_start: 50000
     port_range_end: 50050
     use_external_ip: true

   # 权威密钥定义 (格式: <api_key>: <api_secret>)
   keys:
     reati_wire_api_key: reati_wire_api_secret_32bytes_min
   ```
2. **容器环境变量注入 (推荐生产实践)**：
   在 [deploy/docker-compose.yml](file:///d:/offices/Github/ReatiWire/deploy/docker-compose.yml) 中以环境变量格式注入，避免明文硬编码于静态仓库：
   ```yaml
   # deploy/docker-compose.yml
   services:
     livekit:
       image: livekit/livekit-server:v1.6.0
       environment:
         - LIVEKIT_KEYS=${LIVEKIT_API_KEY}:${LIVEKIT_API_SECRET}
         - REDIS_ADDRESS=redis:6379
         - REDIS_PASSWORD=${REDIS_SECRET}
   ```
3. **密钥复杂度与生成规范**：
   - `api_key`：团队内全局唯一服务标识符，由字母、数字及下划线组成（如 `reati_wire_api_key`）。
   - `api_secret`：HMAC-SHA256 签名私钥，长度不少于 32 字节。生产部署前可通过系统随机熵源生成：
     ```bash
     # 生成 32 字节高熵十六进制私钥
     openssl rand -hex 32
     ```

### 4.2 api_secret 隔离于服务端的安全动因与攻防边界

在 LiveKit 鉴权模型中，持有 `api_secret` 的实体被赋予以下不可降级权限：
- 任意签名有效的 JWT Token；
- 伪造任意身份（包括系统管理员或虚构用户）；
- 加入、监听或关闭任意会议室；
- 踢出成员或篡改推流权限。

若将 `api_secret` 写入客户端的 `config.json` 或前端打包产物中：
- 客户端设备遭受逆向破解或本地文件读取时，全网音视频会议权限即宣告失守；
- 浏览器拉取静态配置会导致公网直接暴露根私钥。

因此，**客户端严禁配置或持久化 `api_secret`**。媒体面鉴权严格执行“服务端单向签发、客户端仅持凭据”的单向传递模式。

### 4.3 会议 Token 签发流程与权限映射流

会议接入凭证的签发流转机制如下：

```text
[ 客户端节点请求 ]
  节点 IP: 100.64.0.2 / 主机名: dev-zhangsan
  动作: 发起加入房间 "arch-review" 请求
             |
             | GET /api/meeting/token?room=arch-review&name=张工
             v
[ 认证签发中介 (服务端/受信网络核心) ]
  1. 校验请求来源节点是否属于有效 Tailnet (100.64.0.0/10)
  2. 读取服务端持有的 api_secret 执行 HMAC-SHA256 签名
  3. 组装短期 JWT Payload:
     - iss (API Key)    : "reati_wire_api_key"
     - sub (Identity)   : "dev-zhangsan"
     - exp (过期时间)   : 当前时间 + 4小时
     - video (权限声明) : { roomJoin: true, room: "arch-review", canPublish: true, canSubscribe: true }
     - metadata (策略)  : { "dynacast": true, "simulcast": true, "max_bandwidth": "85mbps" }
             |
             | 返回签发后的 JWT 字符串
             v
[ 客户端前端 WebRTC 引擎 ]
  携带 JWT 直接向 LiveKit SFU (7880/TCP) 发起 WebSocket 握手进入会议
```

### 4.4 密钥轮转与运维应对方案 (Key Rotation)

当 `api_secret` 面临泄露风险或达到团队安全合规轮转周期时，执行平滑无感轮转：

1. **多密钥过渡阶段 (Multi-Key Ingestion)**：
   在 [deploy/livekit.yaml](file:///d:/offices/Github/ReatiWire/deploy/livekit.yaml) 中同时配置旧密钥与新密钥：
   ```yaml
   keys:
     reati_wire_api_key_v2: new_generated_secret_32bytes_min   # 新密钥用于签名
     reati_wire_api_key: reati_wire_api_secret_32bytes_min       # 旧密钥保留供现有会话平滑退出
   ```
2. **重启 SFU 容器与切换签名端点**：
   重启 LiveKit 容器加载配置。将服务端 JWT 签发服务的环境变量更新为 `reati_wire_api_key_v2`。由于客户端不保存密钥，仅需重新获取一次会议 Token 即可无缝切换至新密钥。
3. **废弃旧密钥 (Decommission)**：
   观察 6 小时（覆盖最大 Token 有效期）后，自 `livekit.yaml` 移除旧 Key 并重载，完成凭证轮转。

---

## 5. 快速核验与上线排查清单 (Troubleshooting Runbook)

| 检查项 | 验证方式 | 预期正常结果 | 故障排查手段 |
| :--- | :--- | :--- | :--- |
| **控制端连通性** | `curl -k https://<CONTROL_URL>/health` | 返回 HTTP 200 或 Headscale 探活报文 | 检查 VPS 443/8018 防火墙及域名解析配置 |
| **客户端密钥注册** | 启动客户端，查看终端输出 | 输出 `[+] 用户态 WireGuard 虚拟 IP: 100.64.0.x` | 确认 `auth_key` 未过期且未被二次复用 |
| **前端自查面板** | 在客户端原生窗体中点击右上角“配置自查” | 虚拟 IP 正确呈现，GUI 显示“内置内存桥接”，媒体服务显示“服务端物理隔离” | 检查 `/api/config` 内存接口及客户端本地 `config.json` 是否存在 |
| **P2P 打洞探测** | 点击 IM 面板“3s 竞速探测” | 徽标切换为 `DIRECT P2P (x ms)` 或 `DERP RELAY` | 检查双方 NAT 拓扑与 VPS 3478/UDP (STUN) 放行情况 |
| **开发机权限隔离** | 通过 `127.0.0.1:1055` 代理探测 `100.64.0.10:22` | `tag:dev` 节点握手成功；普通成员连接被 RST | 检查 Headscale 用户的 Pre-Auth Key 是否正确携带 `tag:dev` 标签 |
| **在线会议入会认证** | 调用 `GET /api/meeting/token` | 返回状态码 200 及合法的 JWT 字符串 | 检查 LiveKit SFU 7880 端口可达性及服务端签名配置 |
