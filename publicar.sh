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
cd /home/claude/faturamento-solar
cat > versao.json <<JSON
{
  "versao": "$V",
  "notas": "$NOTAS",
  "arquivos": {
    "windows-amd64": {
      "url": "https://raw.githubusercontent.com/$REPO/main/versoes/$V/FaturamentoSolar.exe",
      "sha256": "$SHA"
    }
  }
}
JSON
echo "versao.json pronto: $V $SHA"
if [ "${SO_LOCAL:-}" = "1" ]; then exit 0; fi
cd /home/claude/faturamento-solar
mkdir -p "versoes/$V" && cp "$OUT/FaturamentoSolar.exe" "versoes/$V/" && cp "$OUT/Instalar-Faturamento-Solar.exe" ./Instalar-Faturamento-Solar.exe
# copia o código-fonte atual
rm -rf src/programa src/instalador src/pagina; mkdir -p src
cp -r /home/claude/desk/app src/programa && rm -rf src/programa/web
cp -r /home/claude/desk/inst src/instalador && rm -f src/instalador/*.exe
mkdir -p src/pagina && cp /home/claude/fat/{app.html,parser.js,qr.js,build.js,bundleqr.js} src/pagina/
git add -A && git commit -q -m "Versão $V" -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>" -m "Claude-Session: https://claude.ai/code/session_017axorKr1MJ8iYbDtGpjjJD" && git -c push.negotiate=true push -q origin HEAD:main
echo "publicado v$V"
