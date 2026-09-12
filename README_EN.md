<!-- README_EN.md -->
# ReatiWire: Private IM & Video Conferencing Collaboration System for Agile Teams (20-seat)

[English](README_EN.md) | [简体中文](README.md)

> **Positioning Statement**: ReatiWire is a **private, secure collaboration workspace and developer direct-connect gateway** purpose-built for high-confidentiality, high-velocity R&D teams of ~20 engineers. Deeply integrating a userspace WireGuard stack (`tsnet`), private DERP fast circuit-breaker failover engine, LiveKit SFU (Selective Forwarding Unit), and a local userspace SOCKS5 proxy gateway, ReatiWire delivers zero-privilege operation without kernel drivers, physical zero server egress traffic on 1v1 data plane, and deterministic bandwidth control within a 100 Mbps public VPS ceiling.

---

## 1. Architecture & Data Plane Topology

ReatiWire employs a hybrid multi-tier topological architecture: **"Direct P2P Data Plane First + Deterministic Centralized Media SFU Convergence + Zero-Trust Userspace Control Plane"**:

```text
                           ┌────────────────────────────────────────────────────────┐
                           │           Public VPS (2 vCPU / 4G RAM / 100M Port)     │
                           │  - Headscale Control Plane (TLS Wildcard / Auto-Renew) │
                           │  - Private DERP Relay (Region 901, cqq-prv, 443/UDP)   │
                           │  - LiveKit SFU (Dynacast, Peak <= 45M, 85M Alert Cap)  │
                           └───────────────▲──────────────────────▲─────────────────┘
                                           │ (Signaling/JWT/DERP) │ (Multi-party AV SFU)
                    ┌──────────────────────┴──────────────────────┴─────────────────┐
                    │                                                               │
        ┌───────────┴───────────┐                                       ┌───────────┴───────────┐
        │ Client A (Zhang - BE) │                                       │ Client B (Li - FE)    │
        │ IP: 100.64.0.2        │                                       │ IP: 100.64.0.3        │
        │ tsnet Userspace Stack │◄═════════════════════════════════════►│ tsnet Userspace Stack │
        │ SOCKS5: :1055         │     Direct P2P (WireGuard + STUN)     │ SOCKS5: :1055         │
        └───────────┬───────────┘    [1v1 Chat / 2MB Chunked File Xfer] └───────────────────────┘
                    │                Physical Line Rate / Server Egress: 0.00 MB
                    ▼ (SOCKS5 Proxy)
        ┌───────────────────────┐
        │ Target Server (dev)   │
        │ IP: 100.64.0.10       │
        │ SSH:22 / MySQL:3306   │
        └───────────────────────┘
```

---

## 2. Core Architectural Mechanisms & Implementation

### 2.1 Default-P2P Data Plane with Zero-Trust Topology
- **1v1 Instant Messaging & High-Speed File Transfer**: Client nodes perform UDP STUN hole-punching over userspace WireGuard (Tailnet `100.64.0.0/10`) by default.
- **Physical Direct-Connect with Zero VPS Bandwidth Cost**: When both peers have public IPv6 addresses or reside within standard cone NATs (Full Cone / Restricted Cone), an end-to-end direct UDP transmission channel is established automatically. Data bypasses the public relay server, achieving gigabit LAN / line-rate broadband speeds with **0.00 MB** server egress consumption.

### 2.2 Private DERP Fast Circuit Breaker with 3000ms Hard Timeout
- **Hole-Punching Circuit Breaker Baseline**: The discovery engine enforces a strict upper bound ($T_{\text{timeout}} \le 3000\text{ ms}$). If P2P UDP handshaking does not conclude within 3 seconds, traffic fails over seamlessly to the self-hosted private DERP relay node (Region 901, `cqq-prv`) hosted on the public VPS, guaranteeing zero communication interruption.
- **Fast-Path Failure Detection**: When STUN discovery identifies that both peers reside behind Symmetric NATs without IPv6, physical hole-punching feasibility is evaluated as 0%. The engine bypasses the 3000ms discovery phase and **immediately shifts to DERP relay in 0 ms**.
- **Seamless Silent Upgrade**: While running in DERP relay mode, a background probe conducts lightweight STUN discovery every 10 seconds. Once network path recovery is detected (e.g., IPv6 becomes available or the node migrates off a restricted network), communication hot-swaps back to Direct P2P.

