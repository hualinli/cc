"""
Node Simulator for Smart Proctoring System (智慧监考系统)
=============================================================
模拟边缘检测节点，与控制中心双向通信。

用法:
    pip install fastapi uvicorn requests
    python main.py
    python main.py -p 8002 -c http://192.168.1.1:8080 -t my-node-token
"""

from __future__ import annotations

import argparse
import json
import os
import threading
import time
import uuid
from datetime import datetime, timezone
from typing import Any

import requests
from fastapi import FastAPI, Query, Request
from fastapi.responses import HTMLResponse, JSONResponse

# ── 配置 ──
CC_URL = os.getenv("CC_URL", "http://localhost:8080")
PORT = int(os.getenv("PORT", "9999"))
TOKEN = os.getenv("TOKEN", "8c789185dcefd404734375b612a5f1aa")
HEARTBEAT_SEC = int(os.getenv("HEARTBEAT", "30"))

# ── 运行时状态 ──
st: dict[str, Any] = {
    "status": "idle",
    "exam_id": None,
    "exam_subject": "",
    "exam_duration": 0,
    "exam_count": 0,
    "hb_running": False,
    "hb_last": None,
    "hb_err": None,
    "cc_url": CC_URL,
    "token": TOKEN,
    # 手动控制返回值
    "manual_fail": False,       # True 时 schedule_start 返回失败
    "manual_fail_msg": "节点拒绝: 模拟考试启动失败",
}
_msgs: list[dict] = []
_lock = threading.Lock()

app = FastAPI(title="NodeSim", docs_url=None, redoc_url=None)


# ── helpers ──
def _log(dir_: str, url: str, payload: Any, resp: Any = None, err: str = ""):
    with _lock:
        _msgs.append({
            "id": uuid.uuid4().hex[:6],
            "dir": dir_,
            "url": url,
            "payload": payload,
            "resp": resp,
            "err": err,
            "ts": datetime.now(timezone.utc).strftime("%H:%M:%S.%f")[:-3],
        })
        if len(_msgs) > 500:
            del _msgs[:-500]


def _post(path: str, payload: dict, timeout: int = 5) -> dict | None:
    url = f"{st['cc_url'].rstrip('/')}{path}"
    h = {"X-Node-Token": st["token"], "Content-Type": "application/json"}
    try:
        r = requests.post(url, json=payload, headers=h, timeout=timeout)
        try:
            body = r.json()
        except Exception:
            body = {"_raw": r.text[:300]}
        _log("SEND", url, payload, resp={"code": r.status_code, "body": body})
        return body
    except requests.exceptions.ConnectionError as e:
        _log("ERROR", url, payload, err=f"连接拒绝: {e}")
    except requests.exceptions.Timeout:
        _log("ERROR", url, payload, err="超时")
    except Exception as e:
        _log("ERROR", url, payload, err=str(e))
    return None


# ── 入站: 控制中心 → 节点 ──
@app.post("/exam/schedule_start")
async def exam_schedule_start(request: Request, token: str = Query("")):
    try:
        body = await request.json()
    except Exception:
        body = {}
    if token and token != st["token"]:
        _log("RECV", str(request.url), body, err="token 不匹配")
        return JSONResponse({"success": False, "error": "invalid token"}, 403)

    # 手动返回失败模式
    if st["manual_fail"]:
        _log("RECV ◀ 控制中心", str(request.url), body,
             resp={"success": False, "error": st["manual_fail_msg"]})
        return JSONResponse({"success": False, "error": st["manual_fail_msg"]}, 503)

    _log("RECV ◀ 控制中心", str(request.url), body)
    eid = body.get("exam_id")
    st["exam_id"] = eid
    st["exam_subject"] = body.get("subject", "")
    st["exam_duration"] = int(body.get("duration", 0))
    st["exam_rtsp_url"] = body.get("rtsp_url", "")
    st["exam_room_id"] = body.get("classroom_id", 0)
    st["status"] = "busy"

    # 向控制中心回 ack 确认
    if eid:
        threading.Thread(target=lambda: _post("/node-api/v1/tasks/sync", {
            "action": "start", "exam_id": int(eid),
            "room_id": body.get("classroom_id", 0),
            "subject": body.get("subject", ""),
            "start_time": datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
            "duration_minutes": int(body.get("duration", 90)),
        }), daemon=True).start()
    return {"success": True}


