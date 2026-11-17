const fs = require('fs');
let html = fs.readFileSync(__dirname + '/app.html', 'utf8');
const parser = fs.readFileSync(__dirname + '/parser.js', 'utf8').replace(/if \(typeof module[^\n]*\n?/, '');
const qr = fs.readFileSync(__dirname + '/qr.js', 'utf8');
html = html.replace('/*__QR__*/', () => qr).replace('/*__PARSER__*/', () => parser);
fs.writeFileSync(__dirname + '/index.html', html);

// Versão instalada (Windows): pdf.js servido pelo próprio programa e dados pela API local
const cdn = /<script src="https:\/\/cdnjs[^"]+pdf\.worker\.min\.js"><\/script>\s*<script src="https:\/\/cdnjs[^"]+pdf\.min\.js"><\/script>/;
if (!cdn.test(html)) throw new Error('tags do pdf.js não encontradas');
const local = html.replace(cdn, () => `<script>window.LOCAL_API = true;</script>
<script type="module">import * as p from '/pdf.mjs'; p.GlobalWorkerOptions.workerSrc = '/pdf.worker.mjs'; window.pdfjsLib = p;</script>`);
const web = '/home/claude/desk/app/web';
fs.mkdirSync(web, { recursive: true });
fs.writeFileSync(web + '/index.html', local);
for (const f of ['pdf.mjs', 'pdf.worker.mjs']) fs.copyFileSync('/opt/npm-tools/node_modules/pdfjs-dist/legacy/build/' + f, web + '/' + f);
console.log('ok', html.length, local.length);
// verificação de sintaxe dos scripts embutidos
for (const [nome, h] of [['nuvem', html], ['local', local]]) {
  const scripts = [...h.matchAll(/<script(?![^>]*src)(?![^>]*type="module")[^>]*>([\s\S]*?)<\/script>/g)].map(m => m[1]);
  scripts.forEach((sc, i) => { try { new Function(sc); } catch (e) { throw new Error(`erro de sintaxe (${nome}, script ${i}): ${e.message}`); } });
}
console.log('sintaxe ok');
