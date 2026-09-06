<!-- docs/dual_mode_runtime_architecture_and_update_plan.md -->
# ReatiWire 客户端双轨运行时架构与更新方案 (Dual-Mode Runtime Architecture & Update Plan)

## 1. 概述与设计目标 (Objectives)

### 1.1 需求背景
经过对 ReatiWire 现存代码基线的实体验证，当前编译产物本质上属于**全流程架构设计与前端高保真交互验证的原型系统 (PoC / Interactive Prototype)**：
- 成员列表（张工、李工、王运营）、虚拟 IP（`100.64.0.5`）、时延及 P2P/DERP 状态均为硬编码内存数据；
- `pkg/tsnet` 未引入实际的 `tailscale.com/tsnet` 核心库，网络拨号走宿主机普通连接；
- `config.json` 仅作为静态文件供前端面板回显，未真正被网络层用于向控制端鉴权组网。

### 1.2 核心目标
本更新方案旨在设计并建立一套**双轨运行时自适应架构 (Dual-Mode Adaptive Runtime Architecture)**：
1. **零配置 / 无效配置时的无缝降级 (Simulation Mock Mode)**：当本地无 `config.json`、配置项不合法、或无法连通远端 Headscale 控制端时，客户端保持当前完整的高保真交互体验、离线仿真数据与三秒打洞熔断演示能力，保障单机离线开箱即用、方案汇报与视觉评审不受阻断。
2. **真实有效配置时的平滑升轨 (Real Production Mode)**：当检测到合法有效的 `config.json` 且握手成功后，系统自动激活真实的 `tsnet` 用户态 WireGuard 协议栈，向 Headscale 注册并动态拉取真实的 Tailnet 节点拓扑与在线状态，本地 SOCKS5 代理与端到端通信无缝切换为真实数据面通道。
3. **确定性边界与清晰影响面**：定义严格的模式裁决逻辑（Mode Arbiter）、统一接口抽象（Contract Interfaces）以及前后端全生命周期状态同步机制，消除对既有业务逻辑的破坏性侵入。

---

## 2. 约束条件与边界契约矩阵 (Constraints & Boundary Matrix)

```
+---------------------------------------------------------------------------------------------------+
|                                 模式裁决器 (Mode Arbiter) 评估矩阵                                 |
+---------------------------------------------------------------------------------------------------+
|   阶梯检查 (L0 -> L3)    |             通过 (PASS)              |            不通过 (FALLBACK)       |
+--------------------------+-------------------------------------+----------------------------------+
| L0: 配置文件存在性与解析   | config.json 文件存在且 JSON 语法合法   | 降级为 [仿真模式 (SIMULATION)]   |
+--------------------------+-------------------------------------+----------------------------------+
| L1: 凭据真实性有效校验     | 非预设占位符，AuthKey 格式匹配 hs_key  | 降级为 [仿真模式 (SIMULATION)]   |
+--------------------------+-------------------------------------+----------------------------------+
| L2: 控制端端点连通性探测   | ControlURL 域名可解析且 TLS 握手正常  | 告警并安全回退至 [仿真模式]      |
+--------------------------+-------------------------------------+----------------------------------+
| L3: tsnet 协议栈在线纳管   | Headscale 成功分配 100.64.0.x 虚拟IP  | 超时降级，上报错误至前端徽标     |
+---------------------------------------------------------------------------------------------------+
```

### 2.1 边界契约矩阵
| 交互平面 | 仿真模式 (Simulation Mode) 契约 | 生产模式 (Production Mode) 契约 | 强制物理/协议约束 |
| :--- | :--- | :--- | :--- |
| **配置接入 (Config)** | 缺省回退内置默认 Mock 结构体，无需网络 I/O | 读取工作目录或 APPDATA 路径下 `config.json` 并实施校验 | 凭据掩码存储；物理禁止明文外传除握手必要外的任何敏感字段 |
| **网络核心 (tsnet)** | `MockNodeManager`：内存虚拟 IP `100.64.0.5`，固定返回 4 节点 | `RealNodeManager`：驱动 `tailscale.com/tsnet.Server` 实例 | Windows 平台保持 CGO-free；零 Windows TUN 网卡驱动安装依赖 |
| **拓扑监听 (Peers)** | `MockPeerProvider`：预设张工、李工、王运营 3 人静态结构体 | `HeadscalePeerProvider`：轮询 `LocalClient.Status()` 提取真实 Peers | 统一抽象为 `[]PeerPresence` 结构体契约，前端协议层无感知透明切换 |
| **SOCKS5 代理 (Proxy)** | 监听 `127.0.0.1:1055`，请求转发使用宿主机标准拨号器或模拟测试 | 监听 `127.0.0.1:1055`，连接由 `tsnet.Server.DialContext` 接管 | 严格限定仅能转发进入 100.64.0.0/10 虚拟网段，阻断越权旁路穿透 |
| **即时通讯 (IM)** | 本地单机回环与离线消息记录 | 经由 WireGuard 虚拟 IP 直连端口或消息代理通信 | 保持统一消息报文体 `DirectMessage`，前端徽标根据实测时延更新 |

