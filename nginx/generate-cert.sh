#!/usr/bin/env bash
set -euo pipefail

# Generate self-signed SSL certificate for development / testing
# For production, replace with Let's Encrypt or real certs.

CERT_DIR="$(cd "$(dirname "$0")" && pwd)/ssl"
DAYS=365
COUNTRY=CN
STATE=Shanghai
CITY=Shanghai
ORG=XiaoTianQuant
CN=localhost

mkdir -p "$CERT_DIR"

echo "=== Generating self-signed SSL certificate ==="

openssl req -x509 -nodes -days "$DAYS" -newkey rsa:2048 \
  -keyout "$CERT_DIR/key.pem" \
  -out "$CERT_DIR/cert.pem" \
  -subj "/C=$COUNTRY/ST=$STATE/L=$CITY/O=$ORG/CN=$CN" \
  -addext "subjectAltName=DNS:$CN,DNS:*.localhost,IP:127.0.0.1"

chmod 600 "$CERT_DIR/key.pem"
chmod 644 "$CERT_DIR/cert.pem"

echo "✅ SSL cert generated:"
echo "   Cert: $CERT_DIR/cert.pem"
echo "   Key:  $CERT_DIR/key.pem"
echo ""
echo "⚠️  This is a SELF-SIGNED cert for development only."
echo "   For production, use Let's Encrypt: certbot certonly --nginx"
