// collaboration_tool_solution/team_collab/frontend/src/main.js

let activePeer = '100.64.0.2';
let peersData = [];
let devServersData = [];

// DOM Ready
window.addEventListener('DOMContentLoaded', () => {
  initTabs();
  initSystemRibbon();
  initIM();
  initTransfer();
  initMeeting();
  initDevToolbox();
  loadInitialData();
});

// 1. 选项卡切换控制
function initTabs() {
  const navItems = document.querySelectorAll('.nav-item');
  const panes = document.querySelectorAll('.tab-pane');

  navItems.forEach(item => {
    item.addEventListener('click', () => {
      const tabId = item.getAttribute('data-tab');
      navItems.forEach(n => n.classList.remove('active'));
      panes.forEach(p => p.classList.remove('active'));

      item.classList.add('active');
      const targetPane = document.getElementById(tabId);
      if (targetPane) targetPane.classList.add('active');
    });
  });
}

// 2. 状态横栏与代理控制
function initSystemRibbon() {
  const btnToggle = document.getElementById('btn-toggle-proxy');
  let proxyRunning = true;

  btnToggle.addEventListener('click', async () => {
    proxyRunning = !proxyRunning;
    try {
      const res = await fetch(`/api/proxy/toggle?enable=${proxyRunning}`);
      const data = await res.json();
      const stateEl = document.getElementById('val-socks5-state');
      if (data.running) {
        stateEl.textContent = '运行中 (127.0.0.1:1055)';
        stateEl.className = 'ribbon-value text-amber';
        btnToggle.textContent = '关闭本地代理';
      } else {
        stateEl.textContent = '已停止';
        stateEl.className = 'ribbon-value text-rose';
        btnToggle.textContent = '启动本地代理';
      }
    } catch {
      // 离线/本地回退模拟
      const stateEl = document.getElementById('val-socks5-state');
      stateEl.textContent = proxyRunning ? '运行中 (127.0.0.1:1055)' : '已停止';
      btnToggle.textContent = proxyRunning ? '关闭本地代理' : '启动本地代理';
    }
  });
}

// 3. 即时通讯功能逻辑
function initIM() {
  const btnProbe = document.getElementById('btn-probe-link');
  const btnSimulateSym = document.getElementById('btn-simulate-sym');
  const btnSend = document.getElementById('btn-send-message');
  const textInput = document.getElementById('chat-text-input');

  // 3秒打洞竞速探测
  btnProbe.addEventListener('click', async () => {
    btnProbe.disabled = true;
    btnProbe.textContent = '探测打洞中...';
    try {
      const res = await fetch(`/api/probe?peer_ip=${activePeer}`);
      const data = await res.json();
      updateLinkBadge(data.current_mode, data.latency_ms, data.reason);
    } catch {
      updateLinkBadge('DIRECT_P2P', 5, 'Direct P2P Succeeded (Fallback Mock)');
    } finally {
      btnProbe.disabled = false;
      btnProbe.textContent = '3s 竞速探测';
    }
  });

  // 模拟对称 NAT 快速降级 (0ms 熔断切入 DERP)
  btnSimulateSym.addEventListener('click', async () => {
    btnSimulateSym.disabled = true;
    btnSimulateSym.textContent = '快速判定中...';
    try {
      const res = await fetch(`/api/probe?peer_ip=${activePeer}&force_symmetric=true`);
      const data = await res.json();
      updateLinkBadge(data.current_mode, data.latency_ms, data.reason);
    } catch {
      updateLinkBadge('DERP_RELAY', 22, 'Fast-Path: Dual Symmetric NAT (0ms Fallback to DERP)');
    } finally {
      btnSimulateSym.disabled = false;
      btnSimulateSym.textContent = '模拟对称 NAT 快速降级';
    }
  });

  // 发送消息
  const doSend = async () => {
    const text = textInput.value.trim();
    if (!text) return;
    textInput.value = '';

    appendMessageToBox({
      sender_name: '当前用户 (本机)',
      content: text,
      timestamp: new Date().toLocaleTimeString(),
      is_direct_p2p: document.getElementById('chat-link-badge').classList.contains('badge-p2p'),
      latency_ms: 6,
      outgoing: true
    });

    try {
      await fetch('/api/chat/send', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ target_ip: activePeer, content: text })
      });
    } catch {
      // 本地单机模拟
    }
  };

  btnSend.addEventListener('click', doSend);
  textInput.addEventListener('keydown', (e) => {
    if (e.key === 'Enter') doSend();
  });
}

