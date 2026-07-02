#!/bin/bash

# 测试数据注入脚本
# 用途：批量创建 100 条 user、room、node 数据用于测试分页功能

API_BASE="http://localhost:8080"
ADMIN_USER="admin"
ADMIN_PASS="admin"

echo "=== 测试数据注入脚本 ==="
echo "目标: 创建 100 个用户、100 个教室、100 个节点"
echo ""

# 登录获取 session cookie
echo "正在登录..."
COOKIE_FILE=$(mktemp)
LOGIN_RESPONSE=$(wget -q -O- --save-cookies "$COOKIE_FILE" --keep-session-cookies \
  --post-data="username=$ADMIN_USER&password=$ADMIN_PASS" \
  "$API_BASE/login")

if echo "$LOGIN_RESPONSE" | grep -q '"success":true'; then
    echo "✓ 登录成功"
else
    echo "✗ 登录失败: $LOGIN_RESPONSE"
    rm -f "$COOKIE_FILE"
    exit 1
fi

echo ""

# 批量创建用户
echo "开始创建用户..."
SUCCESS_COUNT=0
for i in $(seq 1 100); do
    USERNAME="test_user_$(printf "%03d" $i)"
    PASSWORD="password$i"
    ROLE="proctor"

    RESPONSE=$(wget -q -O- --load-cookies "$COOKIE_FILE" \
      --header="Content-Type: application/json" \
      --post-data="{\"username\":\"$USERNAME\",\"password\":\"$PASSWORD\",\"role\":\"$ROLE\"}" \
      "$API_BASE/api/users")

    if echo "$RESPONSE" | grep -q '"success":true'; then
        SUCCESS_COUNT=$((SUCCESS_COUNT + 1))
        echo -ne "\r创建用户: $SUCCESS_COUNT/100"
    fi
done
echo ""
echo "✓ 用户创建完成: $SUCCESS_COUNT/100"
echo ""

# 批量创建教室
echo "开始创建教室..."
SUCCESS_COUNT=0
BUILDINGS=("A" "B" "C" "D" "E" "F" "G" "H")
TYPES=("阶梯教室" "大多媒体教室" "小多媒体教室" "研讨室" "会议室")

for i in $(seq 1 100); do
    BUILDING_INDEX=$((($i - 1) % ${#BUILDINGS[@]}))
    BUILDING="${BUILDINGS[$BUILDING_INDEX]}"

    TYPE_INDEX=$((($i - 1) % ${#TYPES[@]}))
    TYPE="${TYPES[$TYPE_INDEX]}"

    ROOM_NUM=$(printf "%03d" $i)
    NAME="${BUILDING}${ROOM_NUM}"
    RTSP_URL="rtsp://test.example.com/stream_$i"

    RESPONSE=$(wget -q -O- --load-cookies "$COOKIE_FILE" \
      --header="Content-Type: application/json" \
      --post-data="{\"name\":\"$NAME\",\"building\":\"$BUILDING\",\"type\":\"$TYPE\",\"rtsp_url\":\"$RTSP_URL\"}" \
      "$API_BASE/api/rooms")

    if echo "$RESPONSE" | grep -q '"success":true'; then
        SUCCESS_COUNT=$((SUCCESS_COUNT + 1))
        echo -ne "\r创建教室: $SUCCESS_COUNT/100"
    fi
done
echo ""
echo "✓ 教室创建完成: $SUCCESS_COUNT/100"
echo ""

# 批量创建节点
echo "开始创建节点..."
SUCCESS_COUNT=0
MODELS=("Standard" "Pro" "Max" "Lite")

for i in $(seq 1 100); do
    NODE_NAME="node-$(printf "%03d" $i)"

    MODEL_INDEX=$((($i - 1) % ${#MODELS[@]}))
    MODEL="${MODELS[$MODEL_INDEX]}"

    ADDRESS="192.168.1.$(($i + 100)):8002"

    RESPONSE=$(wget -q -O- --load-cookies "$COOKIE_FILE" \
      --header="Content-Type: application/json" \
      --post-data="{\"name\":\"$NODE_NAME\",\"nodemodel\":\"$MODEL\",\"address\":\"$ADDRESS\"}" \
      "$API_BASE/api/nodes")

    if echo "$RESPONSE" | grep -q '"success":true'; then
        SUCCESS_COUNT=$((SUCCESS_COUNT + 1))
        echo -ne "\r创建节点: $SUCCESS_COUNT/100"
    fi
done
echo ""
echo "✓ 节点创建完成: $SUCCESS_COUNT/100"
echo ""

# 清理
rm -f "$COOKIE_FILE"

echo "=== 数据注入完成 ==="
echo ""
echo "总结:"
echo "- 用户: 100 个监考员账号 (test_user_001 ~ test_user_100, 密码: password1 ~ password100)"
echo "- 教室: 100 个教室分布在 A-H 楼"
echo "- 节点: 100 个节点 (node-001 ~ node-100)"
echo ""
echo "现在可以测试分页功能了！"
