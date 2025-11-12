package nfe

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	xsdvalidate "github.com/form3tech-oss/go-xsd-validate"
)

// ============================================================================
// Tipos de saída – modelados pra bater com as tabelas
// ============================================================================

// ParsedNFe é o objeto completo pra alimentar as tabelas nfe + relacionamentos.
type ParsedNFe struct {
	// Tabela nfe
	ChaveAcesso       string
	HashIntegridade   string
	Modelo            int
	Serie             int
	Numero            int
	EmissaoDate       string // YYYY-MM-DD
	TipoOperacao      int
	TipoAmbiente      int
	NaturezaOperacao  string
	ProtocoloAut      string
	DataAutorizacao   string // YYYY-MM-DD
	CodigoStatus      int
	EmitenteCNPJ      string
	EmitenteRazao     string
	DestCNPJCPF       string
	DestRazao         string
	ValorTotalNota    float64
	ValorProdutos     float64
	ValorDesconto     float64
	ValorICMS         float64
	ValorIPI          float64
	ValorPIS          float64
	ValorCOFINS       float64
	ValorII           float64
	ValorFrete        float64
	ValorSeguro       float64
	ModalidadeFrete   int
	XMLRaw            []byte

	// Tabela nfe_item
	Itens []ParsedItem

	// Tabela nfe_duplicatas
	Duplicatas []ParsedDuplicata

	// Tabela nfe_pagamentos
	Pagamentos []ParsedPagamento
}

type ParsedItem struct {
	NItem              int
	Codigo             string
	CodigoEAN          string
	Descricao          string
	NCM                string
	CFOP               string
	Unidade            string
	Quantidade         float64
	ValorUnitario      float64
	ValorTotalBruto    float64
	ValorFrete         float64
	ValorSeguro        float64
	ValorDesconto      float64
	ValorOutros        float64
	IndTotal           int
	BaseCalculoICMS    float64
	ValorICMS          float64
	BaseCalculoICMSST  float64
	ValorICMSST        float64
	ValorIPI           float64
	ValorPIS           float64
	ValorCOFINS        float64
}

type ParsedDuplicata struct {
	Numero          string
	DataVencimento  string // YYYY-MM-DD
	Valor           float64
}

type ParsedPagamento struct {
	IndicadorPagamento *int   // indPag (0=à vista, 1=a prazo, etc.)
	MeioPagamento      string // tPag (01=Dinheiro, 03=Cartão crédito, etc.)
	Valor              float64
	CNPJCredenciadora  string
	BandeiraCartao     string
	CodigoAutorizacao  string
}

// ============================================================================
// Estruturas mínimas do XML da NF-e (nfeProc, NFe, infNFe, etc.)
// ============================================================================

type nfeProc struct {
	XMLName xml.Name `xml:"nfeProc"`
	NFe     nfe      `xml:"NFe"`
	ProtNFe *protNFe `xml:"protNFe"`
}

type protNFe struct {
	InfProt struct {
		ChNFe  string `xml:"chNFe"`
		DhRecb string `xml:"dhRecbto"`
		NProt  string `xml:"nProt"`
		CStat  string `xml:"cStat"`
	} `xml:"infProt"`
}

type nfe struct {
	XMLName xml.Name `xml:"NFe"`
	InfNFe  infNFe   `xml:"infNFe"`
}

type infNFe struct {
	ID     string `xml:"Id,attr"`
	Versao string `xml:"versao,attr"`

	Ide    ide     `xml:"ide"`
	Emit   emit    `xml:"emit"`
	Dest   *dest   `xml:"dest"`
	Det    []det   `xml:"det"`
	Total  total   `xml:"total"`
	Transp *transp `xml:"transp"`
	Cobr   *cobr   `xml:"cobr"`
	Pag    *pag    `xml:"pag"`
}

type ide struct {
	Modelo int    `xml:"mod"`
	Serie  int    `xml:"serie"`
	NNF    int    `xml:"nNF"`
	DhEmi  string `xml:"dhEmi"` // 4.00
	DEmi   string `xml:"dEmi"`  // 3.10/antigas
	TpNF   int    `xml:"tpNF"`
	TpAmb  int    `xml:"tpAmb"`
	NatOp  string `xml:"natOp"`
}

type emit struct {
	CNPJ  string `xml:"CNPJ"`
	XNome string `xml:"xNome"`
}

