package migrations

import (
	"database/sql"
	"fmt"
)

// Run executa todas as migrations necessárias no banco da aplicação.
func Run(db *sql.DB) error {
	stmts := []string{
		// nfe
		`
CREATE TABLE IF NOT EXISTS nfe (
    id BIGSERIAL PRIMARY KEY,
    chave_acesso CHAR(44) NOT NULL,
    hash_integridade CHAR(64) NOT NULL,

    modelo SMALLINT NOT NULL,
    serie INTEGER NOT NULL,
    numero INTEGER NOT NULL,
    emissao TIMESTAMP(3) NOT NULL,
    tipo_operacao SMALLINT NOT NULL,
    tipo_ambiente SMALLINT NOT NULL,
    natureza_operacao VARCHAR(255) NOT NULL,

    protocolo_autorizacao VARCHAR(50),
    data_autorizacao TIMESTAMP(3),
    codigo_status SMALLINT,

    emitente_cnpj CHAR(14) NOT NULL,
    emitente_razao VARCHAR(255) NOT NULL,
    dest_cnpj_cpf CHAR(14),
    dest_razao VARCHAR(255),

    valor_total_nota NUMERIC(15,2) NOT NULL,
    valor_produtos NUMERIC(15,2) NOT NULL,
    valor_desconto NUMERIC(15,2) DEFAULT 0,
    valor_icms NUMERIC(15,2) DEFAULT 0,
    valor_ipi NUMERIC(15,2) DEFAULT 0,
    valor_pis NUMERIC(15,2) DEFAULT 0,
    valor_cofins NUMERIC(15,2) DEFAULT 0,
    valor_ii NUMERIC(15,2) DEFAULT 0,
    valor_frete NUMERIC(15,2) DEFAULT 0,
    valor_seguro NUMERIC(15,2) DEFAULT 0,

    modalidade_frete SMALLINT,

    created_at TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),

    CONSTRAINT uk_nfe_chave_acesso UNIQUE (chave_acesso),
    CONSTRAINT uk_nfe_hash_integridade UNIQUE (hash_integridade)
);
`,
		`CREATE INDEX IF NOT EXISTS idx_nfe_emissao ON nfe (emissao);`,
		`CREATE INDEX IF NOT EXISTS idx_nfe_emitente_cnpj ON nfe (emitente_cnpj);`,
		`CREATE INDEX IF NOT EXISTS idx_nfe_dest_cnpj_cpf ON nfe (dest_cnpj_cpf);`,
		`CREATE INDEX IF NOT EXISTS idx_nfe_serie_numero ON nfe (serie, numero);`,
		`CREATE INDEX IF NOT EXISTS idx_nfe_emitente_emissao ON nfe (emitente_cnpj, emissao);`,

		// nfe_xml
		`
CREATE TABLE IF NOT EXISTS nfe_xml (
    nfe_id BIGINT PRIMARY KEY,
    xml_raw TEXT NOT NULL,
    xml_json JSONB,

    created_at TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),

    CONSTRAINT fk_nfe_xml_nfe
        FOREIGN KEY (nfe_id) REFERENCES nfe(id)
        ON DELETE CASCADE
);
`,

		// nfe_item
		`
CREATE TABLE IF NOT EXISTS nfe_item (
    id BIGSERIAL PRIMARY KEY,
    nfe_id BIGINT NOT NULL,
    n_item INTEGER NOT NULL,

    codigo VARCHAR(100),
    codigo_ean VARCHAR(14),
    descricao VARCHAR(255),
    ncm CHAR(8),
    cfop CHAR(4),
    unidade VARCHAR(10),

    quantidade NUMERIC(15,4) NOT NULL,
    valor_unit NUMERIC(21,10) NOT NULL,
    valor_total_bruto NUMERIC(15,2) NOT NULL,

    valor_frete NUMERIC(15,2) DEFAULT 0,
    valor_seguro NUMERIC(15,2) DEFAULT 0,
    valor_desconto NUMERIC(15,2) DEFAULT 0,
    valor_outros NUMERIC(15,2) DEFAULT 0,
    ind_total SMALLINT NOT NULL,

    base_calculo_icms NUMERIC(15,2) DEFAULT 0,
    valor_icms NUMERIC(15,2) DEFAULT 0,
    base_calculo_icms_st NUMERIC(15,2) DEFAULT 0,
    valor_icms_st NUMERIC(15,2) DEFAULT 0,
    valor_ipi NUMERIC(15,2) DEFAULT 0,
    valor_pis NUMERIC(15,2) DEFAULT 0,
    valor_cofins NUMERIC(15,2) DEFAULT 0,

    created_at TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),

    CONSTRAINT uk_nfe_item UNIQUE (nfe_id, n_item),
    CONSTRAINT fk_nfe_item_nfe
        FOREIGN KEY (nfe_id) REFERENCES nfe(id)
        ON DELETE CASCADE
);
`,
		`CREATE INDEX IF NOT EXISTS idx_nfe_item_nfe ON nfe_item (nfe_id);`,
		`CREATE INDEX IF NOT EXISTS idx_nfe_item_ncm ON nfe_item (ncm);`,
		`CREATE INDEX IF NOT EXISTS idx_nfe_item_cfop ON nfe_item (cfop);`,
		`CREATE INDEX IF NOT EXISTS idx_nfe_item_codigo ON nfe_item (codigo);`,
		`CREATE INDEX IF NOT EXISTS idx_nfe_item_descricao ON nfe_item (descricao);`,

		// nfe_duplicatas
		`
CREATE TABLE IF NOT EXISTS nfe_duplicatas (
    id BIGSERIAL PRIMARY KEY,
    nfe_id BIGINT NOT NULL,

    numero_duplicata VARCHAR(60),
    data_vencimento DATE,
    valor_duplicata NUMERIC(15,2) NOT NULL,

    created_at TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),

    CONSTRAINT uk_nfe_duplicata UNIQUE (nfe_id, numero_duplicata),
    CONSTRAINT fk_nfe_duplicata_nfe
        FOREIGN KEY (nfe_id) REFERENCES nfe(id)
        ON DELETE CASCADE
);
`,
		`CREATE INDEX IF NOT EXISTS idx_nfe_duplicatas_nfe ON nfe_duplicatas (nfe_id);`,
		`CREATE INDEX IF NOT EXISTS idx_nfe_duplicatas_vencimento ON nfe_duplicatas (data_vencimento);`,
		`CREATE INDEX IF NOT EXISTS idx_nfe_duplicatas_numero ON nfe_duplicatas (numero_duplicata);`,

		// nfe_pagamentos
		`
CREATE TABLE IF NOT EXISTS nfe_pagamentos (
    id BIGSERIAL PRIMARY KEY,
    nfe_id BIGINT NOT NULL,

    indicador_pagamento SMALLINT,
    meio_pagamento VARCHAR(150) NOT NULL,
    valor_pagamento NUMERIC(15,2) NOT NULL,

    cnpj_credenciadora CHAR(14),
    bandeira_cartao CHAR(2),
    codigo_autorizacao VARCHAR(60),

    created_at TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),

    CONSTRAINT fk_nfe_pagamento_nfe
        FOREIGN KEY (nfe_id) REFERENCES nfe(id)
        ON DELETE CASCADE
);
`,
		`CREATE INDEX IF NOT EXISTS idx_nfe_pagamentos_nfe ON nfe_pagamentos (nfe_id);`,
		`CREATE INDEX IF NOT EXISTS idx_nfe_pagamentos_meio ON nfe_pagamentos (meio_pagamento);`,
	}

	for i, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("erro executando migration %d: %w", i+1, err)
		}
	}

	return nil
}