# ── 入站: 教室同步 ──
@app.post("/classrooms")
async def classrooms_sync(request: Request, token: str = Query("")):
    try:
        body = await request.json()
    except Exception:
        body = {}
    if token and token != st["token"]:
        _log("RECV", str(request.url), body, err="token 不匹配")
        return JSONResponse({"success": False, "error": "invalid token"}, 403)

    classrooms = body.get("classrooms", [])
    st["classrooms"] = classrooms
    _log("RECV ◀ 教室同步", str(request.url), {"n": len(classrooms)}, resp={"ok": True})
    return {"ok": True}


# ── 心跳 ──
def _hb_loop():
    while st["hb_running"]:
        _post("/node-api/v1/heartbeat", {"status": st["status"], "details": {}, "port": PORT})
        now = datetime.now(timezone.utc).strftime("%H:%M:%S")
        st["hb_last"] = now
        st["hb_err"] = None
        time.sleep(HEARTBEAT_SEC)


def hb_start():
    if st["hb_running"]:
        return
    st["hb_running"] = True
    threading.Thread(target=_hb_loop, daemon=True).start()


def hb_stop():
    st["hb_running"] = False


# ── API ──
@app.get("/api/state")
def api_state():
    return {
        "config": {"cc_url": st["cc_url"], "token": st["token"], "port": PORT},
        "runtime": {
            "status": st["status"], "exam_id": st["exam_id"],
            "exam_subject": st["exam_subject"], "exam_duration": st["exam_duration"],
            "exam_rtsp_url": st.get("exam_rtsp_url", ""), "exam_room_id": st.get("exam_room_id", 0),
            "exam_count": st["exam_count"], "classrooms_n": len(st.get("classrooms", [])),
            "hb_running": st["hb_running"],
            "hb_last": st["hb_last"], "hb_err": st["hb_err"],
            "msg_n": len(_msgs),
        },
        "manual_fail": st["manual_fail"],
        "manual_fail_msg": st["manual_fail_msg"],
    }


@app.get("/api/msgs")
def api_msgs(since: int = 0, limit: int = 200):
    """since 传消息序号(从0开始)，只返回序号>=since的消息"""
    with _lock:
        ms = list(_msgs)
    if since > 0:
        ms = [m for m in ms if _msgs.index(m) >= since]
    return {"msgs": ms[-limit:], "total": len(_msgs), "next_since": len(_msgs)}


@app.post("/api/cfg")
async def api_cfg(request: Request):
    body = await request.json()
    changed = False
    for k in ("cc_url", "token"):
        if k in body and body[k] != st[k]:
            st[k] = body[k]
            changed = True
    if "manual_fail" in body:
        st["manual_fail"] = bool(body["manual_fail"])
    if "manual_fail_msg" in body:
        st["manual_fail_msg"] = str(body["manual_fail_msg"])
    if changed and st["hb_running"]:
        hb_stop()
        time.sleep(0.3)
        hb_start()
    return {"ok": True, "restarted": changed}