type dest struct {
	CNPJ  string `xml:"CNPJ"`
	CPF   string `xml:"CPF"`
	XNome string `xml:"xNome"`
}

type transp struct {
	ModFrete string `xml:"modFrete"`
}

type total struct {
	ICMSTot icmsTot `xml:"ICMSTot"`
}

type icmsTot struct {
	VNF     string `xml:"vNF"`
	VProd   string `xml:"vProd"`
	VDesc   string `xml:"vDesc"`
	VICMS   string `xml:"vICMS"`
	VIPI    string `xml:"vIPI"`
	VPIS    string `xml:"vPIS"`
	VCOFINS string `xml:"vCOFINS"`
	VII     string `xml:"vII"`
	VFrete  string `xml:"vFrete"`
	VSeg    string `xml:"vSeg"`
}

// ------------------------- Itens (det/prod/imposto) -------------------------

type det struct {
	NItem   string   `xml:"nItem,attr"`
	Prod    prod     `xml:"prod"`
	Imposto imposto  `xml:"imposto"`
}

type prod struct {
	CProd   string `xml:"cProd"`
	CEAN    string `xml:"cEAN"`
	XProd   string `xml:"xProd"`
	NCM     string `xml:"NCM"`
	CFOP    string `xml:"CFOP"`
	UCom    string `xml:"uCom"`
	QCom    string `xml:"qCom"`
	VUnCom  string `xml:"vUnCom"`
	VProd   string `xml:"vProd"`
	VFrete  string `xml:"vFrete"`
	VSeg    string `xml:"vSeg"`
	VDesc   string `xml:"vDesc"`
	VOutro  string `xml:"vOutro"`
	IndTot  string `xml:"indTot"`
}

type imposto struct {
	ICMS   icmsGroup `xml:"ICMS"`
	IPI    *ipi      `xml:"IPI"`
	PIS    *pis      `xml:"PIS"`
	COFINS *cofins   `xml:"COFINS"`
}

// ICMS pode vir em vários formatos
type icmsGroup struct {
	ICMS00    *icmsVal    `xml:"ICMS00"`
	ICMS10    *icmsSTVal  `xml:"ICMS10"`
	ICMS20    *icmsVal    `xml:"ICMS20"`
	ICMS30    *icmsSTVal  `xml:"ICMS30"`
	ICMS40    *icmsSimple `xml:"ICMS40"`
	ICMS51    *icmsVal    `xml:"ICMS51"`
	ICMS60    *icmsSTOnly `xml:"ICMS60"`
	ICMS70    *icmsSTVal  `xml:"ICMS70"`
	ICMS90    *icmsSTVal  `xml:"ICMS90"`
	ICMSPart  *icmsSTVal  `xml:"ICMSPart"`
	ICMSSN101 *icmsSimple `xml:"ICMSSN101"`
	ICMSSN102 *icmsSimple `xml:"ICMSSN102"`
	ICMSSN201 *icmsSTVal  `xml:"ICMSSN201"`
	ICMSSN202 *icmsSTVal  `xml:"ICMSSN202"`
	ICMSSN500 *icmsSTOnly `xml:"ICMSSN500"`
	ICMSSN900 *icmsSTVal  `xml:"ICMSSN900"`
}

type icmsVal struct {
	VBC   string `xml:"vBC"`
	VICMS string `xml:"vICMS"`
}

type icmsSTVal struct {
	VBC    string `xml:"vBC"`
	VICMS  string `xml:"vICMS"`
	VBCST  string `xml:"vBCST"`
	VICMSST string `xml:"vICMSST"`
}

type icmsSTOnly struct {
	VBCST   string `xml:"vBCST"`
	VICMSST string `xml:"vICMSST"`
}

type icmsSimple struct{}

// IPI

type ipi struct {
	Trib *ipiTrib `xml:"IPITrib"`
	NT   *ipiNT   `xml:"IPINT"`
}

type ipiTrib struct {
	VIPI string `xml:"vIPI"`
}

type ipiNT struct{}

// PIS

type pis struct {
	Aliq *pisVal `xml:"PISAliq"`
	Qtde *pisVal `xml:"PISQtde"`
	NT   *pisVal `xml:"PISNT"`
	Outr *pisVal `xml:"PISOutr"`
}

type pisVal struct {
	VPIS string `xml:"vPIS"`
}

// COFINS

