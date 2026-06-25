document.addEventListener('DOMContentLoaded', () => {
    let currentPage = 1;
    const pageSize = 20;

    function normalizeNode(node) {
        if (!node || typeof node !== 'object') return node;
        const normalized = { ...node };
        if (normalized.id === undefined && normalized.ID !== undefined) normalized.id = normalized.ID;
        if (normalized.current_exam_id === undefined && normalized.CurrentExamID !== undefined) normalized.current_exam_id = normalized.CurrentExamID;
        if (normalized.current_exam === undefined && normalized.CurrentExam !== undefined) normalized.current_exam = normalized.CurrentExam;
        if (normalized.last_heartbeat_at === undefined && normalized.LastHeartbeatAt !== undefined) normalized.last_heartbeat_at = normalized.LastHeartbeatAt;
        return normalized;
    }

    function normalizeExam(exam) {
        if (!exam || typeof exam !== 'object') return exam;
        const normalized = { ...exam };
        if (normalized.id === undefined && normalized.ID !== undefined) normalized.id = normalized.ID;
        if (normalized.start_time === undefined && normalized.StartTime !== undefined) normalized.start_time = normalized.StartTime;
        if (normalized.end_time === undefined && normalized.EndTime !== undefined) normalized.end_time = normalized.EndTime;
        if (normalized.room === undefined && normalized.Room !== undefined) normalized.room = normalized.Room;
        if (normalized.node === undefined && normalized.Node !== undefined) normalized.node = normalized.Node;
        return normalized;
    }

    const nodeGrid = document.getElementById('nodeGrid');
    const usernameDisplay = document.getElementById('usernameDisplay');
    const loadingOverlay = document.getElementById('loadingOverlay');
    const examTableBody = document.getElementById('examTableBody');
    const pagination = document.getElementById('pagination');

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

    // 加载统计数据
    async function loadStats() {
        try {
            const response = await fetch('/api/proctor/exams/stats');
            if (handleAuthFailure(response)) return;
            const result = await parseJsonSafe(response);
            if (result.success && result.data) {
                document.getElementById('totalExams').textContent = result.data.total || 0;
                document.getElementById('ongoingExams').textContent = result.data.ongoing || 0;
                document.getElementById('upcomingExams').textContent = result.data.upcoming || 0;
                document.getElementById('completedExams').textContent = result.data.completed || 0;
            }
        } catch (e) {
            console.error('Failed to load stats');
        }
    }

    // 加载楼宇列表（从考试数据中提取）
    async function loadBuildings() {
        try {
            // 获取所有考试，从中提取楼宇列表
            const response = await fetch('/api/proctor/exams?page=1&page_size=1000');
            if (handleAuthFailure(response)) return;
            const result = await parseJsonSafe(response);
            const exams = (result.data || []).map(normalizeExam);

            // 从考试的教室信息中提取楼宇
            const buildings = [...new Set(
                exams
                    .map(e => (e.room || e.Room || {}).building || (e.room || e.Room || {}).Building)
                    .filter(Boolean)
            )].sort();

            const select = document.getElementById('filterBuilding');
            select.innerHTML = '<option value="">全部楼宇</option>' +
                buildings.map(b => `<option value="${b}">${b}</option>`).join('');
        } catch (e) {
            console.error('Failed to load buildings', e);
        }
    }

    // 加载考试列表
    window.loadExams = async function(page = 1) {
        currentPage = page;
        const building = document.getElementById('filterBuilding').value;
        const date = document.getElementById('filterDate').value;
        const status = document.getElementById('filterStatus').value;

        const params = new URLSearchParams({
            page: currentPage,
            page_size: pageSize
        });
        if (building) params.append('building', building);
        if (date) params.append('date', date);
        if (status) params.append('status', status);

        try {
            examTableBody.innerHTML = '<tr><td colspan="8" style="text-align: center; padding: 2rem;"><i class="fa-solid fa-circle-notch fa-spin"></i> 正在加载...</td></tr>';

            const response = await fetch(`/api/proctor/exams?${params}`);
            if (handleAuthFailure(response)) return;
            const result = await parseJsonSafe(response);

            if (result.success) {
                const exams = (result.data || []).map(normalizeExam);
                renderExams(exams);
                renderPagination(result.pagination);
            }
        } catch (e) {
            console.error('Failed to load exams', e);
            examTableBody.innerHTML = '<tr><td colspan="8" style="text-align: center; padding: 2rem; color: #ef4444;">加载失败，请刷新重试</td></tr>';
        }
    }

    function renderExams(exams) {
        if (!exams || exams.length === 0) {
            examTableBody.innerHTML = '<tr><td colspan="8" style="text-align: center; padding: 2rem; color: #9ca3af;">暂无考试数据</td></tr>';
            return;
        }

        examTableBody.innerHTML = exams.map(exam => {
            const room = exam.room || exam.Room || {};
            const node = exam.node || exam.Node || {};
            const now = new Date();
            const startTime = new Date(exam.start_time || exam.StartTime);
            const endTime = exam.end_time || exam.EndTime;

            let statusBadge = '';
            let statusText = '';
            if (endTime) {
                statusBadge = 'status-completed';
                statusText = '已完成';
            } else if (startTime <= now) {
                statusBadge = 'status-ongoing';
                statusText = '进行中';
            } else {
                statusBadge = 'status-upcoming';
                statusText = '即将开始';
            }

            const durationMinutes = Math.floor((exam.duration_seconds || exam.DurationSeconds || 0) / 60);

            return `
                <tr>
                    <td><strong>${exam.subject || exam.Subject || '-'}</strong></td>
                    <td>${room.building || room.Building || '-'}</td>
                    <td>${room.name || room.Name || '-'}</td>
                    <td>${formatDateTime(exam.start_time || exam.StartTime)}</td>
                    <td>${durationMinutes} 分钟</td>
                    <td><span class="status-badge ${statusBadge}">${statusText}</span></td>
                    <td>${node.name || node.Name || '未分配'}</td>
                    <td>
                        ${!endTime && node.id ? `<button onclick="enterNode(${node.id})" style="padding: 0.25rem 0.75rem; background: var(--primary-color); color: white; border: none; border-radius: 0.25rem; cursor: pointer; font-size: 0.75rem;">
                            <i class="fa-solid fa-right-to-bracket"></i> 进入
                        </button>` : '<span style="color: #9ca3af; font-size: 0.75rem;">${endTime ? '已结束' : '-'}</span>'}
                    </td>
                </tr>
            `;
        }).join('');
    }

    function renderPagination(pag) {
        if (!pag || pag.total === 0) {
            pagination.innerHTML = '';
            return;
        }

        const totalPage = pag.total_page || 1;
        const current = pag.page || 1;

        let html = '';

        // 上一页
        html += `<button onclick="loadExams(${current - 1})" ${current <= 1 ? 'disabled' : ''}>
            <i class="fa-solid fa-chevron-left"></i> 上一页
        </button>`;

        // 页码
        const maxButtons = 5;
        let startPage = Math.max(1, current - Math.floor(maxButtons / 2));
        let endPage = Math.min(totalPage, startPage + maxButtons - 1);

        if (endPage - startPage < maxButtons - 1) {
            startPage = Math.max(1, endPage - maxButtons + 1);
        }

        for (let i = startPage; i <= endPage; i++) {
            html += `<button onclick="loadExams(${i})" ${i === current ? 'class="active"' : ''}>${i}</button>`;
        }

        // 下一页
        html += `<button onclick="loadExams(${current + 1})" ${current >= totalPage ? 'disabled' : ''}>
            下一页 <i class="fa-solid fa-chevron-right"></i>
        </button>`;

        html += `<span>共 ${pag.total} 条记录</span>`;

        pagination.innerHTML = html;
    }

    window.clearFilters = function() {
        document.getElementById('filterBuilding').value = '';
        document.getElementById('filterDate').value = '';
        document.getElementById('filterStatus').value = '';
        loadExams(1);
    }

    function formatDateTime(timeStr) {
        if (!timeStr) return '-';
        const date = new Date(timeStr);
        if (Number.isNaN(date.getTime())) return '-';
        return date.toLocaleString('zh-CN', {
            year: 'numeric',
            month: '2-digit',
            day: '2-digit',
            hour: '2-digit',
            minute: '2-digit'
        });
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
            const currentExam = node.current_exam || null;
            const room = currentExam && currentExam.room ? currentExam.room : null;
            const examLocation = currentExam
                ? [room && room.building, room && room.name].filter(Boolean).join(' ') || '未知地点'
                : '暂无当前考试';
            const examSubject = currentExam ? (currentExam.subject || '未填写科目') : '暂无当前考试';
            const examTime = currentExam ? formatDateTime(currentExam.start_time) : '暂无当前考试';
            const isOccupied = !!node.current_exam_id || node.status === 'busy';
            const isUnavailable = node.status === 'offline' || node.status === 'error';
            const statusClass = `status-${node.status}`;
            let statusText = isOccupied ? '已占用' : '未占用';
            if (node.status === 'offline') statusText = '离线';
            if (node.status === 'error') statusText = '异常';
            if (node.status === 'busy') statusText = '监考中';

            let actionText = isOccupied ? '进入监考' : '进入监考';
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
                            <i class="fa-solid fa-location-dot"></i>
                            <span>考试地点: ${examLocation}</span>
                        </div>
                        <div class="info-item">
                            <i class="fa-solid fa-book-open"></i>
                            <span>考试科目: ${examSubject}</span>
                        </div>
                        <div class="info-item">
                            <i class="fa-solid fa-calendar-days"></i>
                            <span>考试时间: ${examTime}</span>
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
                window.location.href = result.jump_url;
            } else {
                alert(result.error || '无法进入该节点，请稍后重试。');
                fetchNodes();
            }
        } catch (error) {
            alert('请求出错，请重试');
        } finally {
            loadingOverlay.style.display = 'none';
        }
    };

    // 修改密码逻辑
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
    loadStats();
    loadBuildings();
    loadExams(1);
    fetchNodes();

    // 自动刷新（静默）
    setInterval(() => {
        loadStats();
        loadExams(currentPage);
    }, 30000); // 每30秒刷新一次
});
