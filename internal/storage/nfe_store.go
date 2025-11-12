package storage

import (
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"

	"nfe-drop/internal/nfe"
)

// ErrNFeAlreadyExists indica que a NFe já está no banco (chave_acesso única).
var ErrNFeAlreadyExists = errors.New("nfe já existe")

// SaveNFeWithRelations insere a NFe, itens, duplicatas, pagamentos
// e o XML bruto (nfe_xml) em uma única transação.
func SaveNFeWithRelations(db *sql.DB, parsed *nfe.ParsedNFe) (nfeID int64, err error) {
	tx, err := db.Begin()
	if err != nil {
		return 0, fmt.Errorf("erro iniciando transação: %w", err)
	}

	// Se der erro em qualquer parte, rollback.
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	nfeID, err = insertNFe(tx, parsed)
	if err != nil {
		// Se for duplicata, deixamos o caller decidir o que fazer.
		if errors.Is(err, ErrNFeAlreadyExists) {
			return 0, ErrNFeAlreadyExists
		}
		return 0, err
	}

	if err = insertNFeXML(tx, nfeID, parsed); err != nil {
		return 0, err
	}

	if err = insertItens(tx, nfeID, parsed.Itens); err != nil {
		return 0, err
	}

	if err = insertDuplicatas(tx, nfeID, parsed.Duplicatas); err != nil {
		return 0, err
	}

	if err = insertPagamentos(tx, nfeID, parsed.Pagamentos); err != nil {
		return 0, err
	}

	if err = tx.Commit(); err != nil {
		return 0, fmt.Errorf("erro no commit da transação: %w", err)
	}

	slog.Info("NFe persistida com sucesso",
		"nfe_id", nfeID,
		"chave", parsed.ChaveAcesso,
		"itens", len(parsed.Itens),
		"duplicatas", len(parsed.Duplicatas),
		"pagamentos", len(parsed.Pagamentos),
	)

	return nfeID, nil
}

func insertNFe(tx *sql.Tx, p *nfe.ParsedNFe) (int64, error) {
	var id int64

	// emissao é NOT NULL na migration, então se vier vazio é erro
	emissao := strings.TrimSpace(p.EmissaoDate)
	if emissao == "" {
		return 0, fmt.Errorf("emissao vazia para chave %s", p.ChaveAcesso)
	}

	dataAut := toNullDate(p.DataAutorizacao)

	const q = `
INSERT INTO nfe (
	chave_acesso,
	hash_integridade,
	modelo,
	serie,
	numero,
	emissao,
	tipo_operacao,
	tipo_ambiente,
	natureza_operacao,
	protocolo_autorizacao,
	data_autorizacao,
	codigo_status,
	emitente_cnpj,
	emitente_razao,
	dest_cnpj_cpf,
	dest_razao,
	valor_total_nota,
	valor_produtos,
	valor_desconto,
	valor_icms,
	valor_ipi,
	valor_pis,
	valor_cofins,
	valor_ii,
	valor_frete,
	valor_seguro,
	modalidade_frete
) VALUES (
	$1,$2,$3,$4,$5,$6,
	$7,$8,$9,
	$10,$11,$12,
	$13,$14,$15,$16,
	$17,$18,$19,$20,$21,$22,$23,$24,$25,$26,$27
)
RETURNING id;
`

	err := tx.QueryRow(
		q,
		p.ChaveAcesso,
		p.HashIntegridade,
		p.Modelo,
		p.Serie,
		p.Numero,
		emissao, // Postgres aceita "YYYY-MM-DD" em TIMESTAMP(3)
		p.TipoOperacao,
		p.TipoAmbiente,
		p.NaturezaOperacao,
		nullableString(p.ProtocoloAut),
		dataAut,
		p.CodigoStatus,
		p.EmitenteCNPJ,
		p.EmitenteRazao,
		nullableString(p.DestCNPJCPF),
		nullableString(p.DestRazao),
		p.ValorTotalNota,
		p.ValorProdutos,
		p.ValorDesconto,
		p.ValorICMS,
		p.ValorIPI,
		p.ValorPIS,
		p.ValorCOFINS,
		p.ValorII,
		p.ValorFrete,
		p.ValorSeguro,
		p.ModalidadeFrete,
	).Scan(&id)

	if err != nil {
		// Detecta erro de unique constraint (chave duplicada).
		if isUniqueViolation(err) {
			slog.Warn("NFe já existe no banco, ignorando reprocessamento",
				"chave", p.ChaveAcesso,
			)
			return 0, ErrNFeAlreadyExists
		}
		return 0, fmt.Errorf("erro inserindo nfe (chave=%s): %w", p.ChaveAcesso, err)
	}

	return id, nil
}