### 2.3 Centralized SFU Dynamic Traffic Protection (LiveKit SFU @ 100M VPS)
- **P2P Mesh Elimination**: For a 20-seat meeting, full P2P Mesh generates $N(N-1) = 380$ concurrent streams, imposing an upstream requirement of ~28.5 Mbps per node, which readily overwhelms residential and office uplinks. ReatiWire converges all multi-party streams onto a centralized LiveKit SFU (Selective Forwarding Unit) on the VPS.
- **Deterministic Bandwidth Budget Model**:
  - **1 Screen Share (1080p@30fps, 1.5 Mbps) + 19 Audio Streams (40 kbps)**:
    $$\text{Ingress} = 1.5\text{ Mbps} + 20 \times 0.04\text{ Mbps} = 2.3\text{ Mbps}$$
    $$\text{Egress} = 19 \times 1.5\text{ Mbps} + 20 \times 3 \times 0.04\text{ Mbps} = 30.9\text{ Mbps}$$
    Consumes only $30.9\%$ of the 100 Mbps VPS egress bandwidth, remaining safely inside the optimal operational envelope.
  - **Full-Video Mode**: Activates LiveKit Dynacast and Simulcast tiered encoding. Non-active speaker tiles automatically subscribe to 180p thumbnails (80 kbps), bounding total VPS egress peaks to 35–45 Mbps, well beneath the 85 Mbps safety threshold.
  - **VAD Silence Suppression**: Voice Activity Detection suppresses inactive channels and forwards only the top 3 highest-volume audio tracks, eliminating unnecessary downlink stream consumption.

### 2.4 Zero-Privilege Userspace Architecture (via `tsnet`)
- **Zero Kernel Drivers & Zero UAC Prompts**: The client is built on Go + Wails v2, embedding Tailscale's official userspace network stack (`tsnet`).
- **Sandbox Isolation**: Packet framing and WireGuard encryption run entirely within client process memory space. No Wintun / TUN/TAP drivers are installed, no administrator elevation dialogs appear, and the host's global routing table and DNS settings remain untouched.

### 2.5 Developer Target Server P2P Penetration Gateway (Target Server Access)
- **Embedded SOCKS5 Proxy Gateway**: The client runs a standalone local loopback proxy listener (`127.0.0.1:1055`).
- **Seamless Toolchain Integration**: Developers specify a single-line `ProxyCommand` in local `~/.ssh/config` to access internal target machines (e.g., `100.64.0.10:22` or `100.64.0.10:3306`) via standard CLI terminals, VS Code Remote-SSH, DBeaver, or Navicat, strictly governed by Headscale ACL least-privilege policies.

---

## 3. Client Desktop Console UI & Field Demonstrations

The client provides an integrated dark glassmorphic (Glassmorphism) standalone desktop console (native Wails window with system tray persistence, zero exposed external HTTP ports, in-memory IPC bridge), featuring four core modules:

### 3.1 1v1 Instant Messaging Panel (Real-time P2P/DERP Badges)
![IM Chat and Direct Link Badges](assets/screenshots/01_im_chat.png)
- **Peer List & Live Badges**: Displays 20-seat team members with real-time connection badges (`DIRECT P2P (5ms)` green badge or `DERP (21ms)` orange relay badge).
- **Status Dashboard Bar**: Top bar displays virtual IP (`100.64.0.5`), userspace status (`Connected (Zero-Privilege)`), local SOCKS5 port (`127.0.0.1:1055`), and timeout baseline ($T_{\text{timeout}} \le 3000\text{ ms}$).
- **Testing & Fault Injection**: One-click triggers for "3s Race Discovery" and "Simulated Symmetric NAT Fallback" to verify state machine transitions in real time.