---

## 3. 架构设计与模块解耦 (Architecture)

### 3.1 双轨分层拓扑架构

```text
+-----------------------------------------------------------------------------------+
|                             前端表现层 (Wails Frontend)                            |
|  - 状态指示横栏: 增加 [运行模式: 仿真演示 | 生产联机] 状态微标                       |
|  - 统一 API: /api/status, /api/peers, /api/config (对底层运行模式完全透明)        |
+-----------------------------------------------------------------------------------+
                                          |
                                          v (IPC 桥接 / In-Memory Handler)
+-----------------------------------------------------------------------------------+
|                             后端调度控制器 (app.go)                               |
|  - App.Startup(): 运行 Mode Arbiter 裁决逻辑                                      |
|  - 维护接口抽象实例: INodeManager, IPeerProvider, IProxyBackend                     |
+-----------------------------------------------------------------------------------+
                                          |
                     +--------------------+--------------------+
                     |                                         |
                     v [分支 A: 无有效配置]                      v [分支 B: 有效配置]
       +-------------------------------+         +-------------------------------+
       |    仿真运行时 (Mock Runtime)   |         |    生产运行时 (Real Runtime)   |
       | - MockNodeManager             |         | - RealTsnetNodeManager        |
       | - StaticPeerProvider (3人固定)|         | - TailnetPeerProvider (动态)  |
       | - SimulatedCircuitBreaker     |         | - LiveCircuitBreaker          |
       | - LoopbackSOCKS5Proxy         |         | - WireGuardTunneledProxy      |
       +-------------------------------+         +-------------------------------+
```

### 3.2 接口抽象设计 (Interface Contracts)

为了保证既有代码架构稳定且不重写业务逻辑，将核心模块抽象为无状态或状态机接口：

```go
// pkg/tsnet/interfaces.go
type INodeManager interface {
    Start(ctx context.Context) error
    Stop() error
    DialContext(ctx context.Context, network, addr string) (net.Conn, error)
    Listen(network, addr string) (net.Listener, error)
    GetStatus() NodeStatus
    GetMode() RuntimeMode // ModeSimulation | ModeProduction
}

// pkg/im/interfaces.go
type IPeerProvider interface {
    GetPeers(ctx context.Context) ([]*PeerPresence, error)
    SubscribePresence(ctx context.Context) (<-chan []*PeerPresence, error)
}
```

---

## 4. 数据流模型 (Data Flow Modeling)

### 4.1 客户端启动与模式裁决时序 (Mode Arbitration Sequence)

```text
[客户端启动] 
     │
     ▼
[读取 config.json]
     ├── (文件缺失 / 格式损坏) ────────────────────────────────────────► [激活 ModeSimulation]
     │                                                                          │
     ▼                                                                          │
[校验字段真实性]                                                                │
     ├── (包含占位符 'hs_key_sample' / URL包含 '.local') ──────────────► [激活 ModeSimulation]
     │                                                                          │
     ▼                                                                          │
[L2: 探测 ControlURL TCP/TLS 连通性 (超时 1500ms)]                              │
     ├── (连通失败 / 网络不可达) ──────────────────────────────────────► [激活 ModeSimulation]
     │                                                                          │
     ▼                                                                          │
[L3: 启动 RealTsnetNodeManager (ts.Up 超时 4000ms)]                             │
     ├── (鉴权拒绝 / 握手超时) ────────────────────────────────────────► [激活 ModeSimulation]
     │                                                                          │
     ▼                                                                          ▼
[成功分配 100.64.0.x IP]                                                [挂载内存静态 3 人拓扑]
     │                                                                          │
     ▼                                                                          ▼
[激活 ModeProduction]                                                   [保持高保真交互演示]
     │                                                                          │
     └───────────────────────────────┬──────────────────────────────────────────┘
                                     │
                                     ▼
                      [前端展示对应模式徽标与拓扑数据]
```

