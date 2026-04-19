// --- 1. 全局：Tab 切换逻辑 ---
function switchTab(pageId, navElement) {
    // 隐藏所有页面
    document.querySelectorAll('.page-section').forEach(el => el.classList.remove('active'));
    // 显示目标页面
    document.getElementById(pageId).classList.add('active');

    // 导航栏样式更新
    document.querySelectorAll('.nav-item').forEach(el => el.classList.remove('active'));
    if (navElement) {
        navElement.classList.add('active');
    }

    // 更新标题
    const titles = {
        'console': '总控制台',
        'observation': '集中观测',
        'single-view': '单点观测',
        'history': '数据回溯',
        'exam-mgmt': '考试管理',
        'user-mgmt': '用户管理',
        'node-mgmt': '节点管理',
        'room-mgmt': '教室管理'
    };
    document.getElementById('pageTitle').innerText = titles[pageId];

    // 逻辑：控制 Header 上的 Grid 控件显示与隐藏
    // 只有在 "集中观测" 页面显示，"单点观测" 不显示
    const gridControls = document.getElementById('grid-controls');
    if (pageId === 'observation') {
        gridControls.style.display = 'flex';
    } else {
        gridControls.style.display = 'none';
    }

    // 用户管理页面：获取用户列表
    if (pageId === 'user-mgmt') {
        fetchUsers();
    }

    // 节点管理页面：获取节点列表
    if (pageId === 'node-mgmt') {
        fetchNodes();
    }

    // 教室管理页面：获取教室列表
    if (pageId === 'room-mgmt') {
        fetchRooms();
    }

    // 总控制台页面：获取考试列表
    if (pageId === 'console') {
        fetchExamsForConsole();
    }

    // 集中观测页面：暂无初始逻辑
    if (pageId === 'observation') {
        // fetchExamsForObservation 被移除
    }

    // 数据回溯页面
    if (pageId === 'history') {
        populateHistoryFilters();
        fetchHistory();
    }

    if (pageId === 'exam-mgmt') {
        fetchExamManagement();
    }

    // 只有在控制台页面才调整图表大小
    if (pageId === 'console') {
        setTimeout(() => {
            if (chartMain) chartMain.resize();
            if (chartPie) chartPie.resize();
        }, 100);
    }
}

let allHistoryRooms = [];
const retryScheduleInFlight = new Set();

function formatScheduleStatus(status) {
    const mapping = {
        pending: { text: '待分配', color: '#9ca3af' },
        assigned: { text: '已分配待通知', color: '#06b6d4' },
        notified: { text: '已通知', color: '#22c55e' },
        running: { text: '进行中', color: '#10b981' },
        assign_failed: { text: '分配失败', color: '#ef4444' },
        notify_failed: { text: '通知失败', color: '#f59e0b' }
    };
    return mapping[status] || { text: status || '未知', color: '#9ca3af' };
}

function getExamMgmtState(exam) {
    if (exam?.end_time) return 'completed';
    const status = exam?.schedule_status;
    if (status === 'running') return 'started';
    if (status === 'pending') return 'unassigned';
    if (status === 'assign_failed' || status === 'notify_failed') return 'failed';
    if (status === 'assigned' || status === 'notified') return 'not_started';
    return 'not_started';
}

function formatExamMgmtState(state) {
    const mapping = {
        started: { text: '已开始', color: '#10b981' },
        not_started: { text: '未开始', color: '#06b6d4' },
        unassigned: { text: '未分配', color: '#9ca3af' },
        failed: { text: '分配失败', color: '#ef4444' }
    };
    return mapping[state] || { text: '未开始', color: '#06b6d4' };
}

function formatDurationMinutes(seconds) {
    const sec = Number(seconds || 0);
    if (sec <= 0) return '-';
    return Number.isInteger(sec / 60) ? String(sec / 60) : (sec / 60).toFixed(1);
}