@app.post("/api/act")
async def api_act(request: Request):
    body = await request.json()
    a = body.get("action", "")

    if a == "hb_now":
        r = _post("/node-api/v1/heartbeat", {"status": st["status"], "details": {}, "port": PORT})
        return {"ok": True, "resp": r}

    if a == "hb_start":
        hb_start()
        return {"ok": True}

    if a == "hb_stop":
        hb_stop()
        return {"ok": True}

    eid = body.get("exam_id") or st["exam_id"]

    if a == "stop":
        if not eid:
            return {"ok": False, "err": "无进行中考试"}
        r = _post("/node-api/v1/tasks/sync", {"action": "stop", "exam_id": int(eid)})
        if r and r.get("success"):
            st["status"] = "idle"; st["exam_id"] = None; st["exam_subject"] = ""
        return {"ok": True, "resp": r}

    if a == "sync":
        if not eid:
            return {"ok": False, "err": "无进行中考试"}
        n = int(body.get("examinee_count", 0))
        st["exam_count"] = n
        r = _post("/node-api/v1/tasks/sync", {"action": "sync", "exam_id": int(eid), "examinee_count": n})
        return {"ok": True, "resp": r}

    if a == "alert":
        if not eid:
            return {"ok": False, "err": "无进行中考试"}
        r = _post("/node-api/v1/alerts", {
            "exam_id": int(eid),
            "seat_number": str(body.get("seat", "A1")),
            "type": str(body.get("type", "suspicious_behavior")),
            "message": str(body.get("message", "")),
            "x": float(body.get("x", 0)),
            "y": float(body.get("y", 0)),
        })
        return {"ok": True, "resp": r}

    if a == "set_status":
        s = body.get("status", "idle")
        if s in ("idle", "busy", "error"):
            st["status"] = s
        return {"ok": True}

    if a == "clear":
        with _lock:
            _msgs.clear()
        return {"ok": True}

    return {"ok": False, "err": f"未知操作: {a}"}


# ── UI ──
@app.get("/", response_class=HTMLResponse)
def ui():
    html = _HTML
    html = html.replace("{PORT}", str(PORT))
    html = html.replace("{CC_URL}", st["cc_url"])
    html = html.replace("{TOKEN}", st["token"])
    html = html.replace("{HB_SEC}", str(HEARTBEAT_SEC))
    html = html.replace("{MANUAL_FAIL}", str(st["manual_fail"]).lower())
    html = html.replace("{FAIL_MSG}", st["manual_fail_msg"])
    html = html.replace("{{", "{").replace("}}", "}")
    return html


