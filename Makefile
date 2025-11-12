# ==============================================================================
# Variáveis dinâmicas
# ==============================================================================

USER        := $(shell whoami)
HOME_DIR    := $(shell echo $$HOME)
PROJECT_DIR := $(shell pwd)
BIN_DIR     := $(PROJECT_DIR)/bin

# ==============================================================================
# Alvos principais / usabilidade
# ==============================================================================

.PHONY: help deps schemas-install migration migration-force build \
        systemd-install start-services stop-services restart-services status-services \
        bootstrap

## Exibe lista de comandos.
help:
	@awk ' \
		BEGIN { n = 0; max_width = 15; pending_desc = ""; \
			green = "\033[32m"; cyan = "\033[36m"; reset = "\033[0m"; \
			printf "\n%sUso:%s\n  make %s<alvo>%s\n\n", cyan, reset, green, reset; \
			printf "%sAlvos disponíveis:%s\n", cyan, reset; } \
		/^[ \t]*##/ { pending_desc = $$0; sub(/^[ \t]*## ?/, "", pending_desc); if ($$0 !~ /:.*##/) { next; } } \
		/^[a-zA-Z0-9_-]+:/ { target = $$0; sub(/:.*/, "", target); desc = pending_desc; \
			if ($$0 ~ /:.*##/) { desc = $$0; sub(/.*## ?/, "", desc); } \
			if (desc != "") { n++; if (length(target) > max_width) max_width = length(target); \
				targets[n] = target; descs[n] = desc; } pending_desc = ""; } \
		END { for (i = 1; i <= n; i++) printf "  %s%-" max_width "s%s  %s\n", green, targets[i], reset, descs[i]; printf "\n"; } \
	' $(firstword $(MAKEFILE_LIST))

## Instala/atualiza dependências Go (go mod tidy).
deps:
	@echo "📦 Instalando/atualizando dependências Go..."
	go mod tidy
	@echo "✅ Dependências ok."

## Baixa/atualiza schemas do sped-nfe em third_party/sped-nfe.
schemas-install:
	@echo "📚 Instalando/atualizando schemas do sped-nfe..."
	mkdir -p third_party
	if [ ! -d "third_party/sped-nfe/.git" ]; then \
	  echo "→ Clonando repositório sped-nfe..."; \
	  git clone https://github.com/nfephp-org/sped-nfe.git third_party/sped-nfe; \
	else \
	  echo "→ Repositório já existe, dando git pull..."; \
	  cd third_party/sped-nfe && git pull --ff-only; \
	fi
	@echo "✅ Schemas disponíveis em third_party/sped-nfe/schemes"

## Executa migrations em modo seguro (não dropa banco existente).
migration:
	@echo "🧬 Executando migrations (modo seguro)..."
	go run ./cmd/nfe-drop-migrator
	@echo "✅ Migrations finalizadas."

## Recria o banco do zero (DROP + CREATE + migrations). CUIDADO.
migration-force:
	@echo "💣 ATENÇÃO: isto irá DROPAR E RECRIAR o banco configurado em NFE_DROP_DB_NAME!"
	go run ./cmd/nfe-drop-migrator --force
	@echo "✅ Banco recriado e migrations aplicadas."

## Compila os binários do watcher e do worker.
build:
	@echo "🔨 Build binários em $(BIN_DIR)..."
	mkdir -p "$(BIN_DIR)"
	go build -o "$(BIN_DIR)/nfe-drop-watcher" ./cmd/nfe-drop-watcher
	go build -o "$(BIN_DIR)/nfe-drop-worker"  ./cmd/nfe-drop-worker
	@echo "✅ Build ok."

## Gera e instala os services do systemd com USER/caminhos dinâmicos.
systemd-install: build
	@echo "🧩 Gerando nfe-drop-watcher.service com USER=$(USER), PROJECT_DIR=$(PROJECT_DIR)..."
	printf "[Unit]\n\
Description=NFE Drop - Watcher (monitor de pasta incoming)\n\
After=network.target\n\
\n\
[Service]\n\
User=$(USER)\n\
Group=$(USER)\n\
WorkingDirectory=$(PROJECT_DIR)\n\
ExecStart=$(BIN_DIR)/nfe-drop-watcher\n\
Restart=always\n\
RestartSec=5\n\
Environment=HOME=$(HOME_DIR)\n\
Environment=USER=$(USER)\n\
\n\
[Install]\n\
WantedBy=multi-user.target\n" | sudo tee /etc/systemd/system/nfe-drop-watcher.service > /dev/null

	@echo "🧩 Gerando nfe-drop-worker.service com USER=$(USER), PROJECT_DIR=$(PROJECT_DIR)..."
	printf "[Unit]\n\
Description=NFE Drop - Worker (processador de XML/ZIP)\n\
After=network.target nfe-drop-watcher.service\n\
\n\
[Service]\n\
User=$(USER)\n\
Group=$(USER)\n\
WorkingDirectory=$(PROJECT_DIR)\n\
ExecStart=$(BIN_DIR)/nfe-drop-worker\n\
Restart=always\n\
RestartSec=5\n\
Environment=HOME=$(HOME_DIR)\n\
Environment=USER=$(USER)\n\
\n\
[Install]\n\
WantedBy=multi-user.target\n" | sudo tee /etc/systemd/system/nfe-drop-worker.service > /dev/null

	@echo "🔄 Recarregando systemd e habilitando serviços..."
	sudo systemctl daemon-reload
	sudo systemctl enable nfe-drop-watcher
	sudo systemctl enable nfe-drop-worker
	@echo "✅ systemd-install concluído."

## Inicia watcher e worker via systemd.
start-services:
	@echo "🚀 Iniciando serviços..."
	sudo systemctl start nfe-drop-watcher
	sudo systemctl start nfe-drop-worker
	@echo "✅ Serviços iniciados."

## Para watcher e worker via systemd.
stop-services:
	@echo "🛑 Parando serviços..."
	- sudo systemctl stop nfe-drop-watcher
	- sudo systemctl stop nfe-drop-worker
	@echo "✅ Serviços parados."

## Reinicia watcher e worker via systemd.
restart-services:
	@echo "🔁 Reiniciando serviços..."
	sudo systemctl restart nfe-drop-watcher
	sudo systemctl restart nfe-drop-worker
	@echo "✅ Serviços reiniciados."

## Mostra status de watcher e worker.
status-services:
	@echo "📊 Status nfe-drop-watcher:"
	systemctl status nfe-drop-watcher --no-pager || true
	@echo ""
	@echo "📊 Status nfe-drop-worker:"
	systemctl status nfe-drop-worker --no-pager || true

## Fluxo completo pós-clone: deps + schemas + migration + systemd-install.
bootstrap: deps schemas-install migration systemd-install
	@echo "🚀 Bootstrap concluído. Agora rode: make start-services"

## Instala e configura filebeat para enviar logs do nfe-drop ao Graylog.
filebeat-install:
	@echo "==> Instalando Filebeat..."
	sudo apt update
	sudo apt install -y filebeat
	@echo "==> Instalando configuração do Filebeat para nfe-drop..."
	sudo install -m 644 deploy/filebeat/filebeat.nfe-drop.yml /etc/filebeat/filebeat.yml
	@echo "==> Habilitando e iniciando Filebeat..."
	sudo systemctl enable filebeat
	sudo systemctl restart filebeat
	@echo "==> Filebeat instalado e configurado."
