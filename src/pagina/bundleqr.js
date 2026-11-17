const fs = require('fs');
const d = '/opt/node22/lib/node_modules/npm/node_modules/qrcode-terminal/vendor/QRCode/';
let out = "/* QR Code encoder - Kazuhiko Arase (MIT), via qrcode-terminal (Apache-2.0) */\n" +
  "var QRLib=(function(){var M={};function req(n){n=n.split('/').pop().replace(/\\.js$/,'');var m=M[n];if(!m.l){m.l=1;m.e={exports:{}};m.f(m.e,req);m.x=m.e.exports}return m.x}\n";
for (const f of fs.readdirSync(d)) {
  out += 'M[' + JSON.stringify(f.replace('.js', '')) + ']={f:function(module,require){var exports=module.exports;\n' + fs.readFileSync(d + f, 'utf8') + '\n}};\n';
}
out += "return {QRCode:req('index'),ECL:req('QRErrorCorrectLevel')}})();\n";
fs.writeFileSync('qr.js', out);
eval(out + ';var q=new QRLib.QRCode(-1,QRLib.ECL.M);q.addData("hello world pix");q.make();console.log("modules",q.getModuleCount(),q.isDark(0,0))');
