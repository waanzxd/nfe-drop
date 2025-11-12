#!/usr/bin/env bash
set -e

echo "[nfe-drop] Instalando Filebeat..."

# Instala dependências (se já tiver, o apt só ignora)
sudo apt update
sudo apt install -y wget gnupg apt-transport-https

# Adiciona repo da Elastic (idempotente)
if [ ! -f /usr/share/keyrings/elastic.gpg ]; then
  wget -qO - https://artifacts.elastic.co/GPG-KEY-elasticsearch \
    | sudo gpg --dearmor -o /usr/share/keyrings/elastic.gpg
fi

if [ ! -f /etc/apt/sources.list.d/elastic-8.x.list ]; then
  echo "deb [signed-by=/usr/share/keyrings/elastic.gpg] https://artifacts.elastic.co/packages/8.x/apt stable main" \
    | sudo tee /etc/apt/sources.list.d/elastic-8.x.list
fi

sudo apt update
sudo apt install -y filebeat

echo "[nfe-drop] Filebeat instalado."