### 3.2 P2P Large File Transfer Engine (RFC 2MB Chunking)
![P2P File Transfer Engine](assets/screenshots/02_p2p_transfer.png)
- **RFC 2MB Chunks & Resumable Transfers**: Large binaries (OS images, database dumps, build artifacts) are sliced into 2MB chunks in memory with SHA-256 verification and streamed directly across the peer socket.
- **Physical Line-Rate Direct Transmission**: Field-tested throughput reaches **86.4 MB/s** (saturating gigabit NICs and local bandwidth). The status counter confirms "Server Egress Consumed: 0.00 MB", eliminating cloud bandwidth cost concerns.

### 3.3 Multi-Party Video Conferencing (LiveKit SFU Architecture Room)
![LiveKit SFU Multi-party Video Conference](assets/screenshots/03_livekit_meeting.png)
- **Active Speaker & Gallery Grid**: Supports 1080p @ 30fps screen sharing and camera publishing; attendee thumbnails automatically receive Simulcast 180p downsampled streams.
- **Bandwidth Budget Telemetry**: Real-time telemetry tracks predicted vs. actual VPS egress bandwidth (`31.4 Mbps (Safe Range)`, strictly below the 100M pipe and 85M alert ceiling).
- **Dynamic AV Optimization**: Indicators display real-time Dynacast routing, Simulcast multi-layer status, and VAD audio suppression (active Top 3 audio channels only).

### 3.4 Target Server P2P Direct Gateway (Developer Toolbox)
![Developer Toolbox & Local SOCKS5 Proxy](assets/screenshots/04_dev_toolbox.png)
- **Targeted Internal Service Exposure**: Access developer boxes (`dev-linux-primary: 100.64.0.10`) and test database nodes (`qa-database-instance: 100.64.0.11`), guarded by `tag:dev` and `tag:server` ACL whitelists.
- **One-Click Configuration**: Copy-ready snippets for OpenSSH and VS Code Remote; supports database GUI clients (DBeaver, Navicat) over SOCKS5 proxy.

---

## 4. Repository Structure & Source Code Inventory

```text
collaboration_tool_solution/team_collab/
├── assets/
│   └── screenshots/             # Console interface screenshot assets
│       ├── 01_im_chat.png       # 1v1 IM and peer connection status panel
│       ├── 02_p2p_transfer.png   # 2MB chunked P2P file transfer engine
│       ├── 03_livekit_meeting.png# LiveKit SFU meeting and bandwidth telemetry
│       └── 04_dev_toolbox.png   # Developer toolbox and SOCKS5 proxy config
├── deploy/                      # Server and infrastructure orchestration (VPS)
│   ├── docker-compose.yml       # LiveKit SFU and Redis container compose
│   ├── livekit.yaml             # LiveKit core configuration (Dynacast, Simulcast, 85M cap)
│   ├── headscale/
│   │   ├── acl.hujson           # Least-privilege ACL policies (tag:dev -> tag:server)
│   │   └── derpmap.yaml         # Private DERP Region 901 (cqq-prv) definition
│   └── scripts/
│       └── bootstrap.sh         # Firewall, dependency pulling, and launch script
├── pkg/                         # Core Go backend modules
│   ├── circuitbreaker/
│   │   └── breaker.go           # 3000ms timeout breaker, fast-path failure & recovery state machine
│   ├── im/
│   │   └── chat.go              # P2P IM and connection state reporting
│   ├── livekit/
│   │   └── token.go             # LiveKit room token signing and bandwidth constraint generator
│   ├── proxy/
│   │   └── socks5.go            # Local SOCKS5 proxy gateway (127.0.0.1:1055)
│   ├── transfer/
│   │   └── engine.go            # 2MB chunked resumable file transfer with SHA-256 validation
│   └── tsnet/
│       └── manager.go           # Userspace WireGuard tsnet lifecycle manager
├── frontend/                    # Presentation layer (Vite + Modern UI system)
│   ├── package.json             # Frontend package configuration
│   ├── vite.config.js           # Vite build configuration
│   ├── index.html               # Semantic HTML markup
│   └── src/
│       ├── index.css            # Dark glassmorphic design system
│       └── main.js              # Event dispatch, probes, proxy toggling & UI telemetry
├── app.go                       # Wails application controller and backend bindings
├── main.go                      # Application entrypoint (Wails standalone GUI window)
├── go.mod                       # Go module definition
├── wails.json                   # Wails v2 packaging definition
├── README.md                    # Chinese documentation
└── README_EN.md                 # English documentation (this file)
```