### 4.2 拓扑数据流 (Peer Topology Flow)
- **仿真模式**：`MockPeerProvider` 直接返回预设的 `张工 (100.64.0.2)`、`李工 (100.64.0.3)`、`王运营 (100.64.0.4)`，时延与直连标志保持恒定或微弱抖动。
- **生产模式**：后台协程通过 `tsnet.Server.LocalClient().Status(ctx)` 监听全网拓扑，自动过滤自身节点，将在线的真实 Tailnet 节点映射为 `PeerPresence`，并通过 Wails 事件推送 (`runtime.EventsEmit`) 或 `/api/peers` 轮询刷新给前端。

---

## 5. 全部影响范围矩阵 (Comprehensive Scope of Impact)

### 5.1 后端工程与核心代码 (`main`, `app`, `pkg`)

| 模块 / 文件 | 变更性质 | 影响内容与改造细节 |
| :--- | :--- | :--- |
| `go.mod` / `go.sum` | **[MODIFY]** | 引入 `tailscale.com` 稳定版依赖（如 `v1.56.1` 或最新兼容版），确保不引入 CGO。 |
| `app.go` | **[MODIFY]** | 1. 引入模式判定逻辑 `determineRuntimeMode(config)`。<br>2. 将 `nodeMgr` 由结构体指针改为 `tsnet.INodeManager` 接口。<br>3. 在 `GetSystemOverview` 中补充 `runtime_mode` 字段。<br>4. 根据模式切换 `chatMgr` 的数据提供源。 |
| `pkg/tsnet/manager.go` | **[MODIFY]** | 提炼 `INodeManager` 接口，原有的模拟实现重命名或封装为 `MockNodeManager`。 |
| `pkg/tsnet/real_manager.go` | **[NEW]** | 实现 `RealTsnetNodeManager`，内部持有 `*tsnet.Server`，封装真正的用户态 WireGuard 注册、`DialContext`、`Listen` 与 `Status` 查询。 |
| `pkg/tsnet/arbiter.go` | **[NEW]** | 实现 `ConfigValidator` 与 `ModeArbiter`，负责四级阶梯检查（L0 语法 -> L1 占位符 -> L2 连通性 -> L3 握手）。 |
| `pkg/proxy/socks5.go` | **[MODIFY]** | 其内部 `dialer` 绑定到 `INodeManager.DialContext`。生产模式下真正直通 Tailnet 内部服务器。 |
| `pkg/im/chat.go` | **[MODIFY]** | 解耦静态节点初始化，提供 `SyncFromTailnet(peers []*PeerPresence)` 方法供动态更新。 |

### 5.2 前端表现层 (`frontend`)

| 文件 | 变更性质 | 影响内容与改造细节 |
| :--- | :--- | :--- |
| `frontend/index.html` | **[MODIFY]** | 在顶部状态栏 (`system-ribbon`) 新增运行模式指示徽标容器 `<div id="val-runtime-mode" class="ribbon-badge"></div>`。 |
| `frontend/src/index.css` | **[MODIFY]** | 新增仿真模式徽标样式（例如浅琥珀色 `badge-simulation`）与生产联机徽标样式（绿色呼吸灯 `badge-production`）。 |
| `frontend/src/main.js` | **[MODIFY]** | 1. 响应 `/api/status` 中的 `runtime_mode`，更新顶栏徽标与提示文案。<br>2. 调整 `renderPeerList`，确保支持动态节点数量（从 0 到数十个），消除只认 3 个固定人员的硬编码假设。<br>3. 在配置自查页增加当前模式说明：“当前运行在[仿真演示模式/生产联机模式]”。 |

### 5.3 配置文件与示例

| 文件 | 变更性质 | 影响内容与改造细节 |
| :--- | :--- | :--- |
| `config.json` | **[MODIFY]** | 规范默认字段，添加 `mode: "auto"` 策略声明；标注哪些为默认示例占位符。 |
| `config.example.json` | **[NEW]** | 提供生产上线配置模板，明确填写合法的公网 Headscale URL 与由控制端生成的 `hs_key_...`。 |
| `.gitignore` | **[MODIFY]** | 确认真实的 `data/tsnet_state` 状态凭据目录被忽略，防止生产私钥提交。 |

### 5.4 平台构建与交付物