function updateLinkBadge(mode, latency, reason) {
  const badgeEl = document.getElementById('chat-link-badge');
  const textEl = document.getElementById('chat-link-text');
  if (mode === 'DIRECT_P2P') {
    badgeEl.className = 'link-badge badge-p2p';
    textEl.textContent = `DIRECT P2P (${latency}ms)`;
  } else {
    badgeEl.className = 'link-badge badge-relay';
    textEl.textContent = `DERP RELAY (${latency}ms)`;
  }
  if (reason) {
    appendSystemNotice(reason);
  }
}

function appendSystemNotice(text) {
  const box = document.getElementById('chat-messages-box');
  const div = document.createElement('div');
  div.style.textAlign = 'center';
  div.style.fontSize = '0.72rem';
  div.style.color = '#94a3b8';
  div.style.margin = '4px 0';
  div.textContent = `[网络内核事件] ${text}`;
  box.appendChild(div);
  box.scrollTop = box.scrollHeight;
}

function appendMessageToBox(msg) {
  const box = document.getElementById('chat-messages-box');
  const bubble = document.createElement('div');
  bubble.className = `message-bubble ${msg.outgoing ? 'outgoing' : 'incoming'}`;

  const linkTag = msg.is_direct_p2p ? 'P2P 直连' : 'DERP 中继';
  bubble.innerHTML = `
    <div class="message-sender">${msg.sender_name}</div>
    <div class="message-content">${escapeHTML(msg.content)}</div>
    <div class="message-meta">
      <span>${msg.timestamp}</span>
      <span>${linkTag} · ${msg.latency_ms}ms</span>
    </div>
  `;
  box.appendChild(bubble);
  box.scrollTop = box.scrollHeight;
}

// 4. 大文件直传逻辑
function initTransfer() {
  const dropzone = document.getElementById('upload-dropzone');
  const filePicker = document.getElementById('file-picker');
  const btnBrowse = document.getElementById('btn-browse-file');
  const btnStart = document.getElementById('btn-start-transfer');
  const progressBox = document.getElementById('transfer-progress-box');
  const progressBar = document.getElementById('trans-bar');
  const chunksText = document.getElementById('trans-chunks');
  const speedText = document.getElementById('trans-speed');

  btnBrowse.addEventListener('click', () => filePicker.click());
  filePicker.addEventListener('change', (e) => {
    if (e.target.files.length > 0) {
      document.getElementById('trans-file-name').textContent =
        `${e.target.files[0].name} (${(e.target.files[0].size / (1024*1024)).toFixed(1)} MB)`;
    }
  });

  btnStart.addEventListener('click', () => {
    progressBox.style.display = 'block';
    let progress = 0;
    const totalChunks = 512;
    const interval = setInterval(() => {
      progress += 16;
      if (progress >= totalChunks) {
        progress = totalChunks;
        clearInterval(interval);
        speedText.textContent = '传输完成 (100% 校验一致)';
      }
      const percent = ((progress / totalChunks) * 100).toFixed(0);
      progressBar.style.width = `${percent}%`;
      chunksText.textContent = `${progress} / ${totalChunks} (2MB/块)`;
    }, 120);
  });
}

// 5. 在线会议网格渲染
function initMeeting() {
  const galleryStrip = document.getElementById('gallery-strip');
  const participants = [
    { name: '李工 (前端/移动端)', role: 'dev', speaking: true, subRes: '360p @ 15fps' },
    { name: '王运营 (产品交付)', role: 'member', speaking: false, subRes: '180p 缩略图' },
    { name: '赵测试 (QA负责人)', role: 'dev', speaking: false, subRes: '180p 缩略图' },
    { name: '孙工程师 (云原生)', role: 'dev', speaking: false, subRes: '180p 缩略图' },
    { name: '钱经理 (技术总监)', role: 'member', speaking: false, subRes: '180p 缩略图' }
  ];

  galleryStrip.innerHTML = participants.map(p => `
    <div class="video-tile ${p.speaking ? 'speaking' : ''}">
      <div class="tile-name">${p.name}</div>
      <div class="tile-badge">${p.subRes} (Dynacast)</div>
    </div>
  `).join('');
}

// 6. 开发者工具箱与 SSH 复制
function initDevToolbox() {
  const btnCopy = document.getElementById('btn-copy-ssh');
  btnCopy.addEventListener('click', async () => {
    const preview = document.getElementById('ssh-config-preview').textContent;
    try {
      await navigator.clipboard.writeText(preview);
      btnCopy.textContent = '已复制到剪贴板!';
      setTimeout(() => { btnCopy.textContent = '复制 OpenSSH 配置'; }, 2000);
    } catch {
      alert('复制成功: 请手动复制文本框内容');
    }
  });
}

