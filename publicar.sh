#!/bin/bash
# Publica uma nova versão: ./publicar.sh 1.1.0 "o que mudou"
set -euo pipefail
V="$1"; NOTAS="${2:-}"
REPO=carlosneto1994/faturamento-solar
OUT=/home/claude/saida/$V; mkdir -p "$OUT"
node /home/claude/fat/build.js
cd /home/claude/desk/app
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "-H windowsgui -s -w -X main.versaoAtual=$V" -o "$OUT/FaturamentoSolar.exe" .
cp "$OUT/FaturamentoSolar.exe" ../inst/FaturamentoSolar.exe
cd ../inst
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "-H windowsgui -s -w -X main.versao=$V" -o "$OUT/Instalar-Faturamento-Solar.exe" .
SHA=$(sha256sum "$OUT/FaturamentoSolar.exe" | cut -d' ' -f1)
cd /home/claude/repo
cat > versao.json <<JSON
{
  "versao": "$V",
  "notas": "$NOTAS",
  "arquivos": {
    "windows-amd64": {
      "url": "https://github.com/$REPO/releases/download/v$V/FaturamentoSolar.exe",
      "sha256": "$SHA"
    }
  }
}
JSON
echo "versao.json pronto: $V $SHA"
if [ "${SO_LOCAL:-}" = "1" ]; then exit 0; fi
gh release create "v$V" "$OUT/FaturamentoSolar.exe" "$OUT/Instalar-Faturamento-Solar.exe" --repo "$REPO" --title "Versão $V" --notes "${NOTAS:-Atualização}"
# copia o código-fonte atual
rm -rf src/programa src/instalador src/pagina; mkdir -p src
cp -r /home/claude/desk/app src/programa && rm -rf src/programa/web
cp -r /home/claude/desk/inst src/instalador && rm -f src/instalador/*.exe
mkdir -p src/pagina && cp /home/claude/fat/{app.html,parser.js,qr.js,build.js,bundleqr.js} src/pagina/
git add -A && git commit -q -m "Versão $V" && git push -q origin HEAD:main
echo "publicado v$V"
