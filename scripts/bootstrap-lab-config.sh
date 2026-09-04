#!/bin/sh
# 复制 config.example.yaml 为 config.yaml，并生成 JWT、加密和管理员密钥。
set -eu
cd "$(dirname "$0")/.."
if [ -f config.yaml ]; then
  echo "config.yaml 已存在，不覆盖。"
  exit 0
fi
if [ ! -f config.example.yaml ]; then
  echo "缺少 config.example.yaml" >&2
  exit 1
fi
jwt=$(openssl rand -hex 32)
key=$(openssl rand -base64 32)
pass=$(openssl rand -base64 18 | tr -d '/+=' | cut -c1-24)
awk -v jwt="$jwt" -v key="$key" -v pass="$pass" '
  /jwtSecret:/ { print "  jwtSecret: \"" jwt "\""; next }
  /credentialEncryptionKey:/ { print "  credentialEncryptionKey: \"" key "\""; next }
  /password: "replace-with-a-strong-password"/ { print "  password: \"" pass "\""; next }
  { print }
' config.example.yaml > config.yaml
chmod 600 config.yaml
echo "已写入 config.yaml（管理员密码仅显示一次）：$pass"
echo "登录地址：http://127.0.0.1:8000，用户：admin"
echo "下一步：docker compose up -d"