_HTML = r"""<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="UTF-8"><meta name="viewport" content="width=device-width,initial-scale=1.0">
<title>Node Simulator</title>
<style>
:root{{--bg:#0b1120;--c1:#1a2332;--b:#2a3544;--t:#e2e8f0;--m:#8899aa;--a:#22d3ee;--g:#10b981;--bl:#60a5fa;--r:#f87171;--idle:#22c55e;--busy:#f59e0b}}
*{{margin:0;padding:0;box-sizing:border-box}}
body{{font-family:'JetBrains Mono',monospace;background:var(--bg);color:var(--t);min-height:100vh}}
.hd{{background:var(--c1);border-bottom:1px solid var(--b);padding:10px 20px;display:flex;align-items:center;justify-content:space-between;gap:12px}}
.hd h1{{font-size:17px;color:var(--a)}}
.hd .st{{font-size:11px;display:flex;align-items:center;gap:6px}}
.dot{{display:inline-block;width:8px;height:8px;border-radius:50%}}
.dot.idle{{background:var(--idle);box-shadow:0 0 5px var(--idle)}}
.dot.busy{{background:var(--busy);box-shadow:0 0 5px var(--busy)}}
.dot.error{{background:var(--r);box-shadow:0 0 5px var(--r)}}
.layout{{display:grid;grid-template-columns:360px 1fr;gap:12px;padding:12px;height:calc(100vh - 48px)}}
.pn{{background:var(--c1);border-radius:8px;border:1px solid var(--b);padding:14px;overflow-y:auto}}
.pn h2{{font-size:12px;color:var(--a);margin-bottom:10px;padding-bottom:5px;border-bottom:1px solid var(--b);text-transform:uppercase;letter-spacing:1px}}
.fg{{margin-bottom:10px}}
.fg label{{display:block;font-size:10px;color:var(--m);margin-bottom:3px;text-transform:uppercase}}
.fg input,.fg select{{width:100%;padding:7px 10px;background:#0b1120;border:1px solid var(--b);border-radius:4px;color:var(--t);font-size:12px;font-family:inherit}}
.fg input:focus,.fg select:focus{{outline:none;border-color:var(--a)}}
.btn{{padding:7px 14px;border:none;border-radius:4px;font-size:11px;font-family:inherit;cursor:pointer;margin:2px;transition:opacity .2s}}
.btn:hover{{opacity:.8}}
.btn-a{{background:var(--a);color:#0b1120;font-weight:600}}
.btn-g{{background:var(--g);color:#fff}}
.btn-r{{background:var(--r);color:#fff}}
.btn-y{{background:var(--busy);color:#0b1120}}
.btn-o{{background:transparent;border:1px solid var(--b);color:var(--t)}}
.btns{{display:flex;flex-wrap:wrap;gap:4px;margin-top:6px}}
.tag{{display:inline-block;padding:2px 6px;border-radius:3px;font-size:9px;font-weight:600}}
.tag-b{{background:rgba(96,165,250,.2);color:var(--bl)}}
.tag-r{{background:rgba(248,113,113,.2);color:var(--r)}}
.tag-g{{background:rgba(16,185,129,.2);color:var(--g)}}
.note{{background:rgba(34,211,238,.06);border:1px dashed rgba(34,211,238,.25);border-radius:6px;padding:10px;margin:8px 0;font-size:11px;line-height:1.5}}
.note.exam{{border-color:var(--busy);background:rgba(245,158,11,.08)}}
.note.fail{{border-color:var(--r);background:rgba(248,113,113,.08)}}
#toast{{position:fixed;top:12px;right:20px;padding:8px 16px;border-radius:4px;font-size:12px;z-index:99;transition:opacity .3s;opacity:0;pointer-events:none}}
.msg-list{{display:flex;flex-direction:column;gap:3px}}
.msg{{font-size:10px;border-radius:4px;border-left:3px solid transparent;overflow:hidden}}
.msg.SEND{{border-left-color:var(--g);background:rgba(16,185,129,.05)}}
.msg.RECV{{border-left-color:var(--bl);background:rgba(96,165,250,.05)}}
.msg.ERROR{{border-left-color:var(--r);background:rgba(248,113,113,.05)}}
.msg-hd{{padding:4px 7px;display:flex;justify-content:space-between;gap:6px}}
.msg-hd .dir{{font-weight:700;font-size:10px}}
.msg.SEND .dir{{color:var(--g)}}.msg.RECV .dir{{color:var(--bl)}}.msg.ERROR .dir{{color:var(--r)}}
.msg-hd .ts{{color:var(--m);font-size:9px}}
.msg-url{{font-size:9px;color:var(--m);padding:0 7px 3px;word-break:break-all}}
.msg-body{{font-size:10px;line-height:1.4;padding:0 7px 5px;white-space:pre-wrap;max-height:90px;overflow-y:auto;color:#cbd5e1}}
.msg-body .k{{color:#7dd3fc}}.msg-body .s{{color:#a5d6a7}}.msg-body .n{{color:#ffab40}}
.msg-resp{{margin:0 5px 5px;padding:3px 5px;background:rgba(0,0,0,.3);border-radius:3px;font-size:9px;color:var(--m);white-space:pre-wrap;max-height:45px;overflow-y:auto}}
.stats{{display:grid;grid-template-columns:1fr 1fr;gap:5px;margin-bottom:8px}}
.st{{padding:7px;background:rgba(0,0,0,.2);border-radius:4px;text-align:center}}
.st .v{{font-size:16px;font-weight:700;color:var(--a)}}
.st .l{{font-size:9px;color:var(--m);margin-top:1px}}
.st .v.busy{{color:var(--busy)}}
</style>
</head>
<body>

<div class="hd">
  <div><h1>◈ Node Simulator</h1></div>
  <div class="st" id="badge">⚫ 启动中...</div>
  <div id="toast"></div>
</div>

<div class="layout">
<!-- LEFT -->
<div class="pn" style="display:flex;flex-direction:column;gap:10px">

<h2>⚙ 连接配置</h2>
<div class="fg"><label>控制中心地址</label><input id="cfgCcUrl" value="{CC_URL}"></div>
<div class="fg"><label>Token</label><input id="cfgToken" value="{TOKEN}"></div>
<button class="btn btn-a" onclick="saveCfg()" style="width:100%">保存配置</button>

<h2>📡 心跳</h2>
<div class="stats">
  <div class="st"><div class="v" id="stStatus">idle</div><div class="l">状态</div></div>
  <div class="st"><div class="v" id="stHb">—</div><div class="l">最后心跳</div></div>
  <div class="st"><div class="v" id="stExam">—</div><div class="l">考试ID</div></div>
  <div class="st"><div class="v" id="stMsg">0</div><div class="l">消息数</div></div>
</div>
<div class="btns">
  <button class="btn btn-a" onclick="act('hb_start')">▶ 开始</button>
  <button class="btn btn-r" onclick="act('hb_stop')">⏹ 停止</button>
  <button class="btn btn-g" onclick="act('hb_now')">📡 立即心跳</button>
</div>

<!-- 当前考试状态 -->
<div id="examCard" class="note exam" style="display:none">
  <div style="font-weight:600;margin-bottom:4px">📋 当前考试 <span class="tag tag-g" id="examTag">进行中</span></div>
  <div>ID: <b id="eiId">—</b> 科目: <b id="eiSubj">—</b></div>
  <div>时长: <b id="eiDur">—</b> 分 · 考生: <b id="eiCount">—</b> 人</div>
</div>

<!-- 手动控制返回值 -->
<h2>🎛 控制返回值 <span class="tag tag-r">DEBUG</span></h2>
<div class="note fail">
  <div style="font-weight:600;margin-bottom:4px">✘ 模拟开考失败</div>
  <div style="font-size:10px;color:var(--m);margin-bottom:6px">开启后，控制中心下发的开考指令将收到失败响应，用于测试控制中心的回滚处理</div>
  <div class="fg"><label>失败原因</label><input id="failMsg" value="{FAIL_MSG}"></div>
  <div class="btns" style="align-items:center">
    <button class="btn btn-r" id="btnFailOn" onclick="setFail(true)">开启失败模式</button>
    <button class="btn btn-g" id="btnFailOff" onclick="setFail(false)">正常模式</button>
    <span id="failStatus" style="font-size:11px;color:var(--m);margin-left:4px"></span>
  </div>
</div>

<h2>🧪 模拟操作 <span class="tag tag-b">TEST</span></h2>
<div class="fg"><label>考生人数</label><input id="simCount" value="30" type="number"></div>
<div class="btns">
  <button class="btn btn-r" onclick="act('stop')">⏹ 结束考试</button>
  <button class="btn btn-y" onclick="act('sync')">🔄 同步人数</button>
</div>

<h2>🚨 模拟告警 <span class="tag tag-r">TEST</span></h2>
<div class="fg"><label>座位</label><input id="altSeat" value="A1"></div>
<div class="fg"><label>类型</label><select id="altType">
  <option>suspicious_behavior</option><option>phone_cheating</option>
  <option>look_around</option><option>sleep</option><option>other</option>
</select></div>
<button class="btn btn-r" onclick="act('alert')">🚨 上报告警</button>

<div style="margin-top:8px" class="btns">
  <button class="btn btn-o" onclick="act('set_status',{{status:'idle'}})">🟢 Idle</button>
  <button class="btn btn-o" onclick="act('set_status',{{status:'busy'}})">🟡 Busy</button>
  <button class="btn btn-o" onclick="act('set_status',{{status:'error'}})">🔴 Error</button>
  <button class="btn btn-o" onclick="act('clear')">🗑 清日志</button>
</div>
<div style="margin-top:8px;font-size:10px;color:var(--m);text-align:center">端口:{PORT} · 心跳:{HB_SEC}s</div>
</div>

<!-- RIGHT -->
<div class="pn" style="display:flex;flex-direction:column">
  <h2>📨 收发日志
    <span style="font-weight:400;font-size:10px;color:var(--m);margin-left:8px">
      <span style="color:var(--g)">SEND ▶</span> 上报
      <span style="color:var(--bl);margin-left:6px">◀ RECV</span> 下发
      <span style="color:var(--r);margin-left:6px">✖ ERROR</span>
    </span>
    <span style="float:right;font-size:10px;color:var(--m)" id="logCount">0 条</span>
  </h2>
  <div id="msgBox" class="msg-list" style="flex:1;overflow-y:auto">
    <div class="placeholder" style="text-align:center;color:var(--m);padding:60px 20px">等待消息…</div>
  </div>
</div>
</div>

<script>
var nextSince=0, failMode={MANUAL_FAIL};
function $(s){{return document.querySelector(s)}}
function esc(s){{return s.replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;')}}

function hl(o){{
  if(o===null||o===undefined)return'<span class="n">null</span>';
  if(typeof o==='string')return'<span class="s">"'+esc(o)+'"</span>';
  if(typeof o==='number')return'<span class="n">'+o+'</span>';
  if(typeof o==='boolean')return'<span class="n">'+o+'</span>';
  if(Array.isArray(o))return'['+o.map(hl).join(',')+']';
  if(typeof o==='object'){{var ps=[];for(var k in o)ps.push('<span class="k">"'+esc(k)+'"</span>: '+hl(o[k]));return'{{'+ps.join(',')+'}}'}}
  return esc(String(o));
}}

function toast(msg,c){{
  var t=$('#toast');if(!t)return;
  t.textContent=msg;t.style.background=c||'#22c55e';t.style.color='#fff';t.style.opacity='1';
  clearTimeout(t._tid);t._tid=setTimeout(function(){{t.style.opacity='0'}},2500);
}}

function updateFailUI(fail){{
  failMode=fail;
  var on=$('#btnFailOn'),off=$('#btnFailOff'),st=$('#failStatus');
  if(on)on.style.opacity=fail?'0.5':'1';
  if(off)off.style.opacity=fail?'1':'0.5';
  if(st)st.textContent=fail?'◀ 当前: 开考将返回失败':'◀ 当前: 正常模式';
  if(st)st.style.color=fail?'var(--r)':'var(--g)';
}}

async function setFail(v){{
  try{{
    var r=await fetch('/api/cfg',{{method:'POST',headers:{{'Content-Type':'application/json'}},
      body:JSON.stringify({{manual_fail:v,manual_fail_msg:$('#failMsg').value}})}});
    updateFailUI(v);
    toast(v?'失败模式已开启':'已恢复正常模式',v?'#f87171':'#22c55e');
  }}catch(e){{toast('设置失败: '+e.message,'#f87171')}}
}}

async function poll(){{
  try{{
    var r=await fetch('/api/state'),s=await r.json(),rt=s.runtime;
    $('#cfgCcUrl').value=s.config.cc_url;
    $('#cfgToken').value=s.config.token;
    $('#stStatus').textContent=rt.status;
    $('#stStatus').className='v'+(rt.status==='busy'?' busy':'');
    $('#stHb').textContent=rt.hb_last||'—';
    $('#stExam').textContent=rt.exam_id||'—';
    $('#stMsg').textContent=rt.msg_n;

    // 连接状态
    var b=$('#badge');
    if(rt.hb_running){{
      b.innerHTML='<span class="dot '+rt.status+'"></span>心跳中 · '+rt.status+(rt.hb_last?' · '+rt.hb_last:'');
    }}else{{b.innerHTML='⚫ 心跳已停止'}}

    // 考试状态卡片
    var ec=$('#examCard');
    if(rt.exam_id){{
      ec.style.display='block';
      $('#eiId').textContent=rt.exam_id;
      $('#eiSubj').textContent=rt.exam_subject;
      $('#eiDur').textContent=rt.exam_duration;
      $('#eiCount').textContent=rt.exam_count;
      $('#examTag').textContent='进行中';
      $('#examTag').className='tag tag-g';
    }}else if(rt.status==='busy'){{
      ec.style.display='block';
      $('#examTag').textContent='等待下发';
      $('#examTag').className='tag tag-b';
    }}else{{ec.style.display='none'}}

    // 失败模式 UI
    updateFailUI(s.manual_fail);
    $('#failMsg').value=s.manual_fail_msg||'';

    // 消息日志
    r=await fetch('/api/msgs?since='+nextSince+'&limit=200');
    var md=await r.json();
    if(md.msgs.length>0){{render(md.msgs)}}
    nextSince=md.next_since||nextSince;
    $('#logCount').textContent=md.total+' 条';
  }}catch(e){{console.error('poll error:',e)}}
}}

function render(ms){{
  var box=$('#msgBox');if(!box)return;
  // 移除占位符
  var ph=box.querySelector('.placeholder');if(ph)ph.remove();
  for(var i=0;i<ms.length;i++){{
    var m=ms[i],cls=m.dir.startsWith('SEND')?'SEND':m.dir.startsWith('RECV')?'RECV':'ERROR';
    var d=document.createElement('div');d.className='msg '+cls;
    var rh='';
    if(m.resp)rh='<div class="msg-resp">↳ '+esc(JSON.stringify(m.resp))+'</div>';
    else if(m.err)rh='<div class="msg-resp" style="color:var(--r)">✖ '+esc(m.err)+'</div>';
    d.innerHTML='<div class="msg-hd"><span class="dir">'+esc(m.dir)+'</span><span class="ts">'+esc(m.ts)+'</span></div>'+
      '<div class="msg-url">'+esc(m.url)+'</div>'+
      '<div class="msg-body">'+hl(m.payload)+'</div>'+rh;
    box.appendChild(d);
  }}
  box.scrollTop=box.scrollHeight;
}}

async function saveCfg(){{
  try{{
    var r=await fetch('/api/cfg',{{method:'POST',headers:{{'Content-Type':'application/json'}},
      body:JSON.stringify({{cc_url:$('#cfgCcUrl').value,token:$('#cfgToken').value}})}});
    var d=await r.json();
    toast(d.restarted?'已保存，心跳已重启':'已保存');
  }}catch(e){{toast('保存失败: '+e.message,'#f87171')}}
  poll();
}}

async function act(a,extra){{
  extra=extra||{{}};
  extra.action=a;
  extra.examinee_count=$('#simCount').value;
  extra.seat=$('#altSeat').value;
  extra.type=$('#altType').value;
  try{{
    var r=await fetch('/api/act',{{method:'POST',headers:{{'Content-Type':'application/json'}},body:JSON.stringify(extra)}});
    var d=await r.json();
    if(d.ok){{toast('操作成功')}}else{{toast(d.err||'失败','#f87171')}}
  }}catch(e){{toast('请求失败: '+e.message,'#f87171')}}
  // 立即刷新消息
  setTimeout(function(){{
    fetch('/api/msgs?since='+nextSince+'&limit=200').then(function(r){{return r.json()}}).then(function(md){{
      if(md.msgs.length>0){{render(md.msgs);nextSince=md.next_since||nextSince;$('#logCount').textContent=md.total+' 条';}}
    }}).catch(function(){{}});
  }},500);
  setTimeout(poll,1000);
}}

updateFailUI(failMode);
setInterval(poll,2000);
poll();
</script>
</body>
</html>"""


# ── main ──
def main():
    global PORT, HEARTBEAT_SEC

    p = argparse.ArgumentParser(description="Node Simulator")
    p.add_argument("-p", "--port", type=int, default=PORT, help="HTTP 端口")
    p.add_argument("-c", "--cc-url", default=CC_URL, help="控制中心地址")
    p.add_argument("-t", "--token", default=TOKEN, help="节点令牌")
    p.add_argument("--hb", type=int, default=HEARTBEAT_SEC, help="心跳间隔秒数")
    args = p.parse_args()

    PORT = args.port
    HEARTBEAT_SEC = args.hb
    st["cc_url"] = args.cc_url
    st["token"] = args.token

    hb_start()

    import uvicorn
    print(f"""
╔══════════════════════════════════════════╗
║  Node Simulator v1.0                     ║
╠══════════════════════════════════════════╣
║  Web UI:  http://localhost:{args.port:<5}        ║
║  CC URL:  {args.cc_url:<30} ║
║  Token:   {args.token:<30} ║
╚══════════════════════════════════════════╝
    """.strip())
    uvicorn.run(app, host="0.0.0.0", port=args.port, log_level="warning")


if __name__ == "__main__":
    main()
