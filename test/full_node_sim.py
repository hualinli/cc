import argparse
import uvicorn
import requests
from fastapi import FastAPI, Request, Query
from fastapi.responses import HTMLResponse
from pydantic import BaseModel
from typing import Optional, Dict, Any

app = FastAPI(title="CC Node Simulator")

# Global configuration
config = {
    "cc_url": "http://localhost:8080",
    "node_id": 1,
    "token": ""
}

# HTML Template as a string for simplicity in a single-file simulator
HTML_TEMPLATE = """
<!DOCTYPE html>
<html>
<head>
    <title>CC Node Simulator - Node {{ node_id }}</title>
    <link href="https://cdn.bootcdn.net/ajax/libs/twitter-bootstrap/5.3.0/css/bootstrap.min.css" rel="stylesheet">
    <style>
        body { background-color: #f8f9fa; padding: 20px; }
        .card { margin-bottom: 20px; box-shadow: 0 0.125rem 0.25rem rgba(0,0,0,0.075); }
        .log-container { background: #212529; color: #00ff00; padding: 15px; border-radius: 5px; height: 300px; overflow-y: auto; font-family: monospace; }
        .status-badge { font-size: 0.9em; }
    </style>
</head>
<body>
    <div class="container">
        <div class="d-flex justify-content-between align-items-center mb-4">
            <h1>Node Simulator <small class="text-muted">#{{ node_id }}</small></h1>
            <span class="badge bg-success status-badge">Authorized</span>
        </div>

        <div class="row">
            <div class="col-md-6">
                <!-- Config Section -->
                <div class="card">
                    <div class="card-header bg-primary text-white">Config</div>
                    <div class="card-body">
                        <div class="mb-3">
                            <label class="form-label">CC Server URL</label>
                            <input type="text" id="cc_url" class="form-control" value="{{ cc_url }}">
                        </div>
                        <div class="row">
                            <div class="col">
                                <label class="form-label">Node ID</label>
                                <input type="number" id="node_id" class="form-control" value="{{ node_id }}">
                            </div>
                            <div class="col">
                                <label class="form-label">Token</label>
                                <input type="text" id="token" class="form-control" value="{{ token }}">
                            </div>
                        </div>
                    </div>
                </div>

                <!-- Heartbeat Section -->
                <div class="card">
                    <div class="card-header bg-info text-white d-flex justify-content-between align-items-center">
                        Heartbeat
                        <div class="form-check form-switch m-0">
                            <input class="form-check-input" type="checkbox" id="auto_hb" checked>
                            <label class="form-check-label text-white" style="font-size: 0.8em;">Auto (30s)</label>
                        </div>
                    </div>
                    <div class="card-body">
                        <div class="input-group">
                            <select id="hb_status" class="form-select">
                                <option value="idle">Idle (空闲)</option>
                                <option value="busy">Busy (忙碌)</option>
                                <option value="error">Error (故障)</option>
                            </select>
                            <button class="btn btn-primary" onclick="sendHeartbeat()">Send Now</button>
                        </div>
                    </div>
                </div>

                <!-- Exam Sync Section -->
                <div class="card">
                    <div class="card-header bg-warning">Exam Task Sync</div>
                    <div class="card-body">
                        <div class="row g-2 mb-2">
                            <div class="col-md-4">
                                <label class="form-label">Action</label>
                                <select id="task_action" class="form-select">
                                    <option value="start">Start</option>
                                    <option value="stop">Stop</option>
                                    <option value="sync">Sync</option>
                                </select>
                            </div>
                            <div class="col-md-4">
                                <label class="form-label">Exam ID</label>
                                <input type="number" id="exam_id" class="form-control" placeholder="Required for stop/sync">
                            </div>
                            <div class="col-md-4">
                                <label class="form-label">Room ID</label>
                                <input type="number" id="room_id" class="form-control" value="1">
                            </div>
                        </div>
                        <div class="row g-2 mb-3">
                            <div class="col-md-8">
                                <label class="form-label">Subject</label>
                                <input type="text" id="subject" class="form-control" value="Mathematics">
                            </div>
                            <div class="col-md-4">
                                <label class="form-label">Examinees</label>
                                <input type="number" id="examinee_count" class="form-control" value="30">
                            </div>
                        </div>
                        <button class="btn btn-primary w-100" onclick="sendTaskSync()">Send Task Sync</button>
                    </div>
                </div>

                <!-- Alert Section -->
                <div class="card">
                    <div class="card-header bg-danger text-white">Report Alert</div>
                    <div class="card-body">
                        <div class="row g-2 mb-2">
                            <div class="col-md-6">
                                <label class="form-label">Type</label>
                                <select id="alert_type" class="form-select">
                                    <option value="phone_cheating">Phone Cheating</option>
                                    <option value="look_around">Look Around</option>
                                    <option value="whispering">Whispering</option>
                                    <option value="stand_up">Stand Up</option>
                                    <option value="other">Other</option>
                                </select>
                            </div>
                            <div class="col-md-6">
                                <label class="form-label">Seat Number</label>
                                <input type="text" id="seat_number" class="form-control" value="A-01">
                            </div>
                        </div>
                        <div class="mb-3">
                            <label class="form-label">Message</label>
                            <input type="text" id="alert_msg" class="form-control" value="Detected potential cheating behavior.">
                        </div>
                        <button class="btn btn-danger w-100" onclick="sendAlert()">Report Alert</button>
                    </div>
                </div>
            </div>

            <div class="col-md-6">
                <!-- Log Section -->
                <div class="card" style="height: calc(100% - 20px);">
                    <div class="card-header bg-dark text-white d-flex justify-content-between align-items-center">
                        Response Log
                        <button class="btn btn-sm btn-outline-light" onclick="clearLog()">Clear</button>
                    </div>
                    <div class="card-body p-0">
                        <div id="log" class="log-container"></div>
                    </div>
                </div>
            </div>
        </div>
    </div>

    <script>
        function addLog(msg, type='info') {
            const logDiv = document.getElementById('log');
            const time = new Date().toLocaleTimeString();
            const color = type === 'error' ? '#ff3333' : (type === 'success' ? '#00ff00' : '#ffffff');
            logDiv.innerHTML += `<div style="color: ${color}">[${time}] ${msg}</div>`;
            logDiv.scrollTop = logDiv.scrollHeight;
        }

        function clearLog() {
            document.getElementById('log').innerHTML = '';
        }

        async function callProxy(endpoint, payload) {
            const cc_url = document.getElementById('cc_url').value;
            const token = document.getElementById('token').value;
            const node_id = document.getElementById('node_id').value;

            try {
                const response = await fetch('/simulate', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({
                        cc_url, token, endpoint, payload
                    })
                });
                const data = await response.json();
                if (data.success) {
                    addLog(`${endpoint} SUCCESS: ` + JSON.stringify(data.response), 'success');
                    if (data.response.exam_id) {
                        document.getElementById('exam_id').value = data.response.exam_id;
                    }
                } else {
                    const errMsg = data.error || (data.response && data.response.error) || 'Unknown Error';
                    addLog(`${endpoint} FAILED: ` + errMsg, 'error');
                }
            } catch (e) {
                addLog('Request Error: ' + e, 'error');
            }
        }

        function sendHeartbeat() {
            const status = document.getElementById('hb_status').value;
            addLog(`Sending Heartbeat (status=${status})...`);
            callProxy('/node-api/v1/heartbeat', { status: status, details: { uptime: "1h", load: 0.5 } });
        }

        // Auto heartbeat
        setInterval(() => {
            if (document.getElementById('auto_hb').checked) {
                sendHeartbeat();
            }
        }, 30000); // 30s

        function sendTaskSync() {
            const action = document.getElementById('task_action').value;
            const exam_id = parseInt(document.getElementById('exam_id').value) || 0;
            const room_id = parseInt(document.getElementById('room_id').value);
            const subject = document.getElementById('subject').value;
            const examinee_count = parseInt(document.getElementById('examinee_count').value);

            addLog(`Sending Task Sync (action=${action})...`);
            callProxy('/node-api/v1/tasks/sync', {
                action,
                exam_id,
                room_id,
                subject,
                examinee_count,
                start_time: new Date().toISOString()
            });
        }

        function sendAlert() {
            const type = document.getElementById('alert_type').value;
            const seat = document.getElementById('seat_number').value;
            const msg = document.getElementById('alert_msg').value;
            const exam_id = parseInt(document.getElementById('exam_id').value) || 0;

            if (!exam_id) {
                addLog('Error: Exam ID is required to report alert.', 'error');
                return;
            }

            addLog(`Reporting Alert (${type})...`);
            callProxy('/node-api/v1/alerts', {
                type,
                seat_number: seat,
                message: msg,
                exam_id: exam_id
            });
        }
    </script>
</body>
</html>
"""