type cofins struct {
	Aliq *cofinsVal `xml:"COFINSAliq"`
	Qtde *cofinsVal `xml:"COFINSQtde"`
	NT   *cofinsVal `xml:"COFINSNT"`
	Outr *cofinsVal `xml:"COFINSOutr"`
}

type cofinsVal struct {
	VCOFINS string `xml:"vCOFINS"`
}

// ---------------------------- Cobr / Duplicatas -----------------------------

type cobr struct {
	Duplicatas []dup `xml:"dup"`
}

type dup struct {
	Numero string `xml:"nDup"`
	DVenc  string `xml:"dVenc"`
	Valor  string `xml:"vDup"`
}

// ---------------------------- Pagamentos ------------------------------------

type pag struct {
	Det []detPag `xml:"detPag"`
}

type detPag struct {
	IndPag string `xml:"indPag"`
	Meio   string `xml:"tPag"`
	Valor  string `xml:"vPag"`
	Card   *card  `xml:"card"`
}

type card struct {
	CNPJ    string `xml:"CNPJ"`
	Bandeira string `xml:"tBand"`
	Aut     string `xml:"cAut"`
}

// ============================================================================
// Função principal de parse + XSD
// ============================================================================

func ParseFile(path string) (*ParsedNFe, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("erro lendo XML %s: %w", path, err)
	}

	// hash_integridade = SHA-256 do XML bruto
	hash := sha256.Sum256(data)
	hashHex := hex.EncodeToString(hash[:])

	// Validação XSD (opcional, controlada por env)
	xsdEnabled := strings.ToLower(os.Getenv("NFE_XSD_ENABLED"))
	if xsdEnabled == "true" || xsdEnabled == "1" || xsdEnabled == "yes" {
		xsdDir := os.Getenv("NFE_XSD_DIR")
		xsdMain := os.Getenv("NFE_XSD_MAIN")
		if xsdDir == "" {
			return nil, fmt.Errorf("NFE_XSD_ENABLED=true mas NFE_XSD_DIR não foi definido")
		}
		if xsdMain == "" {
			return nil, fmt.Errorf("NFE_XSD_ENABLED=true mas NFE_XSD_MAIN não foi definido (ex: procNFe_v4.00.xsd)")
		}
		xsdPath, err := resolveXSDPath(xsdDir, xsdMain)
		if err != nil {
			return nil, err
		}
		if err := validateXMLWithXSD(data, xsdPath); err != nil {
			return nil, err
		}
	}

	// 1) tenta nfeProc
	var proc nfeProc
	if err := xml.Unmarshal(data, &proc); err == nil && proc.NFe.InfNFe.Ide.Modelo != 0 {
		return buildParsedFrom(proc, data, hashHex)
	}

	// 2) tenta NFe "simples"
	var n nfe
	if err := xml.Unmarshal(data, &n); err == nil && n.InfNFe.Ide.Modelo != 0 {
		return buildParsedFrom(n, data, hashHex)
	}

	return nil, fmt.Errorf("XML não reconhecido como nfeProc ou NFe (arquivo: %s)", path)
}

