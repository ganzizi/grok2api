#!/bin/sh
# Copy config.example.yaml to config.yaml and fill JWT, encryption, and admin secrets.
set -eu
umask 077
cd "$(dirname "$0")/.."
if [ -f config.yaml ]; then
  echo "config.yaml already exists; not overwriting."
  exit 0
fi
if [ ! -f config.example.yaml ]; then
  echo "missing config.example.yaml" >&2
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
printf '%s\n' "$pass" > admin-password.txt
chmod 600 admin-password.txt
echo "wrote config.yaml; administrator credentials saved to admin-password.txt (mode 600)"
echo "login: http://127.0.0.1:8000  user=admin"
echo "then: docker compose up -d"