function escapeHtml(text) {
    if (text === null || text === undefined) return '';
    return String(text)
        .replace(/&/g, '&amp;')
        .replace(/</g, '&lt;')
        .replace(/>/g, '&gt;')
        .replace(/"/g, '&quot;')
        .replace(/'/g, '&#039;');
}

function normalizeEntity(entity) {
    if (!entity || typeof entity !== 'object') return entity;

    const normalized = { ...entity };
    if (normalized.id === undefined && normalized.ID !== undefined) normalized.id = normalized.ID;
    if (normalized.created_at === undefined && normalized.CreatedAt !== undefined) normalized.created_at = normalized.CreatedAt;
    if (normalized.updated_at === undefined && normalized.UpdatedAt !== undefined) normalized.updated_at = normalized.UpdatedAt;
    if (normalized.start_time === undefined && normalized.StartTime !== undefined) normalized.start_time = normalized.StartTime;
    if (normalized.end_time === undefined && normalized.EndTime !== undefined) normalized.end_time = normalized.EndTime;
    if (normalized.last_heartbeat_at === undefined && normalized.LastHeartbeatAt !== undefined) normalized.last_heartbeat_at = normalized.LastHeartbeatAt;
    if (normalized.current_user_id === undefined && normalized.CurrentUserID !== undefined) normalized.current_user_id = normalized.CurrentUserID;
    if (normalized.current_exam_id === undefined && normalized.CurrentExamID !== undefined) normalized.current_exam_id = normalized.CurrentExamID;

    if (!normalized.room && normalized.Room) normalized.room = normalizeEntity(normalized.Room);
    if (!normalized.node && normalized.Node) normalized.node = normalizeEntity(normalized.Node);
    if (!normalized.user && normalized.User) normalized.user = normalizeEntity(normalized.User);

    if (normalized.room) normalized.room = normalizeEntity(normalized.room);
    if (normalized.node) normalized.node = normalizeEntity(normalized.node);
    if (normalized.user) normalized.user = normalizeEntity(normalized.user);

    return normalized;
}

function parseValidDate(timeStr) {
    if (!timeStr) return null;
    const date = new Date(timeStr);
    if (Number.isNaN(date.getTime())) return null;
    if (date.getUTCFullYear() <= 1971) return null;
    return date;
}

function formatDateTime(timeStr) {
    const date = parseValidDate(timeStr);
    return date ? date.toLocaleString() : '-';
}

function formatRoomTag(room) {
    if (!room) return '未知';
    const building = String(room.building || '').trim();
    const name = String(room.name || '').trim();
    if (building && name) return `${building}${name}`;
    if (name) return name;
    if (building) return building;
    return '未知';
}

function formatExamEndTime(exam) {
    if (exam?.end_time) {
        return formatDateTime(exam.end_time);
    }

    const startDate = parseValidDate(exam?.start_time);
    const durationSeconds = Number(exam?.duration_seconds || 0);
    if (!startDate || !Number.isFinite(durationSeconds) || durationSeconds <= 0) {
        return '-';
    }

    return formatDateTime(new Date(startDate.getTime() + durationSeconds * 1000));
}

function calculateDurationMinutes(startTime, endTime) {
    const start = parseValidDate(startTime);
    const end = parseValidDate(endTime);
    if (!start || !end) return '-';

    const diffMs = end.getTime() - start.getTime();
    if (diffMs < 0) return '-';

    const minutes = diffMs / 60000;
    return Number.isInteger(minutes) ? String(minutes) : minutes.toFixed(1);
}

function handleAuthFailure(response) {
    if (!response) return false;
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

async function requestJSON(url, options = {}) {
    const response = await fetch(url, options);
    if (handleAuthFailure(response)) {
        return { response, result: null, aborted: true };
    }
    const result = await parseJsonSafe(response);
    return { response, result, aborted: false };
}

function getNextMinuteDate(baseDate = new Date()) {
    const next = new Date(baseDate);
    next.setSeconds(0, 0);
    next.setMinutes(next.getMinutes() + 1);
    return next;
}

function toLocalDateTimeInputValue(date) {
    const year = date.getFullYear();
    const month = String(date.getMonth() + 1).padStart(2, '0');
    const day = String(date.getDate()).padStart(2, '0');
    const hours = String(date.getHours()).padStart(2, '0');
    const minutes = String(date.getMinutes()).padStart(2, '0');
    return `${year}-${month}-${day}T${hours}:${minutes}`;
}

async function fetchHistory() {
    try {
        const building = document.getElementById('history-building').value;
        const roomId = document.getElementById('history-room').value;
        const subject = document.getElementById('history-subject').value;
        const date = document.getElementById('history-date').value;

        // 构建请求参数
        const params = new URLSearchParams();
        if (date) params.append('date', date);
        if (building) params.append('building', building);
        if (roomId) params.append('room_id', roomId);
        if (subject) params.append('subject', subject);

        const { result, aborted } = await requestJSON(`/api/exams?${params.toString()}`);
        if (aborted || !result) return;
        const exams = (result.data || []).map(normalizeEntity).filter(e => !!e.end_time);

        const tbody = document.querySelector('#history tbody');
        tbody.innerHTML = '';

        if (exams.length === 0) {
            tbody.innerHTML = '<tr><td colspan="9" style="text-align: center; color: var(--text-muted);">暂无已结束考试记录</td></tr>';
            return;
        }

        // 并发获取异常数量，单条失败不影响整体渲染。
        const alertResponses = await Promise.allSettled(
            exams.map(e => requestJSON(`/api/alerts?exam_id=${e.id}`))
        );

        for (let i = 0; i < exams.length; i++) {
            const e = exams[i];
            let anomalyCount = 0;
            const durationMinutes = calculateDurationMinutes(e.start_time, e.end_time);

            const settled = alertResponses[i];
            if (settled.status === 'fulfilled' && settled.value && !settled.value.aborted) {
                const alertResult = settled.value.result || {};
                anomalyCount = alertResult.data ? alertResult.data.length : 0;
            }

            const tr = `
                <tr>
                    <td>${i + 1}</td>
                    <td>${formatDateTime(e.start_time)}</td>
                    <td>${durationMinutes}</td>
                    <td>${e.subject || '未知'}</td>
                    <td>${e.room?.building || '-'}</td>
                    <td>${e.room ? e.room.name : '未知'}</td>
                    <td class="${anomalyCount > 0 ? 'text-danger' : 'text-success'}">${anomalyCount}</td>
                    <td><button onclick="viewExamAnomalies(${e.id})" style="padding: 4px 8px; font-size: 12px;">查看异常</button></td>
                    <td>
                        <button class="btn-table btn-delete" onclick="deleteExam(${e.id})">
                            <i class="fa-solid fa-trash"></i> 删除
                        </button>
                    </td>
                </tr>
            `;
            tbody.innerHTML += tr;
        }
    } catch (e) {
        console.error("获取记录失败", e);
    }
}

async function fetchExamManagement() {
    try {
        const subject = document.getElementById('exam-mgmt-subject')?.value || '';
        const status = document.getElementById('exam-mgmt-status')?.value || '';
        const date = document.getElementById('exam-mgmt-date')?.value || '';

        const params = new URLSearchParams();
        if (subject) params.append('subject', subject);
        if (date) params.append('date', date);

        const req = await requestJSON(`/api/exams?${params.toString()}`);
        if (req.aborted || !req.result) return;
        let exams = (req.result.data || []).map(normalizeEntity).filter(e => !e.end_time);

        if (status) {
            exams = exams.filter(e => getExamMgmtState(e) === status);
        }

        const tbody = document.getElementById('exam-mgmt-body');
        if (!tbody) return;
        tbody.innerHTML = '';

        if (exams.length === 0) {
            tbody.innerHTML = '<tr><td colspan="12" style="text-align: center; color: var(--text-muted);">暂无考试数据</td></tr>';
            return;
        }

        exams.forEach((e, idx) => {
            const mgmtState = getExamMgmtState(e);
            const schedule = formatExamMgmtState(mgmtState);
            const canRetry = ['unassigned', 'not_started', 'failed'].includes(mgmtState);
            const canDelete = true;
            const tr = `
                <tr>
                    <td>${idx + 1}</td>
                    <td>${escapeHtml(e.name || '-')}</td>
                    <td>${escapeHtml(e.subject || '-')}</td>
                    <td>${escapeHtml(e.room?.building || '-')}</td>
                    <td>${escapeHtml(e.room?.name || '-')}</td>
                    <td>${escapeHtml(e.user?.username || '-')}</td>
                    <td>${formatDateTime(e.start_time)}</td>
                    <td>${formatDurationMinutes(e.duration_seconds)}</td>
                    <td>${escapeHtml(e.node?.name || '-')}</td>
                    <td><span style="display:inline-block;padding:2px 8px;border-radius:4px;font-size:12px;background:${schedule.color}22;color:${schedule.color};border:1px solid ${schedule.color}55;">${schedule.text}</span></td>
                    <td title="${escapeHtml(e.schedule_error || '')}" style="max-width: 240px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis;">${escapeHtml(e.schedule_error || '-')}</td>
                    <td>
                        ${canRetry ? `<button class="btn-table" onclick="retryScheduleExam(${e.id})" style="background: var(--warning-color); color: white; margin-right: 6px;"><i class="fa-solid fa-rotate-right"></i> 重试</button>` : ''}
                        ${canDelete ? `<button class="btn-table btn-delete" onclick="deleteExam(${e.id})"><i class="fa-solid fa-trash"></i> 删除</button>` : ''}
                    </td>
                </tr>
            `;
            tbody.innerHTML += tr;
        });
    } catch (e) {
        console.error('Fetch exam management failed', e);
    }
}

async function retryScheduleExam(examId) {
    if (retryScheduleInFlight.has(examId)) {
        alert('该考试正在重试中，请稍候');
        return;
    }

    if (!confirm('确定要重试该考试的分配与开考通知吗？')) return;
    retryScheduleInFlight.add(examId);

    try {
        const { response, result, aborted } = await requestJSON(`/api/exams/${examId}/retry-schedule`, { method: 'POST' });
        if (aborted || !result) return;

        if (response.ok && result.success) {
            alert('重试调度成功');
            fetchHistory();
            fetchExamManagement();
            fetchExamsForConsole();
        } else {
            alert(result.error || '重试调度失败');
        }
    } catch (e) {
        console.error('Retry schedule error', e);
        alert('网络请求出错');
    } finally {
        retryScheduleInFlight.delete(examId);
    }
}

async function loadExamFormOptions() {
    const roomSelect = document.getElementById('modalExamRoom');
    const userSelect = document.getElementById('modalExamUser');
    const nodeSelect = document.getElementById('modalExamNode');

    roomSelect.innerHTML = '<option value="">加载中...</option>';
    userSelect.innerHTML = '<option value="">加载中...</option>';
    nodeSelect.innerHTML = '<option value="">加载中...</option>';

    try {
        const [roomsReq, usersReq, nodesReq] = await Promise.all([
            requestJSON('/api/rooms'),
            requestJSON('/api/users'),
            requestJSON('/api/nodes')
        ]);

        if (roomsReq.aborted || usersReq.aborted || nodesReq.aborted) return;

        const roomsResult = roomsReq.result || {};
        const usersResult = usersReq.result || {};
        const nodesResult = nodesReq.result || {};

        const rooms = (roomsResult.data || []).map(normalizeEntity);
        const users = (usersResult.data || []).map(normalizeEntity);
        const nodes = (nodesResult.data || []).map(normalizeEntity);

        roomSelect.innerHTML = rooms.length
            ? rooms.map(r => `<option value="${r.id}">${escapeHtml(r.building)} / ${escapeHtml(r.name)}</option>`).join('')
            : '<option value="">暂无教室</option>';

        userSelect.innerHTML = users.length
            ? users.map(u => `<option value="${u.id}">${escapeHtml(u.username)} (${u.role === 'admin' ? '管理员' : '监考员'})</option>`).join('')
            : '<option value="">暂无用户</option>';

        const availableNodes = nodes.filter(n => n.status === 'idle' && !n.current_user_id);
        nodeSelect.innerHTML = `<option value="">到点自动分配</option>${availableNodes
            .map(n => `<option value="${n.id}">${escapeHtml(n.name)} (${escapeHtml(n.address || '-')})</option>`)
            .join('')}`;
    } catch (e) {
        console.error('Load exam form options failed', e);
        roomSelect.innerHTML = '<option value="">加载失败</option>';
        userSelect.innerHTML = '<option value="">加载失败</option>';
        nodeSelect.innerHTML = '<option value="">加载失败</option>';
    }
}

function openExamModal() {
    const startInput = document.getElementById('modalExamStartTime');
    const minStart = getNextMinuteDate();
    const minStartValue = toLocalDateTimeInputValue(minStart);

    document.getElementById('modalExamName').value = '';
    document.getElementById('modalExamSubject').value = '';
    startInput.min = minStartValue;
    startInput.step = 60;
    startInput.value = minStartValue;
    document.getElementById('modalExamDurationMinutes').value = '120';

    loadExamFormOptions();
    document.getElementById('examModal').style.display = 'flex';
}

function closeExamModal() {
    document.getElementById('examModal').style.display = 'none';
}

async function submitExam() {
    const name = document.getElementById('modalExamName').value.trim();
    const subject = document.getElementById('modalExamSubject').value.trim();
    const startTime = document.getElementById('modalExamStartTime').value;
    const durationMinutes = Number(document.getElementById('modalExamDurationMinutes').value);
    const roomId = Number(document.getElementById('modalExamRoom').value);
    const userId = Number(document.getElementById('modalExamUser').value);
    const nodeVal = document.getElementById('modalExamNode').value;

    if (!subject || !startTime || !durationMinutes || !roomId || !userId) {
        alert('请填写完整的必填项');
        return;
    }

    const selectedStart = new Date(startTime);
    const minStart = getNextMinuteDate();
    if (Number.isNaN(selectedStart.getTime()) || selectedStart < minStart) {
        alert('开始时间最早只能选择当前时刻的下一分钟');
        return;
    }

    const payload = {
        name,
        subject,
        room_id: roomId,
        user_id: userId,
        start_time: new Date(startTime).toISOString(),
        duration_seconds: Math.round(durationMinutes * 60)
    };
    if (nodeVal) {
        payload.node_id = Number(nodeVal);
    }

    try {
        const { response, result, aborted } = await requestJSON('/api/exams', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(payload)
        });
        if (aborted || !result) return;

        if (response.ok && result.success) {
            alert('考试创建成功');
            closeExamModal();
            fetchHistory();
            fetchExamManagement();
        } else {
            alert(result.error || '创建考试失败');
        }
    } catch (e) {
        console.error('Create exam failed', e);
        alert('网络请求出错');
    }
}

// 查看考试的异常记录
async function viewExamAnomalies(examId) {
    try {
        const { result, aborted } = await requestJSON(`/api/alerts?exam_id=${examId}`);
        if (aborted || !result) return;
        const alerts = (result.data || []).map(normalizeEntity);

        // 检查是否已经存在异常弹窗
        const existingModal = document.getElementById('anomaly-modal');

        if (alerts.length === 0) {
            if (existingModal) {
                existingModal.remove();
            }
            alert('该考试没有异常记录');
            return;
        }

        // 创建一个弹窗显示异常记录
        let alertsHTML = `<div style="max-height: 500px; overflow-y: auto;">
            <h3>考试异常记录 (EXP-${examId})</h3>
            <table style="width: 100%; margin-top: 10px;">
                <thead>
                    <tr>
                        <th>时间</th>
                        <th>座位号</th>
                        <th>异常类型</th>
                        <th>消息</th>
                        <th>操作</th>
                    </tr>
                </thead>
                <tbody>`;

        const typeNames = {
            'phone_cheating': '手机作弊',
            'look_around': '东张西望',
            'whispering': '交头接耳',
            'leave_sheet': '离开答题卡',
            'stand_up': '站立',
            'other': '其他异常'
        };

        alerts.forEach(alert => {
            let picPath = alert.picture_path || '';
            if (picPath.startsWith('.')) {
                picPath = picPath.substring(1);
            }
            if (picPath && !picPath.startsWith('/')) {
                picPath = '/' + picPath;
            }

            alertsHTML += `
                <tr>
                    <td>${formatDateTime(alert.created_at)}</td>
                    <td>${alert.seat_number}</td>
                    <td>${typeNames[alert.type] || alert.type}</td>
                    <td>${alert.message}</td>
                    <td>
                        ${picPath ? `<button onclick="window.open('${picPath}', '_blank')" style="font-size: 11px; padding: 3px 6px;">查看图片</button>` : '-'}
                        <button class="btn-table btn-delete" onclick="deleteAlert(${alert.id}, ${examId})" style="font-size: 11px; padding: 3px 6px; margin-left: 5px;">删除</button>
                    </td>
                </tr>
            `;
        });

        alertsHTML += `</tbody></table></div>`;

        const modalContent = `
            <div style="background: #1f2937; padding: 20px; border-radius: 8px; max-width: 900px; width: 90%; color: white;">
                ${alertsHTML}
                <button onclick="document.getElementById('anomaly-modal').remove()" style="margin-top: 15px; padding: 8px 16px; background: var(--accent-color); color: white; border: none; border-radius: 4px; cursor: pointer;">关闭</button>
            </div>
        `;

        if (existingModal) {
            existingModal.innerHTML = modalContent;
        } else {
            // 简单的弹窗显示（实际项目中建议使用更好的弹窗组件）
            const modalDiv = document.createElement('div');
            modalDiv.id = 'anomaly-modal';
            modalDiv.style.cssText = 'position: fixed; top: 0; left: 0; width: 100%; height: 100%; background: rgba(0,0,0,0.7); display: flex; align-items: center; justify-content: center; z-index: 10000;';
            modalDiv.innerHTML = modalContent;
            document.body.appendChild(modalDiv);
        }
    } catch (e) {
        console.error("获取异常记录失败", e);
        alert('获取异常记录失败');
    }
}

// 删除考试记录
async function deleteExam(examId) {
    if (!confirm('确定要删除这条考试记录吗？相关的异常记录也会被删除。')) return;

    try {
        const { response, result, aborted } = await requestJSON(`/api/exams/${examId}`, { method: 'DELETE' });
        if (aborted || !result) return;

        if (response.ok && result.success) {
            alert('删除成功');
            fetchHistory();
            fetchExamManagement();
        } else {
            alert(result.error || '删除失败');
        }
    } catch (err) {
        alert('网络请求出错');
        console.error(err);
    }
}

// 删除异常记录
async function deleteAlert(alertId, examId) {
    if (!confirm('确定要删除这条异常记录吗？')) return;

    try {
        const { response, result, aborted } = await requestJSON(`/api/alerts/${alertId}`, { method: 'DELETE' });
        if (aborted || !result) return;

        if (response.ok && result.success) {
            alert('删除成功');
            // 重新显示该考试的异常记录
            viewExamAnomalies(examId);
            // 刷新历史记录列表
            fetchHistory();
        } else {
            alert(result.error || '删除失败');
        }
    } catch (err) {
        alert('网络请求出错');
        console.error(err);
    }
}

async function populateHistoryFilters() {
    try {
        // 1. 抓取所有教室数据
        const { result: roomsResult, aborted } = await requestJSON('/api/rooms');
        if (aborted || !roomsResult) return;
        allHistoryRooms = (roomsResult.data || []).map(normalizeEntity);

        // 2. 提取并填充楼宇
        const buildings = [...new Set(allHistoryRooms.map(r => r.building))];
        const buildSelect = document.getElementById('history-building');
        const currentBuildVal = buildSelect.value;
        buildSelect.innerHTML = '<option value="">全部楼宇</option>';
        buildings.forEach(b => {
            const opt = document.createElement('option');
            opt.value = b;
            opt.textContent = b;
            buildSelect.appendChild(opt);
        });
        buildSelect.value = currentBuildVal;

        // 3. 触发一次教室下拉框同步
        updateRoomDropdown();
    } catch (e) {
        console.error("初始化筛选器失败", e);
    }
}

function updateRoomDropdown() {
    const building = document.getElementById('history-building').value;
    const roomSelect = document.getElementById('history-room');
    const currentRoomVal = roomSelect.value;

    roomSelect.innerHTML = '<option value="">全部教室</option>';

    // 过滤出该楼宇下的教室
    const filteredRooms = building
        ? allHistoryRooms.filter(r => r.building === building)
        : allHistoryRooms;

    filteredRooms.forEach(r => {
        const opt = document.createElement('option');
        opt.value = r.id;
        opt.textContent = r.name;
        roomSelect.appendChild(opt);
    });

    // 尝试恢复之前选中的值（如果该教室依然存在于列表中）
    roomSelect.value = filteredRooms.find(r => r.id == currentRoomVal) ? currentRoomVal : "";
}

// --- 侧边栏折叠功能 ---
function toggleSidebar() {
    const sidebar = document.getElementById('sidebar');
    sidebar.classList.toggle('collapsed');

    // 调整图表大小以适应新的布局
    setTimeout(() => {
        chartMain.resize();
        chartPie.resize();
    }, 300);
}

// --- 2. 页面 1：ECharts 初始化与数据源 ---
var chartMain = null;
var chartPie = null;

// 安全初始化图表
const chartMainEl = document.getElementById('chart-main');
const chartPieEl = document.getElementById('chart-pie');
if (chartMainEl) {
    chartMain = echarts.init(chartMainEl);
}
if (chartPieEl) {
    chartPie = echarts.init(chartPieEl);
}

// 柱状图配置
const optionMain = {
    backgroundColor: 'transparent',
    title: { text: '各考场异常数量监控', textStyle: { color: '#fff', fontSize: 14 } },
    tooltip: { trigger: 'axis', axisPointer: { type: 'shadow' } },
    legend: { show: false },
    grid: { top: 40, right: 20, bottom: 40, left: 40 },
    xAxis: {
        type: 'category',
        data: [],
        axisLine: { lineStyle: { color: '#4b5563' } },
        axisLabel: { color: '#9ca3af' }
    },
    yAxis: {
        type: 'value',
        name: '',
        splitLine: { lineStyle: { color: 'rgba(255,255,255,0.1)' } },
        axisLine: { lineStyle: { color: '#4b5563' } }
    },
    series: [
        {
            name: '异常数量',
            type: 'bar',
            barWidth: 30,
            itemStyle: {
                color: new echarts.graphic.LinearGradient(0, 0, 0, 1, [
                    { offset: 0, color: '#06b6d4' },
                    { offset: 1, color: '#0891b2' }
                ])
            },
            data: []
        }
    ]
};

// 饼图配置
const optionPie = {
    backgroundColor: 'transparent',
    title: { text: '考场异常分布占比', textStyle: { color: '#fff', fontSize: 14 } },
    tooltip: { trigger: 'item', formatter: '{b}: {c} ({d}%)' },
    legend: {
        type: 'scroll',
        bottom: 0,
        textStyle: { color: '#9ca3af', fontSize: 10 }
    },
    series: [
        {
            name: '考场异常数',
            type: 'pie',
            radius: ['40%', '65%'],
            center: ['50%', '50%'],
            itemStyle: { borderRadius: 5, borderColor: '#1f2937', borderWidth: 2 },
            label: { show: false },
            data: []
        }
    ]
};

if (chartMain) chartMain.setOption(optionMain);
if (chartPie) chartPie.setOption(optionPie);

// --- 3. 动态刷新逻辑 ---
async function refreshData() {
    try {
        // 使用新的统计接口
        const { result, aborted } = await requestJSON('/api/exams/stats');
        if (aborted || !result) return;

        if (!result.success) {
            console.error("获取统计数据失败");
            return;
        }

        const data = result.data;
        const exams = (data.ongoing_exams || []).map(normalizeEntity);

        // 1. 更新统计卡片（添加安全检查）
        const statRooms = document.getElementById('stat-rooms');
        const statStudents = document.getElementById('stat-students');
        const statAnomalies = document.getElementById('stat-anomalies');
        const statCoeff = document.getElementById('stat-coeff');

        if (statRooms) statRooms.innerText = data.total_rooms;
        if (statStudents) statStudents.innerText = data.total_students;
        if (statAnomalies) statAnomalies.innerText = data.total_anomalies;
        if (statCoeff) statCoeff.innerText = data.anomaly_coeff.toFixed(3);

        // 2. 准备图表数据
        const roomNames = exams.map(e => {
            if (!e.room) return '未知';
            return e.room.building ? `${e.room.building}-${e.room.name}` : e.room.name;
        });
        const anomalyCounts = exams.map(e => e.anomalies_count || 0);

        const pieData = exams.map(e => ({
            value: e.anomalies_count || 0,
            name: e.room ? (e.room.building ? `${e.room.building}-${e.room.name}` : e.room.name) : '未知'
        })).filter(item => item.value > 0);

        if (pieData.length === 0) {
            pieData.push({ value: 0, name: '无异常', itemStyle: { color: '#10b981' } });
        }

        // 3. 更新柱状图（添加安全检查）
        if (chartMain) {
            chartMain.setOption({
                xAxis: { data: roomNames },
                series: [{ data: anomalyCounts }]
            });
        }

        // 4. 更新饼图（添加安全检查）
        if (chartPie) {
            chartPie.setOption({
                series: [{ data: pieData }]
            });
        }

        // 5. 更新表格
        const tbody = document.getElementById('exam-list-body');
        if (tbody && document.getElementById('console').classList.contains('active')) {
            tbody.innerHTML = '';
            if (exams.length === 0) {
                tbody.innerHTML = '<tr><td colspan="10" style="text-align: center; color: #9ca3af;">暂无正在进行的考试</td></tr>';
            } else {
                exams.forEach((e, index) => {
                    const tr = `
                    <tr>
                        <td>${index + 1}</td>
                        <td>${e.subject || '未知'}</td>
                        <td>${e.room && e.room.building ? e.room.building : '-'}</td>
                        <td>${e.room ? e.room.name : '未知'}</td>
                        <td>${e.node ? e.node.name : '未知'}</td>
                        <td>${formatDateTime(e.start_time)}</td>
                        <td>${formatExamEndTime(e)}</td>
                        <td>${e.examinee_count || 0}</td>
                        <td><span style="color: ${e.anomalies_count > 0 ? '#ef4444' : '#10b981'}">${e.anomalies_count || 0}</span></td>
                        <td>
                            <button class="btn-table" onclick="observeExam(${e.id})" style="background: var(--accent-color); color: white; font-size: 12px; padding: 4px 8px; margin-right: 6px;">
                                <i class="fa-solid fa-eye"></i> 查看
                            </button>
                            <button class="btn-table" onclick="viewExamAnomalies(${e.id})" style="background: var(--warning-color); color: white; font-size: 12px; padding: 4px 8px;">
                                <i class="fa-solid fa-triangle-exclamation"></i> 异常
                            </button>
                        </td>
                    </tr>
                `;
                    tbody.innerHTML += tr;
                });
            }
        }
    } catch (e) {
        console.error("刷新控制台数据失败", e);
    }
}

// 按钮点击事件处理
function viewExamDetails(examId) {
    // 跳转到数据回溯页面并查看该考试的异常
    switchTab('history', document.querySelectorAll('.nav-item')[3]);
    // 可以添加额外的过滤逻辑
}

async function endExam(examId) {
    if (!confirm('确定要结束这场考试吗？')) return;

    try {
        const { response, result, aborted } = await requestJSON(`/api/exams/${examId}/end`, { method: 'POST' });
        if (aborted || !result) return;

        if (response.ok && result.success) {
            alert('考试已结束');
            refreshData();
        } else {
            alert(result.error || '操作失败');
        }
    } catch (e) {
        console.error('结束考试失败', e);
        alert('网络请求出错');
    }
}

function viewAnomaly(examId) {
    alert(`正在调取考试 ${examId} 的异常截图...`);
}

function viewLive(examId) {
    alert(`正在切换至考试 ${examId} 的实时直播流...`);
    // 这里可以添加逻辑自动切换到集中观测tab并高亮该考场
}

function disconnectRoom(examId) {
    endExam(examId);
}

setInterval(refreshData, 3000);
refreshData();

// --- 4. 页面 2：网格生成逻辑 ---
function updateGrid() {
    const rows = document.getElementById('grid-rows').value;
    const cols = document.getElementById('grid-cols').value;
    const container = document.getElementById('monitor-container');

    // 设置 CSS Grid
    container.style.gridTemplateColumns = `repeat(${cols}, 1fr)`;
    container.style.gridTemplateRows = `repeat(${rows}, 1fr)`;

    // 生成格子
    const total = rows * cols;
    let html = '';
    for (let i = 0; i < total; i++) {
        html += `
                    <div class="monitor-screen add-btn" onclick="addExam(${i})">
                        <i class="fa-solid fa-plus"></i>
                    </div>
                `;
    }
    container.innerHTML = html;
}

let currentTargetBox = null;

function addExam(index, isSingle = false) {
    currentTargetBox = { index, isSingle };
    const modal = document.getElementById('streamSelectionModal');
    modal.style.display = 'flex';
    loadOngoingExamsForSelection();
}

function closeStreamSelectionModal() {
    document.getElementById('streamSelectionModal').style.display = 'none';
    currentTargetBox = null;
}

async function loadOngoingExamsForSelection() {
    const container = document.getElementById('ongoing-exams-list');
    container.innerHTML = '<div style="text-align: center; color: #9ca3af; padding: 20px;">正在加载正在进行的考试...</div>';

    try {
        const { result, aborted } = await requestJSON('/api/exams/stats');
        if (aborted || !result) return;

        if (!result.success) {
            container.innerHTML = '<div style="text-align: center; color: #ef4444; padding: 20px;">加载失败</div>';
            return;
        }

        const exams = (result.data.ongoing_exams || []).map(normalizeEntity);
        if (exams.length === 0) {
            container.innerHTML = '<div style="text-align: center; color: #9ca3af; padding: 20px;">当前没有正在进行的考试</div>';
            return;
        }

        container.innerHTML = exams.map(exam => `
            <div class="selection-item" onclick="selectStream(${exam.id})" style="padding: 15px; border-bottom: 1px solid #374151; cursor: pointer; display: flex; justify-content: space-between; align-items: center;">
                <div>
                    <div style="font-weight: bold; color: #fff;">${exam.subject}</div>
                    <div style="font-size: 13px; color: #9ca3af; margin-top: 4px;">
                        <i class="fa-solid fa-school"></i> ${formatRoomTag(exam.room)} 
                        | <i class="fa-solid fa-server"></i> ${exam.node ? exam.node.name : (exam.Node ? exam.Node.name : '未知')}
                    </div>
                </div>
                <i class="fa-solid fa-chevron-right" style="color: var(--accent-color);"></i>
            </div>
        `).join('');

        // 简易悬停样式 (如果 CSS 没有定义)
        const items = container.querySelectorAll('.selection-item');
        items.forEach(item => {
            item.onmouseover = () => item.style.backgroundColor = '#374151';
            item.onmouseout = () => item.style.backgroundColor = 'transparent';
        });

    } catch (e) {
        console.error("加载考试列表失败", e);
        container.innerHTML = '<div style="text-align: center; color: #ef4444; padding: 20px;">网络请求出错</div>';
    }
}

async function selectStream(examId) {
    if (!currentTargetBox) return;

    try {
        const { result, aborted } = await requestJSON('/api/exams');
        if (aborted || !result) return;
        const exams = (result.data || []).map(normalizeEntity);
        const exam = exams.find(e => e.id == examId);

        if (!exam || !exam.node) {
            alert('找不到节点信息');
            return;
        }

        const nodeToken = exam.node.token;
        const nodeAddress = exam.node.address;
        const streamUrl = `http://${nodeAddress}/stream?token=${nodeToken}`;

        const { index, isSingle } = currentTargetBox;
        let targetElement;

        if (isSingle) {
            targetElement = document.querySelector('#single-view .monitor-grid .monitor-screen');
        } else {
            targetElement = document.getElementById('monitor-container').children[index];
        }

        if (targetElement) {
            targetElement.classList.remove('add-btn');
            // 仍然保留点击事件，方便再次切换
            targetElement.onclick = () => addExam(index, isSingle);

            targetElement.innerHTML = `
                <div style="width: 100%; height: 100%; position: relative; pointer-events: none; background: #000; border-radius: 8px; overflow: hidden;">
                    <img src="${streamUrl}" style="width: 100%; height: 100%; object-fit: contain; pointer-events: auto;">
                    <div style="position: absolute; bottom: 0; left: 0; right: 0; background: rgba(0,0,0,0.6); padding: 4px 8px; font-size: 11px; color: white; display: flex; justify-content: space-between; align-items: center; pointer-events: auto;">
                        <span>${formatRoomTag(exam.room)} - ${exam.subject}</span>
                        <i class="fa-solid fa-xmark reset-box" onclick="resetBox(event, ${index}, ${isSingle})" style="cursor: pointer; padding: 2px 5px;"></i>
                    </div>
                </div>
            `;
        }

        closeStreamSelectionModal();
    } catch (err) {
        console.error('Select stream error:', err);
        alert('加载考试流失败');
    }
}

function resetBox(event, index, isSingle) {
    if (event) event.stopPropagation();

    let targetElement;
    if (isSingle) {
        targetElement = document.querySelector('#single-view .monitor-grid .monitor-screen');
    } else {
        targetElement = document.getElementById('monitor-container').children[index];
    }

    if (targetElement) {
        targetElement.classList.add('add-btn');
        targetElement.onclick = () => addExam(index, isSingle);
        targetElement.innerHTML = '<i class="fa-solid fa-plus"></i>';
    }
}

// 初始化网格
updateGrid();

// --- 5. 时钟逻辑 ---
function updateClock() {
    const now = new Date();
    document.getElementById('clock').innerText = now.toLocaleTimeString();
}
updateClock(); // 立即运行一次
setInterval(updateClock, 1000);

window.addEventListener('resize', () => {
    chartMain.resize();
    chartPie.resize();
});

// --- 6. 全屏功能 (新增) ---
function toggleFullScreen() {
    if (!document.fullscreenElement) {
        document.documentElement.requestFullscreen().catch(err => {
            console.error(`Error attempting to enable full-screen mode: ${err.message} (${err.name})`);
        });
    } else {
        if (document.exitFullscreen) {
            document.exitFullscreen();
        }
    }
}

// 监听全屏变化，更新图标
document.addEventListener('fullscreenchange', () => {
    const icon = document.getElementById('fs-icon');
    if (document.fullscreenElement) {
        icon.classList.remove('fa-expand');
        icon.classList.add('fa-compress');
    } else {
        icon.classList.remove('fa-compress');
        icon.classList.add('fa-expand');
    }
});

// --- 7. 后台交互与数据初始化 (新增) ---
// 获取用户信息并更新侧边栏
async function fetchUserInfo() {
    try {
        const { result, aborted } = await requestJSON('/api/me');
        if (aborted || !result || !result.username) return;
        document.getElementById('sidebarUsername').innerText = result.username;
        document.getElementById('sidebarAvatar').innerText = result.username.charAt(0).toUpperCase();
    } catch (err) {
        console.error('Fetch current user info failed', err);
    }
}

// 退出登录
async function logout() {
    if (confirm("确定要退出登录吗？")) {
        const response = await fetch('/logout');
        const result = await response.json();
        if (result.success) {
            window.location.href = result.redirect;
        }
    }
}

// --- 用户管理逻辑 ---
async function fetchUsers() {
    try {
        const { response, result, aborted } = await requestJSON('/api/users'); // 匹配 main.go
        if (aborted || !result) return;
        if (!response.ok) throw new Error('无法获取用户列表');
        const users = (result.data || []).map(normalizeEntity); // 兼容 id/ID 等字段

        const tbody = document.getElementById('user-list-body');
        tbody.innerHTML = '';
        users.forEach((user, idx) => {
            const tr = document.createElement('tr');
            // 注意：Go 返回的 JSON 字段名通常是大写开头的（Username, Role, ID, CreatedAt）
            tr.innerHTML = `
            <td>${idx + 1}</td>
                <td>${user.username}</td>
                <td><span class="badge ${user.role === 'admin' ? 'bg-danger' : 'bg-primary'}">${user.role === 'admin' ? '管理员' : '监考员'}</span></td>
                <td>${formatDateTime(user.created_at)}</td>
                <td>
                    <div style="display: flex; gap: 5px;">
                        <button class="btn-table" onclick="openUserModal('edit', ${JSON.stringify(user).replace(/"/g, '&quot;')})" style="background: var(--accent-color); color: white;">
                            <i class="fa-solid fa-pen-to-square"></i> 编辑
                        </button>
                        ${user.username !== 'admin' ? `
                            <button class="btn-table btn-delete" onclick="deleteUser(${user.id}, '${user.username}')">
                                <i class="fa-solid fa-trash"></i> 删除
                            </button>
                        ` : ''}
                    </div>
                </td>
            `;
            tbody.appendChild(tr);
        });
    } catch (err) {
        console.error('Fetch users error:', err);
    }
}

// --- 统一弹窗逻辑 (新增/编辑) ---
function openUserModal(mode, userData = null) {
    const modal = document.getElementById('userModal');
    const title = document.getElementById('userModalTitle');
    const submitBtn = document.getElementById('modalSubmitBtn');
    const note = document.getElementById('modalNote');
    const pwdLabel = document.getElementById('modalPasswordLabel');

    // 清空表单
    document.getElementById('modalUserId').value = '';
    document.getElementById('modalUsername').value = '';
    document.getElementById('modalPassword').value = '';
    document.getElementById('modalRole').value = 'proctor';

    if (mode === 'add') {
        title.innerText = '新增考场管理人员';
        submitBtn.innerText = '确认添加';
        pwdLabel.innerText = '初始密码';
        note.style.display = 'none';
        document.getElementById('modalUsername').disabled = false;
        document.getElementById('modalUsername').style.opacity = '1';
    } else {
        title.innerText = '编辑用户信息';
        submitBtn.innerText = '保存修改';
        pwdLabel.innerText = '修改密码';
        note.innerText = '管理员拥有最高权限，可以重置该用户的用户名和密码。留空密码则不修改。';
        note.style.display = 'block';

        // 填充数据
        document.getElementById('modalUserId').value = userData.id;
        document.getElementById('modalUsername').value = userData.username;
        document.getElementById('modalRole').value = userData.role;
    }

    modal.style.display = 'flex';
}

function closeUserModal() {
    document.getElementById('userModal').style.display = 'none';
}

async function submitUser() {
    const id = document.getElementById('modalUserId').value;
    const username = document.getElementById('modalUsername').value;
    const password = document.getElementById('modalPassword').value;
    const role = document.getElementById('modalRole').value;

    if (!username || (!id && !password)) {
        alert("请填写完整信息");
        return;
    }

    const isEdit = id !== '';
    const url = isEdit ? `/api/users/${id}` : '/api/users';
    const method = isEdit ? 'PUT' : 'POST';

    const body = { username, role };
    if (password.trim() !== '') {
        body.password = password;
    }

    try {
        const response = await fetch(url, {
            method: method,
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(body)
        });

        const result = await response.json();

        if (response.ok && result.success) {
            alert(isEdit ? '修改成功' : '添加用户成功');
            closeUserModal();
            fetchUsers();
        } else {
            // 这里会显示后端返回的具体错误，比如 "用户名已存在"
            alert(result.error || (isEdit ? '修改失败' : '添加失败'));
        }
    } catch (err) {
        alert('网络请求出错');
    }
}

async function deleteUser(id, username) {
    if (!confirm(`确定要删除用户 "${username}" 吗？`)) return;

    try {
        const { response, result, aborted } = await requestJSON(`/api/users/${id}`, { method: 'DELETE' });
        if (aborted || !result) return;

        if (response.ok && result.success) {
            alert('删除成功');
            fetchUsers();
        } else {
            alert(result.error || '删除失败');
        }
    } catch (err) {
        alert('网络请求出错');
    }
}

async function changePassword() {
    const oldPass = document.getElementById('oldPassword').value;
    const newPass = document.getElementById('newPassword').value;
    const confirmPass = document.getElementById('confirmPassword').value;

    if (!oldPass || !newPass) {
        alert("请填写完整信息");
        return;
    }

    if (newPass !== confirmPass) {
        alert("两次输入的新密码不一致");
        return;
    }

    try {
        const { response, result, aborted } = await requestJSON('/api/users/password', {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ old_password: oldPass, new_password: newPass })
        });
        if (aborted || !result) return;
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

// 初始化
fetchUserInfo();
fetchRooms(); // 初始获取数据
fetchExamsForConsole(); // 初始获取考试数据（总控制台）
setInterval(fetchRooms, 10000); // 10秒同步一次考场数据
setInterval(fetchNodes, 10000); // 10秒同步一次节点数据

// --- 节点管理逻辑 ---
async function fetchNodes() {
    try {
        const { response, result, aborted } = await requestJSON('/api/nodes');
        if (aborted || !result) return;
        if (!response.ok) throw new Error('无法获取节点列表');
        const nodes = (result.data || []).map(normalizeEntity);

        // 获取精确统计数据
        const { result: statsResult, aborted: statsAborted } = await requestJSON('/api/nodes/stats');
        if (statsAborted || !statsResult) return;
        const stats = statsResult.data || {};

        document.getElementById('node-online-count').innerText = stats.online || 0;
        document.getElementById('node-using-count').innerText = stats.occupied || 0;
        document.getElementById('node-offline-count').innerText = stats.offline || 0;
        document.getElementById('node-total-count').innerText = stats.total || 0;

        // 渲染表格
        const tbody = document.getElementById('node-list-body');
        tbody.innerHTML = '';
        nodes.forEach((node, idx) => {
            const isOccupied = !!node.current_user_id || !!node.current_exam_id || node.status === 'busy';
            // 细化状态显示
            let statusClass = 'bg-primary';
            let statusText = '空闲可用';

            if (node.status === 'offline') {
                statusClass = 'bg-danger';
                statusText = '离线';
            } else if (node.status === 'error') {
                statusClass = 'bg-danger';
                statusText = '故障';
            } else if (node.status === 'busy') {
                statusClass = 'bg-warning';
                statusText = '正在监考';
            } else if (isOccupied && node.status === 'idle') {
                statusClass = 'bg-info';
                statusText = '已占用(未开始)';
            }

            const tr = document.createElement('tr');
            tr.innerHTML = `
                <td>${idx + 1}</td>
                <td>
                    <a href="#" onclick="jumpToNode(${node.id}); return false;" style="color: var(--accent-color); text-decoration: none;">
                        ${node.name}
                    </a>
                </td>
                <td>${node.nodemodel || '-'}</td>
                <td>${node.address}</td>
                <td><span class="badge ${statusClass}">${statusText}</span></td>
                <td>${formatDateTime(node.last_heartbeat_at)}</td>
                <td>
                    <div style="display: flex; gap: 5px;">
                        ${isOccupied ? `
                            <button class="btn-table" onclick="releaseNode(${node.id}, '${node.name}')" style="background: var(--warning-color); color: white;">
                                <i class="fa-solid fa-unlock"></i> 释放
                            </button>
                        ` : ''}
                        <button class="btn-table" onclick="openNodeModal('edit', ${JSON.stringify(node).replace(/"/g, '&quot;')})" style="background: var(--accent-color); color: white;">
                            <i class="fa-solid fa-pen-to-square"></i> 编辑
                        </button>
                        <button class="btn-table btn-delete" onclick="deleteNode(${node.id}, '${node.name}')">
                            <i class="fa-solid fa-trash"></i> 删除
                        </button>
                    </div>
                </td>
            `;
            tbody.appendChild(tr);
        });
    } catch (err) {
        console.error('Fetch nodes error:', err);
    }
}

function openNodeModal(mode, nodeData = null) {
    const modal = document.getElementById('nodeModal');
    const title = document.getElementById('nodeModalTitle');
    const submitBtn = document.getElementById('modalNodeSubmitBtn');

    // 清空表单
    document.getElementById('modalNodeId').value = '';
    document.getElementById('modalNodeName').value = '';
    document.getElementById('modalNodeModel').value = '';
    document.getElementById('modalNodeAddress').value = '';
    document.getElementById('tokenRow').style.display = 'none';

    if (mode === 'add') {
        title.innerText = '新增节点';
        submitBtn.innerText = '确认添加';
    } else {
        title.innerText = '编辑节点信息';
        submitBtn.innerText = '保存修改';

        // 填充数据
        document.getElementById('modalNodeId').value = nodeData.id;
        document.getElementById('modalNodeName').value = nodeData.name;
        document.getElementById('modalNodeModel').value = nodeData.nodemodel || '';
        document.getElementById('modalNodeAddress').value = nodeData.address;
        document.getElementById('modalNodeToken').value = nodeData.token;
        document.getElementById('tokenRow').style.display = 'flex';
    }

    modal.style.display = 'flex';
}

function closeNodeModal() {
    document.getElementById('nodeModal').style.display = 'none';
}

function copyToken() {
    const tokenInput = document.getElementById('modalNodeToken');
    const toast = document.getElementById('copyToast');

    tokenInput.select();
    tokenInput.setSelectionRange(0, 99999);

    navigator.clipboard.writeText(tokenInput.value).then(() => {
        showCopyToast(toast);
    }).catch(err => {
        console.error('无法复制: ', err);
        try {
            document.execCommand('copy');
            showCopyToast(toast);
        } catch (e) {
            console.error('复制失败');
        }
    });
}

function showCopyToast(toast) {
    if (!toast) return;
    toast.style.display = 'block';
    // 2秒后由于 CSS 动画会消失，这里简单重置 display
    setTimeout(() => {
        toast.style.display = 'none';
    }, 2000);
}

async function submitNode() {
    const id = document.getElementById('modalNodeId').value;
    const name = document.getElementById('modalNodeName').value;
    const model = document.getElementById('modalNodeModel').value;
    const address = document.getElementById('modalNodeAddress').value;

    if (!name || !model) {
        alert("请填写完整信息");
        return;
    }

    const isEdit = id !== '';
    const url = isEdit ? `/api/nodes/${id}` : '/api/nodes';
    const method = isEdit ? 'PUT' : 'POST';

    const body = { name, nodemodel: model, address };

    try {
        const response = await fetch(url, {
            method: method,
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(body)
        });

        const result = await response.json();

        if (response.ok && result.success) {
            alert(isEdit ? '修改成功' : '添加节点成功');
            closeNodeModal();
            fetchNodes();
        } else {
            alert(result.error || (isEdit ? '修改失败' : '添加失败'));
        }
    } catch (err) {
        alert('网络请求出错');
    }
}

async function deleteNode(id, name) {
    if (!confirm(`确定要删除节点 "${name}" 吗？`)) return;

    try {
        const { response, result, aborted } = await requestJSON(`/api/nodes/${id}`, { method: 'DELETE' });
        if (aborted || !result) return;

        if (response.ok && result.success) {
            alert('删除成功');
            fetchNodes();
        } else {
            alert(result.error || '删除失败');
        }
    } catch (err) {
        alert('网络请求出错');
    }
}
// 总控制台：获取并显示正在进行的考试（通过 busy 节点查询）
async function fetchExamsForConsole() {
    try {
        // 使用 stats 接口，它通过 busy 节点查询正在进行的考试
        const { response, result, aborted } = await requestJSON('/api/exams/stats');
        if (aborted || !result) return;
        if (!response.ok) throw new Error('无法获取考试统计');

        if (!result.success) {
            console.error('获取考试统计失败');
            return;
        }

        const exams = (result.data.ongoing_exams || []).map(normalizeEntity);
        const tbody = document.getElementById('exam-list-body');

        // 只在控制台页面显示时才更新表格
        if (tbody && document.getElementById('console').classList.contains('active')) {
            tbody.innerHTML = '';
            if (exams.length === 0) {
                tbody.innerHTML = '<tr><td colspan="10" style="text-align: center; color: #9ca3af;">暂无正在进行的考试</td></tr>';
            } else {
                exams.forEach((exam, index) => {
                    const startTime = formatDateTime(exam.start_time);
                    const endTime = formatExamEndTime(exam);
                    const tr = document.createElement('tr');
                    tr.innerHTML = `
                        <td>${index + 1}</td>
                        <td>${exam.subject || '未知'}</td>
                        <td>${exam.room?.building || '-'}</td>
                        <td>${exam.room?.name || '未知'}</td>
                        <td>${exam.node?.name || '未知'}</td>
                        <td>${startTime}</td>
                        <td>${endTime}</td>
                        <td>${exam.examinee_count || 0}</td>
                        <td><span style="color: ${exam.anomalies_count > 0 ? '#ef4444' : '#10b981'}">${exam.anomalies_count || 0}</span></td>
                        <td>
                            <button class="btn-table" onclick="observeExam(${exam.id})" style="background: var(--accent-color); color: white; font-size: 12px; padding: 4px 8px; margin-right: 6px;">
                                <i class="fa-solid fa-eye"></i> 查看
                            </button>
                            <button class="btn-table" onclick="viewExamAnomalies(${exam.id})" style="background: var(--warning-color); color: white; font-size: 12px; padding: 4px 8px;">
                                <i class="fa-solid fa-triangle-exclamation"></i> 异常
                            </button>
                        </td>
                    `;
                    tbody.appendChild(tr);
                });
            }
        }
    } catch (err) {
        console.error('获取考试列表失败:', err);
    }
}

// 实时观测按钮：跳到单点观测，显示节点的 /stream
async function observeExam(examId) {
    try {
        // 跳转到单点观测，打开 stream
        switchTab('single-view', document.querySelector('[onclick*="single-view"]'));

        // 模拟选择信号源的行为
        currentTargetBox = { index: 0, isSingle: true };
        await selectStream(examId);
    } catch (err) {
        console.error('Observe exam error:', err);
    }
}

// 关闭单点观测
function closeMonitor() {
    const container = document.getElementById('single-view');
    const grid = container.querySelector('.monitor-grid');
    grid.innerHTML = `
        <div class="monitor-screen add-btn" onclick="addExam(0, true)">
            <i class="fa-solid fa-plus"></i>
        </div>
    `;
}
async function releaseNode(id, name) {
    if (!confirm(`确定要强制释放节点 "${name}" 吗？`)) return;

    try {
        const { response, result, aborted } = await requestJSON(`/api/nodes/${id}/release`, { method: 'POST' });
        if (aborted || !result) return;

        if (response.ok && result.success) {
            alert('节点已释放');
            fetchNodes();
        } else {
            alert(result.error || result.message || '释放失败');
        }
    } catch (err) {
        alert('网络请求出错');
    }
}

async function jumpToNode(nodeId) {
    try {
        const { result, aborted } = await requestJSON(`/api/nodes/${nodeId}/jump`);
        if (aborted || !result) return;

        if (result.success && result.jump_url) {
            window.open(result.jump_url, '_blank');
        } else {
            alert(result.error || '无法进入节点');
        }
    } catch (err) {
        alert('网络请求出错');
    }
}

// --- 系统设置：配置同步 ---
async function syncRooms() {
    if (!confirm('确定要同步教室信息吗？')) return;
    try {
        const { result, aborted } = await requestJSON('/api/sync/rooms', { method: 'POST' });
        if (aborted || !result) return;
        if (result.success) {
            alert('教室信息同步指令已发送');
        } else {
            alert('同步失败: ' + (result.error || result.message));
        }
    } catch (err) {
        alert('网络请求出错');
    }
}

// --- 教室管理逻辑 ---
async function fetchRooms() {
    try {
        const { response, result, aborted } = await requestJSON('/api/rooms');
        if (aborted || !result) return;
        if (!response.ok) throw new Error('无法获取教室列表');
        const rooms = (result.data || []).map(normalizeEntity);

        // 更新统计面板
        const buildings = [...new Set(rooms.map(r => r.building))];
        document.getElementById('room-total-count').innerText = rooms.length;
        document.getElementById('room-building-count').innerText = buildings.length;

        // 渲染表格
        const tbody = document.getElementById('classroom-list-body');
        tbody.innerHTML = '';
        rooms.forEach((room, idx) => {
            const tr = document.createElement('tr');
            tr.innerHTML = `
            <td>${idx + 1}</td>
                <td>${room.building}</td>
                <td>${room.name}</td>
                <td>${room.type || '-'}</td>
                <td style="font-family: monospace; font-size: 11px;">${room.rtsp_url}</td>
                <td>${formatDateTime(room.created_at)}</td>
                <td>
                    <div style="display: flex; gap: 5px;">
                        <button class="btn-table" onclick="openRoomModal('edit', ${JSON.stringify(room).replace(/"/g, '&quot;')})" style="background: var(--accent-color); color: white;">
                            <i class="fa-solid fa-pen-to-square"></i> 编辑
                        </button>
                        <button class="btn-table btn-delete" onclick="deleteRoom(${room.id}, '${room.name}')">
                            <i class="fa-solid fa-trash"></i> 删除
                        </button>
                    </div>
                </td>
            `;
            tbody.appendChild(tr);
        });
    } catch (err) {
        console.error('Fetch rooms error:', err);
    }
}

function openRoomModal(mode, roomData = null) {
    const modal = document.getElementById('roomModal');
    const title = document.getElementById('roomModalTitle');
    const submitBtn = document.getElementById('modalRoomSubmitBtn');

    // 清空表单
    document.getElementById('modalRoomId').value = '';
    document.getElementById('modalRoomName').value = '';
    document.getElementById('modalRoomBuilding').value = '';
    document.getElementById('modalRoomType').value = '';
    document.getElementById('modalRoomRTSPUrl').value = '';

    if (mode === 'add') {
        title.innerText = '新增教室';
        submitBtn.innerText = '确认添加';
    } else {
        title.innerText = '编辑教室信息';
        submitBtn.innerText = '保存修改';

        // 填充数据
        document.getElementById('modalRoomId').value = roomData.id;
        document.getElementById('modalRoomName').value = roomData.name;
        document.getElementById('modalRoomBuilding').value = roomData.building;
        document.getElementById('modalRoomType').value = roomData.type || '';
        document.getElementById('modalRoomRTSPUrl').value = roomData.rtsp_url;
    }

    modal.style.display = 'flex';
}

function closeRoomModal() {
    document.getElementById('roomModal').style.display = 'none';
}

async function submitRoom() {
    const id = document.getElementById('modalRoomId').value;
    const name = document.getElementById('modalRoomName').value;
    const building = document.getElementById('modalRoomBuilding').value;
    const type = document.getElementById('modalRoomType').value;
    const rtspUrl = document.getElementById('modalRoomRTSPUrl').value;

    if (!name || !building || !type || !rtspUrl) {
        alert("请填写完整信息");
        return;
    }

    const isEdit = id !== '';
    const url = isEdit ? `/api/rooms/${id}` : '/api/rooms';
    const method = isEdit ? 'PUT' : 'POST';

    const body = { name, building, type, rtsp_url: rtspUrl };

    try {
        const { response, result, aborted } = await requestJSON(url, {
            method: method,
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(body)
        });
        if (aborted || !result) return;

        if (response.ok && result.success) {
            alert(isEdit ? '修改成功' : '添加教室成功');
            closeRoomModal();
            fetchRooms();
        } else {
            alert(result.error || (isEdit ? '修改失败' : '添加失败'));
        }
    } catch (err) {
        alert('网络请求出错');
    }
}

async function deleteRoom(id, name) {
    if (!confirm(`确定要删除教室 "${name}" 吗？`)) return;

    try {
        const { response, result, aborted } = await requestJSON(`/api/rooms/${id}`, { method: 'DELETE' });
        if (aborted || !result) return;

        if (response.ok && result.success) {
            alert('删除成功');
            fetchRooms();
        } else {
            alert(result.error || '删除失败');
        }
    } catch (err) {
        alert('网络请求出错');
    }
}