// 7. 加载后端初始化数据
async function loadInitialData() {
  try {
    const peersRes = await fetch('/api/peers');
    peersData = await peersRes.json();
    renderPeerList(peersData);
  } catch {
    peersData = [
      { ip: '100.64.0.2', name: '张工 (后端架构)', role: 'dev', is_online: true, is_direct_p2p: true, latency_ms: 5 },
      { ip: '100.64.0.3', name: '李工 (前端/移动端)', role: 'dev', is_online: true, is_direct_p2p: false, latency_ms: 21 },
      { ip: '100.64.0.4', name: '王运营 (产品交付)', role: 'member', is_online: true, is_direct_p2p: true, latency_ms: 8 }
    ];
    renderPeerList(peersData);
  }

  try {
    const devRes = await fetch('/api/dev-servers');
    devServersData = await devRes.json();
    renderDevServers(devServersData);
  } catch {
    devServersData = [
      {
        name: 'dev-linux-primary',
        ip: '100.64.0.10',
        ssh_port: 22,
        db_port: 3306,
        description: '核心研发测试服务器 (Ubuntu 22.04 LTS, Docker/K8s)',
        tag: 'tag:server'
      },
      {
        name: 'qa-database-instance',
        ip: '100.64.0.11',
        ssh_port: 22,
        db_port: 5432,
        description: '测试集群 PostgreSQL / Redis 数据存储节点',
        tag: 'tag:server'
      }
    ];
    renderDevServers(devServersData);
  }

  // 默认填充首条历史消息
  appendMessageToBox({
    sender_name: '张工 (后端架构)',
    content: '你好，开发机 100.64.0.10 上的微服务已启动，你可以经本地代理 127.0.0.1:1055 直连调试 SSH 与 MySQL。',
    timestamp: '10:45',
    is_direct_p2p: true,
    latency_ms: 5,
    outgoing: false
  });
}

function renderPeerList(peers) {
  const container = document.getElementById('peer-list-container');
  container.innerHTML = peers.map(p => `
    <li class="peer-item ${p.ip === activePeer ? 'active' : ''}" data-ip="${p.ip}">
      <div class="peer-info">
        <span class="peer-name">${p.name}</span>
        <span class="peer-ip">${p.ip}</span>
      </div>
      <div class="link-badge ${p.is_direct_p2p ? 'badge-p2p' : 'badge-relay'}">
        <span class="dot"></span>
        <span>${p.is_direct_p2p ? 'P2P' : 'DERP'} · ${p.latency_ms}ms</span>
      </div>
    </li>
  `).join('');

  container.querySelectorAll('.peer-item').forEach(item => {
    item.addEventListener('click', () => {
      container.querySelectorAll('.peer-item').forEach(el => el.classList.remove('active'));
      item.classList.add('active');
      activePeer = item.getAttribute('data-ip');
      const peer = peersData.find(p => p.ip === activePeer);
      if (peer) {
        document.getElementById('chat-target-name').textContent = peer.name;
        document.getElementById('chat-target-ip').textContent = peer.ip;
        updateLinkBadge(peer.is_direct_p2p ? 'DIRECT_P2P' : 'DERP_RELAY', peer.latency_ms);
      }
    });
  });
}

function renderDevServers(servers) {
  const container = document.getElementById('dev-servers-container');
  container.innerHTML = servers.map(s => `
    <div class="server-card">
      <div class="server-card-header">
        <div>
          <div class="server-title">${s.name}</div>
          <div class="server-ip">${s.ip}</div>
        </div>
        <span class="badge badge-emerald">tag:server</span>
      </div>
      <p style="font-size:0.8rem; color:#94a3b8;">${s.description}</p>
      <div class="server-ports">
        <span class="port-tag">SSH : ${s.ssh_port}</span>
        <span class="port-tag">DB : ${s.db_port}</span>
        <span class="port-tag">Proxy : 127.0.0.1:1055</span>
      </div>
    </div>
  `).join('');
}

function escapeHTML(str) {
  return str.replace(/[&<>'"]/g,
    tag => ({
      '&': '&amp;',
      '<': '&lt;',
      '>': '&gt;',
      "'": '&#39;',
      '"': '&quot;'
    }[tag] || tag)
  );
}