func buildParsedFrom(v interface{}, xmlRaw []byte, hashHex string) (*ParsedNFe, error) {
	var inf infNFe
	var prot *protNFe

	switch t := v.(type) {
	case nfeProc:
		inf = t.NFe.InfNFe
		prot = t.ProtNFe
	case nfe:
		inf = t.InfNFe
	default:
		return nil, fmt.Errorf("tipo inesperado em buildParsedFrom")
	}

	p := &ParsedNFe{
		HashIntegridade: hashHex,
		XMLRaw:          xmlRaw,
	}

	// Chave de acesso
	chave := ""
	if prot != nil && prot.InfProt.ChNFe != "" {
		chave = onlyDigits(prot.InfProt.ChNFe)
	}
	if chave == "" {
		// tenta extrair do Id (NFe + chave)
		chave = onlyDigits(extractChave(inf.ID))
	}
	p.ChaveAcesso = chave

	// Cabeçalho
	p.Modelo = inf.Ide.Modelo
	p.Serie = inf.Ide.Serie
	p.Numero = inf.Ide.NNF

	// Data de emissão – pode vir em dhEmi (datetime) ou dEmi (date)
	emissao := inf.Ide.DhEmi
	if emissao == "" {
		emissao = inf.Ide.DEmi
	}
	p.EmissaoDate = normalizeDateYMD(emissao)

	p.TipoOperacao = inf.Ide.TpNF
	p.TipoAmbiente = inf.Ide.TpAmb
	p.NaturezaOperacao = strings.TrimSpace(inf.Ide.NatOp)

	// Emitente / Destinatário
	p.EmitenteCNPJ = onlyDigits(inf.Emit.CNPJ)
	p.EmitenteRazao = strings.TrimSpace(inf.Emit.XNome)

	if inf.Dest != nil {
		doc := inf.Dest.CNPJ
		if doc == "" {
			doc = inf.Dest.CPF
		}
		p.DestCNPJCPF = onlyDigits(doc)
		p.DestRazao = strings.TrimSpace(inf.Dest.XNome)
	}

	// Totais
	p.ValorTotalNota = parseFloat(inf.Total.ICMSTot.VNF)
	p.ValorProdutos = parseFloat(inf.Total.ICMSTot.VProd)
	p.ValorDesconto = parseFloat(inf.Total.ICMSTot.VDesc)
	p.ValorICMS = parseFloat(inf.Total.ICMSTot.VICMS)
	p.ValorIPI = parseFloat(inf.Total.ICMSTot.VIPI)
	p.ValorPIS = parseFloat(inf.Total.ICMSTot.VPIS)
	p.ValorCOFINS = parseFloat(inf.Total.ICMSTot.VCOFINS)
	p.ValorII = parseFloat(inf.Total.ICMSTot.VII)
	p.ValorFrete = parseFloat(inf.Total.ICMSTot.VFrete)
	p.ValorSeguro = parseFloat(inf.Total.ICMSTot.VSeg)

	// Modalidade de frete
	if inf.Transp != nil {
		p.ModalidadeFrete = parseInt(inf.Transp.ModFrete)
	}

	// Protocolo / autorização
	if prot != nil {
		p.ProtocoloAut = strings.TrimSpace(prot.InfProt.NProt)
		p.DataAutorizacao = normalizeDateYMD(prot.InfProt.DhRecb)
		p.CodigoStatus = parseInt(prot.InfProt.CStat)
	}

	// Itens
	for _, d := range inf.Det {
		item := buildItemFromDet(d)
		p.Itens = append(p.Itens, item)
	}

	// Duplicatas
	if inf.Cobr != nil {
		for _, du := range inf.Cobr.Duplicatas {
			p.Duplicatas = append(p.Duplicatas, ParsedDuplicata{
				Numero:         strings.TrimSpace(du.Numero),
				DataVencimento: normalizeYMDDate(du.DVenc),
				Valor:          parseFloat(du.Valor),
			})
		}
	}

	// Pagamentos
	if inf.Pag != nil {
		for _, dp := range inf.Pag.Det {
			var indPtr *int
			if strings.TrimSpace(dp.IndPag) != "" {
				v := parseInt(dp.IndPag)
				indPtr = &v
			}
			var cnpjCred, band, aut string
			if dp.Card != nil {
				cnpjCred = onlyDigits(dp.Card.CNPJ)
				band = strings.TrimSpace(dp.Card.Bandeira)
				aut = strings.TrimSpace(dp.Card.Aut)
			}
			p.Pagamentos = append(p.Pagamentos, ParsedPagamento{
				IndicadorPagamento: indPtr,
				MeioPagamento:      strings.TrimSpace(dp.Meio),
				Valor:              parseFloat(dp.Valor),
				CNPJCredenciadora:  cnpjCred,
				BandeiraCartao:     band,
				CodigoAutorizacao:  aut,
			})
		}
	}

	return p, nil
}

// ============================================================================
// Helpers para itens e impostos
// ============================================================================

