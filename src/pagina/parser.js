// Leitor da conta Energisa Paraíba (DANF3E). Recebe itens de texto do pdf.js por página.
function construirLinhas(paginas) {
  const linhas = [];
  paginas.forEach((itens, pi) => {
    const ord = itens.slice().sort((a, b) => b.y - a.y || a.x - b.x);
    const grupos = [];
    for (const it of ord) {
      let g = grupos.find(g => Math.abs(g.y - it.y) <= 2.5);
      if (!g) { g = { y: it.y, itens: [] }; grupos.push(g); }
      g.itens.push(it);
    }
    grupos.sort((a, b) => b.y - a.y);
    for (const g of grupos) {
      g.itens.sort((a, b) => a.x - b.x);
      let txt = '', fim = null;
      for (const it of g.itens) {
        if (fim !== null) txt += (it.x - fim > 6) ? '   ' : (it.x - fim > 0.8 ? ' ' : '');
        txt += it.s;
        fim = it.x + (it.w || 0);
      }
      linhas.push({ pagina: pi + 1, y: g.y, texto: txt.replace(/\s+$/, '') });
    }
  });
  return linhas;
}

const num = s => {
  if (s == null) return null;
  s = String(s).trim();
  const neg = /^-/.test(s) || /-$/.test(s);
  s = s.replace(/[^\d,]/g, '').replace(',', '.');
  const v = parseFloat(s);
  return isNaN(v) ? null : (neg ? -v : v);
};
const MESES = ['janeiro','fevereiro','março','abril','maio','junho','julho','agosto','setembro','outubro','novembro','dezembro'];
const MESES_AB = ['JAN','FEV','MAR','ABR','MAI','JUN','JUL','AGO','SET','OUT','NOV','DEZ'];
const soD = s => String(s || '').replace(/\D/g, '');
const NUM_RE = /-?\d{1,3}(?:\.\d{3})*,\d+-?|-?\d+,\d+-?/g;