class ProxyRequest(BaseModel):
    cc_url: str
    token: str
    endpoint: str
    payload: Dict[str, Any]

@app.get("/", response_class=HTMLResponse)
async def index(request: Request, token: Optional[str] = Query(None)):
    # Simple token validation for "jump"
    if config["token"] and token != config["token"]:
        return HTMLResponse(content="<h1>403 Unauthorized</h1><p>Invalid Token provided in Jump URL.</p>", status_code=403)
    
    # Render with current config
    html_content = HTML_TEMPLATE.replace("{{ cc_url }}", config["cc_url"])
    html_content = html_content.replace("{{ node_id }}", str(config["node_id"]))
    html_content = html_content.replace("{{ token }}", config["token"])
    return html_content

@app.post("/simulate")
async def simulate_to_cc(data: ProxyRequest):
    headers = {
        "Authorization": f"Bearer {data.token}",
        "X-Node-Token": data.token,  # Support both header types just in case
        "Content-Type": "application/json"
    }
    
    url = f"{data.cc_url.rstrip('/')}{data.endpoint}"
    
    try:
        # Use requests to perform the actual call to CC
        resp = requests.post(url, json=data.payload, headers=headers, timeout=5)
        res_json = resp.json() if resp.status_code != 204 else {}
        return {
            "success": resp.status_code == 200,
            "status_code": resp.status_code,
            "response": res_json,
            "error": res_json.get("error") if isinstance(res_json, dict) else None
        }
    except Exception as e:
        return {"success": False, "error": str(e)}

if __name__ == "__main__":
    parser = argparse.ArgumentParser(description="CC Node Simulator")
    parser.add_argument("--cc-url", default="http://localhost:8080", help="CC Server URL")
    parser.add_argument("--node-id", type=int, default=1, help="Node ID")
    parser.add_argument("--token", required=True, help="Node Auth Token")
    parser.add_argument("--port", type=int, default=8001, help="Simulator Port")
    parser.add_argument("--host", default="0.0.0.0", help="Simulator Host")

    args = parser.parse_args()
    
    config["cc_url"] = args.cc_url
    config["node_id"] = args.node_id
    config["token"] = args.token

    print(f"Simulator started at http://{args.host}:{args.port}")
    print(f"Jump URL example: http://localhost:{args.port}?token={args.token}")
    
    uvicorn.run(app, host=args.host, port=args.port)