func buildItemFromDet(d det) ParsedItem {
	var item ParsedItem

	item.NItem = parseInt(d.NItem)
	item.Codigo = strings.TrimSpace(d.Prod.CProd)
	item.CodigoEAN = strings.TrimSpace(d.Prod.CEAN)
	item.Descricao = strings.TrimSpace(d.Prod.XProd)
	item.NCM = strings.TrimSpace(d.Prod.NCM)
	item.CFOP = strings.TrimSpace(d.Prod.CFOP)
	item.Unidade = strings.TrimSpace(d.Prod.UCom)

	item.Quantidade = parseFloat(d.Prod.QCom)
	item.ValorUnitario = parseFloat(d.Prod.VUnCom)
	item.ValorTotalBruto = parseFloat(d.Prod.VProd)

	item.ValorFrete = parseFloat(d.Prod.VFrete)
	item.ValorSeguro = parseFloat(d.Prod.VSeg)
	item.ValorDesconto = parseFloat(d.Prod.VDesc)
	item.ValorOutros = parseFloat(d.Prod.VOutro)

	item.IndTotal = parseInt(d.Prod.IndTot)

	// Impostos
	bcICMS, vICMS, bcST, vICMSST := extractICMS(d.Imposto.ICMS)
	item.BaseCalculoICMS = bcICMS
	item.ValorICMS = vICMS
	item.BaseCalculoICMSST = bcST
	item.ValorICMSST = vICMSST

	item.ValorIPI = extractIPI(d.Imposto.IPI)
	item.ValorPIS = extractPIS(d.Imposto.PIS)
	item.ValorCOFINS = extractCOFINS(d.Imposto.COFINS)

	return item
}

func extractICMS(g icmsGroup) (bc, vicms, bcst, vicmsst float64) {
	// vamos na primeira estrutura existente que tenha valores relevantes

	if g.ICMS00 != nil {
		bc = parseFloat(g.ICMS00.VBC)
		vicms = parseFloat(g.ICMS00.VICMS)
		return
	}
	if g.ICMS20 != nil {
		bc = parseFloat(g.ICMS20.VBC)
		vicms = parseFloat(g.ICMS20.VICMS)
		return
	}
	if g.ICMS51 != nil {
		bc = parseFloat(g.ICMS51.VBC)
		vicms = parseFloat(g.ICMS51.VICMS)
		return
	}

	if g.ICMS10 != nil {
		bc = parseFloat(g.ICMS10.VBC)
		vicms = parseFloat(g.ICMS10.VICMS)
		bcst = parseFloat(g.ICMS10.VBCST)
		vicmsst = parseFloat(g.ICMS10.VICMSST)
		return
	}
	if g.ICMS30 != nil {
		bcst = parseFloat(g.ICMS30.VBCST)
		vicmsst = parseFloat(g.ICMS30.VICMSST)
		return
	}
	if g.ICMS70 != nil {
		bc = parseFloat(g.ICMS70.VBC)
		vicms = parseFloat(g.ICMS70.VICMS)
		bcst = parseFloat(g.ICMS70.VBCST)
		vicmsst = parseFloat(g.ICMS70.VICMSST)
		return
	}
	if g.ICMS90 != nil {
		bc = parseFloat(g.ICMS90.VBC)
		vicms = parseFloat(g.ICMS90.VICMS)
		bcst = parseFloat(g.ICMS90.VBCST)
		vicmsst = parseFloat(g.ICMS90.VICMSST)
		return
	}
	if g.ICMSPart != nil {
		bc = parseFloat(g.ICMSPart.VBC)
		vicms = parseFloat(g.ICMSPart.VICMS)
		bcst = parseFloat(g.ICMSPart.VBCST)
		vicmsst = parseFloat(g.ICMSPart.VICMSST)
		return
	}
	if g.ICMSSN201 != nil {
		bcst = parseFloat(g.ICMSSN201.VBCST)
		vicmsst = parseFloat(g.ICMSSN201.VICMSST)
		return
	}
	if g.ICMSSN202 != nil {
		bcst = parseFloat(g.ICMSSN202.VBCST)
		vicmsst = parseFloat(g.ICMSSN202.VICMSST)
		return
	}
	if g.ICMSSN500 != nil {
		bcst = parseFloat(g.ICMSSN500.VBCST)
		vicmsst = parseFloat(g.ICMSSN500.VICMSST)
		return
	}
	if g.ICMSSN900 != nil {
		bc = parseFloat(g.ICMSSN900.VBC)
		vicms = parseFloat(g.ICMSSN900.VICMS)
		bcst = parseFloat(g.ICMSSN900.VBCST)
		vicmsst = parseFloat(g.ICMSSN900.VICMSST)
		return
	}

	// ICMS40, ICMSSN101/102 não têm base/valor – retornam zero mesmo
	return
}

func extractIPI(i *ipi) float64 {
	if i == nil {
		return 0
	}
	if i.Trib != nil {
		return parseFloat(i.Trib.VIPI)
	}
	return 0
}