function lerContaEnergisa(paginas) {
  const linhas = construirLinhas(paginas);
  const T = linhas.map(l => l.texto);
  const tudo = T.join('\n');
  const r = { avisos: [], itens: [], historico: [] };

  // Distribuidora
  r.distribuidora = /ENERGISA PARA[IÍ]BA/i.test(tudo) ? 'Energisa Paraíba' : (/ENERGISA/i.test(tudo) ? 'Energisa' : '');
  if (!r.distribuidora) r.avisos.push('Não parece ser uma conta da Energisa. Confira os dados.');

  // UC
  let m;

  // Referência, vencimento e total
  m = tudo.match(new RegExp('(' + MESES.join('|') + ')\\s*\\/\\s*(\\d{4})', 'i'));
  if (m) { r.mesRef = MESES.indexOf(m[1].toLowerCase()) + 1; r.anoRef = +m[2]; }
  // O mês de referência certo é o da linha "Mês / Ano  vencimento  R$ total" (o texto da conta pode citar outros meses)
  const reMes = new RegExp('(' + MESES.join('|') + ')\\s*\\/\\s*(\\d{4})', 'i');
  const linhaRef = T.find(t => reMes.test(t) && /\d{2}\/\d{2}\/\d{4}/.test(t) && /R\$/.test(t)) || T.find(t => reMes.test(t) && /R\$/.test(t));
  if (linhaRef) { const mm = linhaRef.match(reMes); r.mesRef = MESES.indexOf(mm[1].toLowerCase()) + 1; r.anoRef = +mm[2]; }
  if (linhaRef) {
    const d = linhaRef.match(/(\d{2}\/\d{2}\/\d{4})/); r.vencimento = d ? d[1] : '';
    const v = linhaRef.match(/R\$\s*([\d.]+,\d{2})/); r.totalConta = v ? num(v[1]) : null;
  }
  if (r.totalConta == null) { m = tudo.match(/TOTAL:\s*([\d.]+,\d{2})/); if (m) r.totalConta = num(m[1]); }

  // Leituras (anterior, atual, dias, próxima)
  m = tudo.match(/(\d{2}\/\d{2}\/\d{4})\s+(\d{2}\/\d{2}\/\d{4})\s+(\d{1,3})\s+(\d{2}\/\d{2}\/\d{4})/);
  if (m) { r.leituraAnterior = m[1]; r.leituraAtual = m[2]; r.dias = +m[3]; r.proximaLeitura = m[4]; }
  else {
    m = tudo.match(/Leitura Anterior:\s*(\d{2}\/\d{2}\/\d{4})\s+Leitura Atual:\s*(\d{2}\/\d{2}\/\d{4})\s+Dias:\s*(\d+)/);
    if (m) { r.leituraAnterior = m[1]; r.leituraAtual = m[2]; r.dias = +m[3]; }
  }

  // Nota fiscal / emissão
  m = tudo.match(/NOTA FISCAL N[ºo°]:\s*([\d.]+)/i); r.notaFiscal = m ? m[1] : '';
  m = tudo.match(/DATA DE EMISS[ÃA]O:\s*(\d{2}\/\d{2}\/\d{4})/i); r.emissao = m ? m[1] : '';

  // Cliente: bloco do PAGADOR traz nome e CPF/CNPJ completos
  const iPag = T.findIndex(t => /^\s*PAGADOR\b/.test(t));
  if (iPag >= 0) {
    const l1 = T[iPag + 1] || '';
    const doc = l1.match(/(\d{3}\.\d{3}\.\d{3}-\d{2}|\d{2}\.\d{3}\.\d{3}\/\d{4}-\d{2})/);
    r.documento = doc ? doc[1] : '';
    r.nome = l1.replace(doc ? doc[1] : '', '').trim();
    const l2 = T[iPag + 2] || '';
    if (!/SACADOR/i.test(l2)) r.endereco = l2.trim().replace(/\s{2,}/g, ' - ');
  }
  if (!r.documento) { m = tudo.match(/CNPJ\/CPF\/RANI:\s*([\dX.\/-]+)/i); r.documento = m ? m[1] : ''; }
  // UC: número no bloco superior da página 1 (formato 9.999.999.999-99 ou 999.999.999-99), diferente do CPF do pagador
  r.uc = '';
  const iItens = T.findIndex(t => /Itens da Fatura/i.test(t));
  const topo = T.slice(0, iItens > 0 ? iItens : 40);
  for (const t of topo) {
    if (/CNPJ|Insc|chave|Protocolo/i.test(t)) continue;
    const cands = t.match(/\b\d{1,3}(?:\.\d{3}){2,3}-\d{2}\b/g) || [];
    const c = cands.find(x => soD(x) !== soD(r.documento));
    if (c) { r.uc = c; break; }
  }
  m = tudo.match(/Classifica[çc][ãa]o:\s*(.+)/i); r.classificacao = m ? m[1].replace(/\s{2,}.*/, '').trim() : '';
  m = tudo.match(/LIGA[ÇC][ÃA]O:\s*(\w+)/i); r.ligacao = m ? m[1] : '';
  m = tudo.match(/classificada como (GD_?\w+)/i); r.modalidadeGD = m ? m[1] : '';

  // Itens da fatura
  const iIni = T.findIndex(t => /Itens da Fatura/i.test(t));
  const iFim = T.findIndex((t, i) => i > iIni && /^\s*TOTAL:/i.test(t.trim()));
  if (iIni >= 0) {
    for (let i = iIni + 1; i < (iFim > 0 ? iFim : iIni + 40); i++) {
      let t = T[i].trim();
      if (!t || /^(PIS|COFINS|ICMS|Quant\.?|Consumo kWh|N[ºo] DIAS|FAT|CONSUMO FATURADO)\b/i.test(t)) continue;
      if (/^(JAN|FEV|MAR|ABR|MAI|JUN|JUL|AGO|SET|OUT|NOV|DEZ)\/\d{2}/.test(t)) continue;
      // junta números quebrados pelo PDF ("1.312 ,00" -> "1.312,00")
      t = t.replace(/(\d)\s+([,.])\s*(\d)/g, '$1$2$3').replace(/(\d)\s+-(?=\s|$)/g, '$1-');
      let it;
      const mk = t.match(/^(.+?)\s+(KWH|kWh)\s+(-?[\d.]+,\d+-?)\s+(-?[\d.]+,\d+-?)\s+(-?[\d.]+,\d+-?)/);
      if (mk) it = { descricao: mk[1].trim(), unidade: 'KWH', quantidade: Math.abs(num(mk[3])), precoUnit: Math.abs(num(mk[4])), valor: num(mk[5]) };
      else {
        const mn = NUM_RE.exec(t); NUM_RE.lastIndex = 0;
        if (!mn || mn.index === 0) continue;
        const desc = t.slice(0, mn.index).replace(/\s+(KWH|kWh)\s*$/, '').trim();
        if (!/[A-Za-zÀ-ú]/.test(desc)) continue;
        it = { descricao: desc, unidade: '', valor: num(mn[0]) };
        // Linhas de energia (injetada, ajuste GD, consumo) em outro formato: acha kWh, tarifa e valor pelos próprios números
        if (/injetad|compensad|ajuste\s+gd|trf\.?\s*reduzida|^consumo/i.test(desc)) {
          const resto = t.slice(mn.index);
          const ns = (resto.match(NUM_RE) || []).map(x => ({ v: num(x), dec: (x.split(',')[1] || '').replace('-', '').length }));
          const iT = ns.findIndex((n, i) => i > 0 && Math.abs(n.v) < 10 && n.dec >= 4); // tarifa: ex. 0,850720
          if (iT > 0) {
            const q = Math.abs(ns[iT - 1].v), tar = Math.abs(ns[iT].v), alvo = q * tar;
            // valor: o número depois da tarifa mais próximo de kWh × tarifa
            const cands = ns.slice(iT + 1, iT + 4).map(n => n.v);
            let val = cands.length ? cands.reduce((m, v) => Math.abs(Math.abs(v) - alvo) < Math.abs(Math.abs(m) - alvo) ? v : m, cands[0]) : null;
            if (val == null || Math.abs(Math.abs(val) - alvo) > Math.max(1, alvo * 0.02)) val = -Math.round(alvo * 100) / 100;
            if (/injetad|compensad/i.test(desc) && val > 0) val = -val;
            it = { descricao: desc, unidade: 'KWH', quantidade: q, precoUnit: tar, valor: val };
          } else if (/injetad|compensad/i.test(desc) && ns.length) {
            // sem tarifa identificável: não arrisca usar kWh como R$; o aviso de conferência vai aparecer
            const neg = ns.find(n => n.v < 0);
            it = { descricao: desc, unidade: 'KWH', quantidade: Math.abs(ns[0].v), precoUnit: 0, valor: neg ? neg.v : 0 };
          }
        }
      }
      const desc = it.descricao;
      if (/ajuste\s+gd|trf\.?\s*reduzida|fio\s*b/i.test(desc)) it.tipo = 'ajuste_gd';
      else if (/injetad|compensad|energia\s+atv\s+inj/i.test(desc)) { it.tipo = 'credito'; const g = desc.match(/GD\s*(III|II|I)\b/i); it.gd = g ? 'GD ' + g[1].toUpperCase() : 'GD'; }
      else if (/^consumo/i.test(desc)) it.tipo = 'consumo';
      else if (/ilum|cip|cosip/i.test(desc)) it.tipo = 'iluminacao';
      else if (/multa|juros|atualiza/i.test(desc)) it.tipo = 'encargo_atraso';
      else it.tipo = 'outro';
      r.itens.push(it);
    }
  }
  const cons = r.itens.filter(i => i.tipo === 'consumo');
  const cred = r.itens.filter(i => i.tipo === 'credito');
  r.consumoKwh = cons.reduce((s, i) => s + (i.quantidade || 0), 0);
  r.tarifaConsumo = cons[0] ? cons[0].precoUnit : null;
  r.compensadoKwh = cred.reduce((s, i) => s + (i.quantidade || 0), 0);
  r.valorCreditos = Math.abs(cred.reduce((s, i) => s + (i.valor || 0), 0));
  r.tarifaCompensada = cred[0] ? cred[0].precoUnit : null;
  // Cada linha de energia injetada com a sua tarifa (a tarifa muda de mês para mês)
  r.injecoes = cred.map(c => { const o = c.descricao.match(/(\d{1,2}\/\d{4})/); return { gd: c.gd === 'GD II' ? 'GD II' : 'GD I', origem: o ? o[1] : '', kwh: c.quantidade || 0, tarifa: c.precoUnit || 0, valor: Math.abs(c.valor || 0) }; });
  r.ajusteGD = Math.round(r.itens.filter(i => i.tipo === 'ajuste_gd').reduce((s, i) => s + (i.valor || 0), 0) * 100) / 100;
  const grupos = {};
  for (const c of cred) { const g = grupos[c.gd] || (grupos[c.gd] = { gd: c.gd, kwh: 0, valor: 0 }); g.kwh += c.quantidade || 0; g.valor += Math.abs(c.valor || 0); }
  r.creditosPorTipo = Object.values(grupos).map(g => ({ ...g, valor: Math.round(g.valor * 100) / 100 })).sort((a, b) => a.gd.localeCompare(b.gd));

  // Saldo de créditos
  m = tudo.match(/Saldo Acumulado:\s*([\d.,]+)/i); r.saldoKwh = m ? num(m[1].includes(',') ? m[1] : m[1].replace(/\./g, '') + ',0') : null;
  m = tudo.match(/A expirar no pr[óo]ximo ciclo:\s*([\d.,]+)/i); r.expirarKwh = m ? num(m[1].includes(',') ? m[1] : m[1].replace(/\./g, '') + ',0') : null;

  // Histórico de consumo (tabela com kWh)
  const hist = {};
  for (const t of T) {
    const re = /\b(JAN|FEV|MAR|ABR|MAI|JUN|JUL|AGO|SET|OUT|NOV|DEZ)\/(\d{2})\s+(\d{1,3}(?:\.\d{3})*,\d{2})/g;
    let h; while ((h = re.exec(t))) hist[h[1] + '/' + h[2]] = num(h[3]);
  }
  r.historico = Object.entries(hist).map(([k, v]) => ({ mes: k, kwh: v }))
    .sort((a, b) => { const [ma, ya] = a.mes.split('/'), [mb, yb] = b.mes.split('/'); return (ya - yb) || (MESES_AB.indexOf(ma) - MESES_AB.indexOf(mb)); });

  // Conferência
  const soma = r.itens.reduce((s, i) => s + (i.valor || 0), 0);
  r.somaItens = Math.round(soma * 100) / 100;
  // Conferência só da energia injetada (é sobre ela que o desconto é aplicado): kWh × tarifa deve dar o valor da linha
  (r.injecoes || []).forEach(l => {
    const calc = (l.kwh || 0) * (l.tarifa || 0);
    if (Math.abs(calc - l.valor) > Math.max(0.05, l.valor * 0.001)) r.avisos.push(`Confira a linha de energia injetada ${l.gd}${l.origem ? ' ' + l.origem : ''}: ${l.kwh} kWh × ${String(l.tarifa).replace('.', ',')} não dá R$ ${l.valor.toFixed(2).replace('.', ',')}.`);
  });
  if (!cred.length) r.avisos.push('Não encontrei energia injetada nesta conta. Confira se é a conta certa ou preencha as linhas de energia injetada à mão.');
  if (!r.uc) r.avisos.push('Não encontrei o número da UC.');
  // Trechos da conta para diagnóstico (sem nome nem CPF): linha da referência, itens da fatura e quadro de créditos
  const iI = T.findIndex(t => /Itens da Fatura/i.test(t)), iT = T.findIndex((t, i) => i > iI && /^\s*TOTAL:/i.test(t.trim()));
  const sel = [];
  if (linhaRef) sel.push(linhaRef);
  if (iI >= 0) sel.push(...T.slice(iI, iT > iI ? iT + 1 : iI + 30));
  sel.push(...T.filter(t => /Saldo Acumulado|classificada como|NOTA FISCAL N/i.test(t)));
  r.diag = sel.map(t => t.replace(/\b\d{3}\.\d{3}\.\d{3}-\d{2}\b/g, '***.***.***-**').replace(/\b\d{2}\.\d{3}\.\d{3}\/\d{4}-\d{2}\b/g, '**.***.***/****-**').replace(/\s{3,}/g, '  |  ').slice(0, 220));
  return r;
}
if (typeof module !== 'undefined') module.exports = { lerContaEnergisa, construirLinhas };
