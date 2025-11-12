# roles/nfe-drop-wazuh/files/ensure-certificates.sh
#!/bin/bash
set -e

WAZUH_DIR="$1"
USER="$2"

if [ -z "$WAZUH_DIR" ] || [ -z "$USER" ]; then
    echo "Uso: $0 <wazuh_dir> <user>"
    exit 1
fi

echo "🔧 Garantindo certificados Wazuh como usuário $USER..."

cd "$WAZUH_DIR/single-node"

# Para stack se estiver rodando
sudo -u "$USER" docker compose down || true

# Remove certificados problemáticos
rm -rf config/wazuh_indexer_ssl_certs
rm -rf config/wazuh_manager_ssl_certs
rm -rf config/wazuh_dashboard_ssl_certs

# Recria diretórios
mkdir -p config/wazuh_indexer_ssl_certs
mkdir -p config/wazuh_manager_ssl_certs  
mkdir -p config/wazuh_dashboard_ssl_certs

# Garante permissões
chown -R "$USER":"$USER" config/
chmod -R 755 config/

# Gera certificados
if [ -f generate-certificates.sh ]; then
    sudo -u "$USER" chmod +x generate-certificates.sh
    sudo -u "$USER" ./generate-certificates.sh
else
    echo "❌ generate-certificates.sh não encontrado em $(pwd)"
    exit 1
fi

echo "✅ Certificados garantidos!"