// nfe_xml: guarda o XML bruto + json (se um dia você quiser popular)
func insertNFeXML(tx *sql.Tx, nfeID int64, p *nfe.ParsedNFe) error {
	const q = `
INSERT INTO nfe_xml (
	nfe_id,
	xml_raw,
	xml_json
) VALUES (
	$1,$2,$3
);
`
	xmlRaw := string(p.XMLRaw)

	_, err := tx.Exec(
		q,
		nfeID,
		xmlRaw,
		nil, // por enquanto não estamos populando xml_json
	)
	if err != nil {
		return fmt.Errorf("erro inserindo nfe_xml (nfe_id=%d): %w", nfeID, err)
	}

	return nil
}

func insertItens(tx *sql.Tx, nfeID int64, itens []nfe.ParsedItem) error {
	if len(itens) == 0 {
		return nil
	}

	const q = `
INSERT INTO nfe_item (
	nfe_id,
	n_item,
	codigo,
	codigo_ean,
	descricao,
	ncm,
	cfop,
	unidade,
	quantidade,
	valor_unit,
	valor_total_bruto,
	valor_frete,
	valor_seguro,
	valor_desconto,
	valor_outros,
	ind_total,
	base_calculo_icms,
	valor_icms,
	base_calculo_icms_st,
	valor_icms_st,
	valor_ipi,
	valor_pis,
	valor_cofins
) VALUES (
	$1,$2,$3,$4,$5,$6,$7,$8,
	$9,$10,$11,$12,$13,$14,$15,$16,
	$17,$18,$19,$20,$21,$22,$23
);
`

	for _, it := range itens {
		_, err := tx.Exec(
			q,
			nfeID,
			it.NItem,
			nullableString(it.Codigo),
			nullableString(it.CodigoEAN),
			nullableString(it.Descricao),
			nullableString(it.NCM),
			nullableString(it.CFOP),
			nullableString(it.Unidade),
			it.Quantidade,
			it.ValorUnitario,
			it.ValorTotalBruto,
			it.ValorFrete,
			it.ValorSeguro,
			it.ValorDesconto,
			it.ValorOutros,
			it.IndTotal,
			it.BaseCalculoICMS,
			it.ValorICMS,
			it.BaseCalculoICMSST,
			it.ValorICMSST,
			it.ValorIPI,
			it.ValorPIS,
			it.ValorCOFINS,
		)
		if err != nil {
			return fmt.Errorf("erro inserindo item n_item=%d da nfe_id=%d: %w", it.NItem, nfeID, err)
		}
	}

	return nil
}

func insertDuplicatas(tx *sql.Tx, nfeID int64, dups []nfe.ParsedDuplicata) error {
	if len(dups) == 0 {
		return nil
	}

	const q = `
INSERT INTO nfe_duplicatas (
	nfe_id,
	numero_duplicata,
	data_vencimento,
	valor_duplicata
) VALUES (
	$1,$2,$3,$4
);
`

	for _, d := range dups {
		_, err := tx.Exec(
			q,
			nfeID,
			nullableString(d.Numero),
			toNullDate(d.DataVencimento),
			d.Valor,
		)
		if err != nil {
			return fmt.Errorf("erro inserindo duplicata nfe_id=%d numero=%s: %w", nfeID, d.Numero, err)
		}
	}

	return nil
}

func insertPagamentos(tx *sql.Tx, nfeID int64, pags []nfe.ParsedPagamento) error {
	if len(pags) == 0 {
		return nil
	}

	const q = `
INSERT INTO nfe_pagamentos (
	nfe_id,
	indicador_pagamento,
	meio_pagamento,
	valor_pagamento,
	cnpj_credenciadora,
	bandeira_cartao,
	codigo_autorizacao
) VALUES (
	$1,$2,$3,$4,$5,$6,$7
);
`

	for _, p := range pags {
		var ind interface{}
		if p.IndicadorPagamento != nil {
			ind = *p.IndicadorPagamento
		} else {
			ind = nil
		}

		_, err := tx.Exec(
			q,
			nfeID,
			ind,
			nullableString(p.MeioPagamento),
			p.Valor,
			nullableString(p.CNPJCredenciadora),
			nullableString(p.BandeiraCartao),
			nullableString(p.CodigoAutorizacao),
		)
		if err != nil {
			return fmt.Errorf("erro inserindo pagamento nfe_id=%d meio_pagamento=%s: %w", nfeID, p.MeioPagamento, err)
		}
	}

	return nil
}

// ========================= helpers =============================

func toNullDate(s string) interface{} {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	// "YYYY-MM-DD" cai direto em DATE/TIMESTAMP no Postgres
	return s
}

func nullableString(s string) interface{} {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	return s
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505"
	}
	return false
}