func extractPIS(p *pis) float64 {
	if p == nil {
		return 0
	}
	if p.Aliq != nil && p.Aliq.VPIS != "" {
		return parseFloat(p.Aliq.VPIS)
	}
	if p.Qtde != nil && p.Qtde.VPIS != "" {
		return parseFloat(p.Qtde.VPIS)
	}
	if p.NT != nil && p.NT.VPIS != "" {
		return parseFloat(p.NT.VPIS)
	}
	if p.Outr != nil && p.Outr.VPIS != "" {
		return parseFloat(p.Outr.VPIS)
	}
	return 0
}

func extractCOFINS(c *cofins) float64 {
	if c == nil {
		return 0
	}
	if c.Aliq != nil && c.Aliq.VCOFINS != "" {
		return parseFloat(c.Aliq.VCOFINS)
	}
	if c.Qtde != nil && c.Qtde.VCOFINS != "" {
		return parseFloat(c.Qtde.VCOFINS)
	}
	if c.NT != nil && c.NT.VCOFINS != "" {
		return parseFloat(c.NT.VCOFINS)
	}
	if c.Outr != nil && c.Outr.VCOFINS != "" {
		return parseFloat(c.Outr.VCOFINS)
	}
	return 0
}

// ============================================================================
// Helpers XSD
// ============================================================================

func validateXMLWithXSD(xmlData []byte, xsdPath string) error {
	if _, err := os.Stat(xsdPath); err != nil {
		return fmt.Errorf("XSD não encontrado em %s: %w", xsdPath, err)
	}

	if err := xsdvalidate.Init(); err != nil {
		return fmt.Errorf("erro inicializando validador XSD: %w", err)
	}
	defer xsdvalidate.Cleanup()

	xsdHandler, err := xsdvalidate.NewXsdHandlerUrl(xsdPath, xsdvalidate.ParsErrDefault)
	if err != nil {
		return fmt.Errorf("erro carregando XSD %s: %w", xsdPath, err)
	}
	defer xsdHandler.Free()

	if err := xsdHandler.ValidateMem(xmlData, xsdvalidate.ValidErrDefault); err != nil {
		return fmt.Errorf("XML inválido segundo XSD (%s): %w", xsdPath, err)
	}

	return nil
}

func resolveXSDPath(baseDir, xsdFile string) (string, error) {
	if xsdFile == "" {
		return "", fmt.Errorf("NFE_XSD_MAIN não definido")
	}
	if filepath.IsAbs(xsdFile) {
		return xsdFile, nil
	}
	return filepath.Join(baseDir, xsdFile), nil
}

// ============================================================================
// Helpers genéricos (datas, números, chave, etc.)
// ============================================================================

func extractChave(id string) string {
	// id costuma ser algo como "NFe3514..." -> removemos "NFe"
	id = strings.TrimSpace(id)
	id = strings.TrimPrefix(id, "NFe")
	return id
}

func parseFloat(v string) float64 {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}
	v = strings.ReplaceAll(v, ",", ".")
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return 0
	}
	return f
}

func parseInt(v string) int {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}
	i, err := strconv.Atoi(v)
	if err != nil {
		return 0
	}
	return i
}

// normalizeDateYMD recebe algo como "2025-11-11T12:34:56-03:00" ou "2025-11-11"
// e devolve só "2025-11-11".
func normalizeDateYMD(d string) string {
	d = strings.TrimSpace(d)
	if d == "" {
		return ""
	}

	// Se já vier só data (YYYY-MM-DD), devolve direto
	if len(d) == 10 && d[4] == '-' && d[7] == '-' {
		return d
	}

	// Tenta alguns layouts comuns
	layouts := []string{
		time.RFC3339,
		"2006-01-02T15:04:05-07:00",
		"2006-01-02T15:04:05",
	}

	for _, layout := range layouts {
		if t, err := time.Parse(layout, d); err == nil {
			return t.Format("2006-01-02")
		}
	}

	// Se nada funcionar, devolve só os 10 primeiros se tiver cara de data
	if len(d) >= 10 {
		return d[:10]
	}
	return d
}

// normalizeYMDDate é pra campos já no formato YYYY-MM-DD (dVenc).
func normalizeYMDDate(d string) string {
	d = strings.TrimSpace(d)
	if d == "" {
		return ""
	}
	if len(d) >= 10 {
		return d[:10]
	}
	return d
}

func onlyDigits(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}