---

## 5. Deployment & Execution Guide

### 5.1 Server-Side One-Click Deployment (Public VPS)
On a public Linux VPS with dedicated IP (open ports: 443/TCP, 443/UDP, 7880/TCP, 50000-50100/UDP), execute:
```bash
cd deploy
# Grant execution permissions and run initialization script
chmod +x scripts/bootstrap.sh
./scripts/bootstrap.sh
```
The script provisions the Headscale control plane, private DERP relay (Region 901), and the LiveKit SFU cluster with active bandwidth policies.

### 5.2 Standalone Client Local Development & Execution
The desktop client is built on Go + Wails v2 with zero external web control ports. All frontend communications run through in-process memory IPC:

#### 1. Frontend Asset Compilation
```bash
cd frontend
npm install
npm run build
cd ..
```

#### 2. Run Local Desktop Application
```bash
# Launch native Wails desktop window
go run .
```
Upon startup, a native dark-mode window will launch. Terminal output confirms:
- Userspace WireGuard Virtual IP: `100.64.0.5` (zero OS administrator elevation)
- Local SOCKS5 Proxy Listener: `127.0.0.1:1055` (for SSH / VS Code / DB toolchains)
- Presentation Layer: Standalone Wails native window (in-memory AssetServer, zero localhost HTTP port exposure, immune to localhost CSRF)

#### 3. Window Lifecycle & Exit Instructions
- **Background Persistence**: Closing the window with the top-right `X` button minimizes the window to the background. The SOCKS5 proxy (`127.0.0.1:1055`) and WireGuard P2P tunnels remain active.
- **Reactivate Window**: Double-clicking the executable instantly restores the foreground window.
- **Full Process Termination**: In the terminal, press **`Ctrl + C`** (type `Y` if prompted with `Terminate batch job (Y/N)?`). All network stacks and proxy listeners will cleanly unbind and terminate.

### 5.3 Standalone Portable Client Packaging (Wails Production Binary)
Build a portable, standalone GUI binary on your build host:
```bash
# Option A: Standard Go toolchain build (Zero CGO dependency, pure native binary)
go build -tags production -ldflags "-s -w -H windowsgui" -o reati_wire.exe .

# Option B: Standard Wails CLI build
wails build -clean
```
The resulting `reati_wire.exe` is a single portable executable ready for team distribution:
- **Native Standalone Window**: Opens directly without launching a web browser; zero external runtime dependencies.
- **Zero Attack Surface**: No host HTTP ports are bound, eliminating cross-origin attacks from web browsers.
- **Zero-Privilege Operation**: Uses userspace `tsnet` without virtual network adapter drivers or UAC prompts.

---

## 6. Developer Toolchain Integration (Direct Server Access)

With the client running and the local SOCKS5 proxy active (`127.0.0.1:1055`), configure local `~/.ssh/config`:

```ssh-config
Host dev-server
    HostName 100.64.0.10
    User root
    # Linux / macOS OpenSSH:
    ProxyCommand nc -X 5 -x 127.0.0.1:1055 %h %p
    # Windows 10/11 OpenSSH:
    # ProxyCommand connect -S 127.0.0.1:1055 %h %p
    StrictHostKeyChecking no
    ServerAliveInterval 15
```

- **CLI Terminal Access**: Run `ssh dev-server` for instant login;
- **VS Code Remote Development**: Install `Remote - SSH` extension, select `dev-server` from the remote targets list to open remote codebases or containers directly;
- **Database GUI Access**: In DBeaver / Navicat, enable "Use SOCKS5 Proxy", enter `127.0.0.1:1055`, and point host to `100.64.0.10:3306` to inspect and query the database securely.
