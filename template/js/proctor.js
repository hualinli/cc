document.addEventListener('DOMContentLoaded', () => {
    function normalizeNode(node) {
        if (!node || typeof node !== 'object') return node;
        const normalized = { ...node };
        if (normalized.id === undefined && normalized.ID !== undefined) normalized.id = normalized.ID;
        if (normalized.current_user_id === undefined && normalized.CurrentUserID !== undefined) normalized.current_user_id = normalized.CurrentUserID;
        if (normalized.current_exam_id === undefined && normalized.CurrentExamID !== undefined) normalized.current_exam_id = normalized.CurrentExamID;
        if (normalized.last_heartbeat_at === undefined && normalized.LastHeartbeatAt !== undefined) normalized.last_heartbeat_at = normalized.LastHeartbeatAt;
        return normalized;
    }

    const nodeGrid = document.getElementById('nodeGrid');
    const usernameDisplay = document.getElementById('usernameDisplay');
    const loadingOverlay = document.getElementById('loadingOverlay');

    function handleAuthFailure(response) {
        if (response.status === 401) {
            window.location.href = '/login';
            return true;
        }
        if (response.status === 403) {
            alert('当前账号无权执行该操作');
            return true;
        }
        return false;
    }

    async function parseJsonSafe(response) {
        try {
            return await response.json();
        } catch (e) {
            return {};
        }
    }

    async function fetchUserInfo() {
        try {
            const response = await fetch('/api/me');
            if (handleAuthFailure(response)) return;
            const data = await parseJsonSafe(response);
            if (data.username) {
                usernameDisplay.innerText = data.username;
            }
        } catch (e) {
            console.error('Failed to fetch user info');
        }
    }

    window.logout = async function () {
        if (confirm("确定要退出登录吗？")) {
            try {
                const response = await fetch('/logout');
                const result = await response.json();
                if (result.success) {
                    window.location.href = result.redirect || '/login';
                }
            } catch (e) {
                alert("退出失败，请重试");
            }
        }
    }

    async function fetchNodes() {
        try {
            const response = await fetch('/api/proctor/nodes');
            if (handleAuthFailure(response)) return;
            const result = await parseJsonSafe(response);
            // 确定使用标准返回格式 { success: true, data: [] }
            const nodes = (result.data || []).map(normalizeNode);
            renderNodes(nodes);
        } catch (error) {
            console.error('Failed to fetch nodes:', error);
            nodeGrid.innerHTML = `
                <div class="empty-state">
                    <i class="fa-solid fa-triangle-exclamation" style="font-size: 2rem; color: var(--status-offline);"></i>
                    <p style="margin-top: 1rem;">获取节点列表失败，请刷新页面重试。</p>
                </div>
            `;
        }
    }

    function renderNodes(nodes) {
        if (!nodes || nodes.length === 0) {
            nodeGrid.innerHTML = `
                <div class="empty-state">
                    <i class="fa-solid fa-inbox" style="font-size: 2rem; color: var(--text-muted);"></i>
                    <p style="margin-top: 1rem;">当前没有可用的监考节点。</p>
                </div>
            `;
            return;
        }

        nodeGrid.innerHTML = nodes.map(node => {
            const isOccupied = !!node.current_user_id || !!node.current_exam_id || node.status === 'busy';
            const isUnavailable = node.status === 'offline' || node.status === 'error';
            const statusClass = `status-${node.status}`;
            let statusText = isOccupied ? '已占用' : '未占用';
            if (node.status === 'offline') statusText = '离线';
            if (node.status === 'error') statusText = '异常';
            if (node.status === 'busy') statusText = '监考中';

            let actionText = isOccupied ? '继续监考' : '进入监考';
            let actionIcon = isOccupied ? 'fa-play' : 'fa-right-to-bracket';
            if (isUnavailable) {
                actionText = node.status === 'error' ? '节点异常' : '节点离线';
                actionIcon = 'fa-ban';
            }

            const buttonAttrs = isUnavailable
                ? 'disabled aria-disabled="true"'
                : `onclick="enterNode(${node.id})"`;

            return `
                <div class="node-card ${isOccupied ? 'my-node' : ''}">
                    ${isOccupied ? '<div class="my-badge">当前占用中</div>' : ''}
                    <div class="node-header">
                        <div class="node-name">${node.name}</div>
                        <div style="display: flex; align-items: center; font-size: 0.8125rem;">
                            <span class="status-dot ${statusClass}"></span>
                            ${statusText}
                        </div>
                    </div>
                    <div class="node-info">
                        <div class="info-item">
                            <i class="fa-solid fa-microchip"></i>
                            <span>型号: ${node.nodemodel || '-'}</span>
                        </div>
                        <div class="info-item">
                            <i class="fa-solid fa-network-wired"></i>
                            <span>地址: ${node.address}</span>
                        </div>
                        <div class="info-item">
                            <i class="fa-solid fa-clock"></i>
                            <span>最后心跳: ${formatTime(node.last_heartbeat_at)}</span>
                        </div>
                    </div>
                    <button class="enter-btn ${isOccupied ? 'resume' : ''}" 
                            ${buttonAttrs}>
                        <i class="fa-solid ${actionIcon}"></i>
                        ${actionText}
                    </button>
                </div>
            `;
        }).join('');
    }

    window.enterNode = async (nodeId) => {
        loadingOverlay.style.display = 'flex';
        try {
            const response = await fetch(`/api/proctor/nodes/${nodeId}/jump`, {
                method: 'POST'
            });
            if (handleAuthFailure(response)) return;
            const result = await parseJsonSafe(response);
            if (result.success && result.jump_url) {
                // 跳转到具体的监考节点页面
                window.location.href = result.jump_url;
            } else {
                alert(result.error || '无法进入该节点，请稍后重试。');
                fetchNodes(); // 刷新列表
            }
        } catch (error) {
            alert('请求出错，请重试');
        } finally {
            loadingOverlay.style.display = 'none';
        }
    };

    function formatTime(timeStr) {
        if (!timeStr) return '未知';
        const date = new Date(timeStr);
        if (Number.isNaN(date.getTime())) return '无效时间';
        if (date.getUTCFullYear() <= 1971) return '未知';
        return date.toLocaleTimeString();
    }

    // --- 修改密码逻辑 ---
    const passwordModal = document.getElementById('passwordModal');

    window.openPasswordModal = function () {
        passwordModal.style.display = 'flex';
    }

    window.closePasswordModal = function () {
        passwordModal.style.display = 'none';
        document.getElementById('oldPassword').value = '';
        document.getElementById('newPassword').value = '';
        document.getElementById('confirmPassword').value = '';
    }

    window.submitPasswordChange = async function () {
        const old_password = document.getElementById('oldPassword').value.trim();
        const new_password = document.getElementById('newPassword').value.trim();
        const confirm_password = document.getElementById('confirmPassword').value.trim();

        if (!old_password || !new_password) {
            alert("请填写完整信息");
            return;
        }

        if (new_password !== confirm_password) {
            alert("两次输入的新密码不一致");
            return;
        }

        try {
            const response = await fetch('/api/users/password', {
                method: 'PUT',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ old_password, new_password })
            });
            const result = await parseJsonSafe(response);

            // 改密接口会用 401 表示旧密码错误，不能按未登录处理直接跳转。
            if (response.status === 401 && result && result.error === '旧密码错误') {
                alert(result.error);
                return;
            }
            if (handleAuthFailure(response)) return;

            if (response.ok && result.success) {
                alert('密码修改成功，请重新登录');
                window.location.href = '/login';
            } else {
                alert(result.error || '修改密码失败');
            }
        } catch (err) {
            alert('网络请求出错');
        }
    }

    // Initial load
    fetchUserInfo();
    fetchNodes();

    // 页面重新可见时立即刷新，避免后台标签页恢复后状态滞后。
    document.addEventListener('visibilitychange', () => {
        if (!document.hidden) {
            fetchNodes();
        }
    });

    // 缩短轮询间隔，降低状态变化感知延迟。
    setInterval(fetchNodes, 3000);
});
