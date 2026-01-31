document.addEventListener('DOMContentLoaded', () => {
    const nodeGrid = document.getElementById('nodeGrid');
    const usernameDisplay = document.getElementById('usernameDisplay');
    const loadingOverlay = document.getElementById('loadingOverlay');

    async function fetchUserInfo() {
        try {
            const response = await fetch('/api/me');
            const data = await response.json();
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
            if (response.status === 401) {
                window.location.href = '/login';
                return;
            }
            const result = await response.json();
            // 确定使用标准返回格式 { success: true, data: [] }
            const nodes = result.data || [];
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
            const isMyNode = node.is_assigned_to_me;
            const statusClass = `status-${node.status}`;
            const statusText = {
                'idle': '空闲',
                'busy': node.is_assigned_to_me ? '您正在使用' : '正在使用',
                'offline': '离线',
                'error': '异常'
            }[node.status] || node.status;

            return `
                <div class="node-card ${isMyNode ? 'my-node' : ''}">
                    ${isMyNode ? '<div class="my-badge">当前使用中</div>' : ''}
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
                            <span>型号: ${node.model}</span>
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
                    <button class="enter-btn ${isMyNode ? 'resume' : ''}" 
                            onclick="enterNode(${node.id})">
                        <i class="fa-solid ${isMyNode ? 'fa-play' : 'fa-right-to-bracket'}"></i>
                        ${isMyNode ? '恢复连接' : '进入监考'}
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
            const result = await response.json();
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
        return date.toLocaleTimeString();
    }

    // --- 修改密码逻辑 ---
    const passwordModal = document.getElementById('passwordModal');
    
    window.openPasswordModal = function() {
        passwordModal.style.display = 'flex';
    }

    window.closePasswordModal = function() {
        passwordModal.style.display = 'none';
        document.getElementById('oldPassword').value = '';
        document.getElementById('newPassword').value = '';
        document.getElementById('confirmPassword').value = '';
    }

    window.submitPasswordChange = async function() {
        const old_password = document.getElementById('oldPassword').value;
        const new_password = document.getElementById('newPassword').value;
        const confirm_password = document.getElementById('confirmPassword').value;

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
            const result = await response.json();
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
    // Refresh every 10 seconds for more responsive updates
    setInterval(fetchNodes, 10000);
});