| 项目 | 影响分析 |
| :--- | :--- |
| **Windows 平台二进制体积** | 引入 `tailscale.com` 核心库后，Go 静态二进制体积会有所增加（通常增大约 15MB~25MB）。使用 `-ldflags "-s -w -H windowsgui"` 保持体积压制。 |
| **CGO 与驱动依赖** | `tsnet` 为纯 Go 实现的用户态 WireGuard，**不依赖** Windows CGO，**不依赖** WinTun / TAP-Windows 驱动安装，普通用户权限即可运行，无 UAC 提权干扰。 |
| **构建速度** | 初次构建拉取 Tailscale 模块需耗费少量下载时间，后续增量编译无影响。 |

---

## 6. 工作分解结构 (Work Breakdown Structure - WBS)

```text
Phase 1: 契约与解耦 (Contracts & Interfaces)
  ├── 1.1 依赖引入: 在 go.mod 中引入 tailscale.com 稳定包并锁定
  ├── 1.2 接口抽象: 声明 tsnet.INodeManager 与 im.IPeerProvider
  └── 1.3 模式判定器: 编写 pkg/tsnet/arbiter.go 实现 L0~L2 校验规则

Phase 2: 双轨后端实现 (Backend Dual-Mode Core)
  ├── 2.1 仿真后端封装: 固化 MockNodeManager 作为兜底基线
  ├── 2.2 生产协议栈接入: 编写 RealTsnetNodeManager，对接 tsnet.Server
  ├── 2.3 动态拓扑适配: 编写 LocalClient Status 状态监听并转换为 PeerPresence
  └── 2.4 控制器调度: 改造 app.go 组装模式裁决与动态路由

Phase 3: 前端交互与视觉适配 (Frontend Visuals & State)
  ├── 3.1 模式指示徽标: 在顶栏实现 Simulation/Production 双色态展示
  ├── 3.2 动态列表适配: 消除前端固定 3 人依赖，实现真实节点动态挂载
  └── 3.3 配置面板反馈: 明确标注当前命中模式及回退原因（若触发降级）

Phase 4: 双模门禁验证 (Verification & Quality Gateways)
  ├── 4.1 离线/缺配置验证: 移除 config.json，确保原演示逻辑 100% 完整复原
  ├── 4.2 联机/真配置验证: 接入有效 Headscale 服务，验证虚拟 IP 注入与节点刷新
  └── 4.3 Windows GUI 二进制打包验证: 确认 wails 构建产物稳定执行
```

---

## 7. 验收指标 (Acceptance Criteria)

### 7.1 仿真模式验收标准 (Mode Simulation Gate)
1. **零配置启动**：在删掉或重命名 `config.json` 的情况下，启动 `reati_wire.exe` 正常运行，无 Panic、无崩溃弹窗。
2. **原型功能完备**：
   - 顶栏显示 `运行模式: 仿真演示 (SIMULATION)`。
   - 成员列表稳定呈现“张工”、“李工”、“王运营”，P2P/DERP 竞速探测按钮工作正常并返回模拟指标。
   - SOCKS5 代理可正常在界面开关；在线会议网格呈现演示卡片；文件传输演示进度条按原样执行。

### 7.2 生产模式验收标准 (Mode Production Gate)
1. **配置有效注入**：当工作目录放置真实可用的 `config.json` 时，应用启动自动命中生产模式，顶栏切换为 `运行模式: 生产联机 (PRODUCTION)`。
2. **虚拟网卡建立**：系统状态横栏的虚拟 IP 显示由 Headscale 控制端实际分配的 `100.64.0.x`，而非静态硬编码值。
3. **真实拓扑发现**：
   - 成员列表自动清空演示用假人，实时呈现当前 Tailnet 中在线的真实节点 Hostname 与真实虚拟 IP。
   - 当外部有真实节点上下线时，客户端列表能在 10 秒内动态同步更新。
4. **真实网络转发**：本地 `127.0.0.1:1055` SOCKS5 代理可真正打通向 Tailnet 内部服务器的 TCP 访问。

### 7.3 降级回退鲁棒性标准 (Fallback Robustness)
- 当 `config.json` 配置了无法访问的公网 IP 或域名时，客户端启动探测超时（严格限制在 2 秒内），自动静默回退至仿真模式，并于自查面板给出“网络不可达，已平滑切换至仿真演示”的明确提示，严禁界面卡死或进程崩溃退出。
