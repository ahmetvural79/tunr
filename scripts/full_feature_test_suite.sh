#!/usr/bin/env bash
set -uo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR" || exit 1

REPORT_DIR="$ROOT_DIR/artifacts/test-reports"
mkdir -p "$REPORT_DIR"
GO_CACHE_DIR="$(mktemp -d "${TMPDIR:-/tmp}/tunr-go-cache.XXXXXX")"
export GOMODCACHE="$GO_CACHE_DIR/gomod"
export GOCACHE="$GO_CACHE_DIR/gobuild"
mkdir -p "$GOMODCACHE" "$GOCACHE"

REPORT_FILE="$REPORT_DIR/tunr-feature-suite-$(date +%Y%m%d-%H%M%S).md"
BIN_DIR="$(mktemp -d "${TMPDIR:-/tmp}/tunr-feature-suite.XXXXXX")"
BIN_PATH="$BIN_DIR/tunr"
TMP_LOG="$(mktemp "${TMPDIR:-/tmp}/tunr-test-log.XXXXXX")"

cleanup() {
	rm -rf "$BIN_DIR" >/dev/null 2>&1 || true
	rm -rf "$GO_CACHE_DIR" >/dev/null 2>&1 || true
	rm -f "$TMP_LOG" >/dev/null 2>&1 || true
}
trap cleanup EXIT

TOTAL_CHECKS=0
FAILED_CHECKS=0

write_header() {
	{
		echo "# tunr Full Feature Test Report"
		echo
		echo "- Date: $(date -u +"%Y-%m-%d %H:%M:%S UTC")"
		echo "- Host: $(uname -srm)"
		echo "- Repo: $ROOT_DIR"
		echo
		echo "## Coverage"
		echo
		echo "- Build validation for CLI binary"
		echo "- Full Go test suite across repository"
		echo "- Vibecoder feature tests in \`internal/proxy\`"
		echo "- CLI command smoke checks"
		echo "- Vibecoder flags/help surface checks"
		echo "- Turkish-content regression check in docs and command surfaces"
		echo
		echo "## Results"
		echo
	} > "$REPORT_FILE"
}

cleanup_legacy_workspace_cache() {
	local legacy_cache="$ROOT_DIR/.cache"
	if [[ ! -d "$legacy_cache" ]]; then
		echo "Legacy cache cleanup: no .cache directory found."
		return 0
	fi

	chmod -R u+w "$legacy_cache" 2>/dev/null || true
	chflags -R nouchg "$legacy_cache" 2>/dev/null || true

	if rm -rf "$legacy_cache" 2>/dev/null; then
		echo "Legacy cache cleanup: removed .cache directory."
		return 0
	fi

	echo "Legacy cache cleanup: could not remove .cache (permission/ownership issue)."
	echo "Run manually with elevated privileges if needed:"
	echo "  sudo chflags -R nouchg \"$legacy_cache\" && sudo chmod -R u+rwX \"$legacy_cache\" && sudo rm -rf \"$legacy_cache\""
	return 0
}

run_check() {
	local check_name="$1"
	shift
	TOTAL_CHECKS=$((TOTAL_CHECKS + 1))

	if "$@" >"$TMP_LOG" 2>&1; then
		{
			echo "### PASS: $check_name"
			echo
		} >> "$REPORT_FILE"
		printf 'PASS  %s\n' "$check_name"
		return 0
	fi

	FAILED_CHECKS=$((FAILED_CHECKS + 1))
	{
		echo "### FAIL: $check_name"
		echo
		echo '```text'
		cat "$TMP_LOG"
		echo '```'
		echo
	} >> "$REPORT_FILE"
	printf 'FAIL  %s\n' "$check_name"
	return 1
}

write_header
cleanup_legacy_workspace_cache >> "$REPORT_FILE" 2>&1

run_check "Build tunr CLI" \
	go build -trimpath -ldflags="-s -w" -o "$BIN_PATH" ./cmd/tunr

run_check "Run all Go tests" \
	go test ./...

run_check "Run vibecoder/proxy tests with verbose output" \
	go test -v ./internal/proxy

run_check "CLI smoke: help for all major commands" \
	bash -lc "\"$BIN_PATH\" share --help >/dev/null && \
	\"$BIN_PATH\" start --help >/dev/null && \
	\"$BIN_PATH\" stop --help >/dev/null && \
	\"$BIN_PATH\" status --help >/dev/null && \
	\"$BIN_PATH\" logs --help >/dev/null && \
	\"$BIN_PATH\" replay --help >/dev/null && \
	\"$BIN_PATH\" open --help >/dev/null && \
	\"$BIN_PATH\" login --help >/dev/null && \
	\"$BIN_PATH\" logout --help >/dev/null && \
	\"$BIN_PATH\" doctor --help >/dev/null && \
	\"$BIN_PATH\" config --help >/dev/null && \
	\"$BIN_PATH\" update --help >/dev/null && \
	\"$BIN_PATH\" uninstall --help >/dev/null && \
	\"$BIN_PATH\" mcp --help >/dev/null"

run_check "Vibecoder flags exposed in share help" \
	bash -lc "out=\$(\"$BIN_PATH\" share --help) && \
	[[ \"\$out\" == *\"--demo\"* ]] && \
	[[ \"\$out\" == *\"--freeze\"* ]] && \
	[[ \"\$out\" == *\"--inject-widget\"* ]] && \
	[[ \"\$out\" == *\"--auto-login\"* ]] && \
	[[ \"\$out\" == *\"--route\"* ]] && \
	[[ \"\$out\" == *\"--password\"* ]] && \
	[[ \"\$out\" == *\"--ttl\"* ]]"

run_check "No Turkish characters in docs and command surfaces" \
	python3 -c "import pathlib, re, sys; pats=[pathlib.Path('docs'), pathlib.Path('cmd/tunr'), pathlib.Path('vscode-tunr')]; exts={'.md','.go','.js','.json'}; rx=re.compile(r'[çğıöşüÇĞİÖŞÜ]'); \
bad=[]; \
[bad.append(str(p)) for root in pats for p in root.rglob('*') if p.is_file() and p.suffix in exts and rx.search(p.read_text(encoding='utf-8', errors='ignore'))]; \
sys.exit(1 if bad else 0)"

{
	echo "## Summary"
	echo
	echo "- Total checks: $TOTAL_CHECKS"
	echo "- Passed: $((TOTAL_CHECKS - FAILED_CHECKS))"
	echo "- Failed: $FAILED_CHECKS"
	echo
	if [[ "$FAILED_CHECKS" -eq 0 ]]; then
		echo "Feature suite status: PASS"
	else
		echo "Feature suite status: FAIL"
	fi
	echo
} >> "$REPORT_FILE"

echo
echo "Report generated: $REPORT_FILE"
if [[ "$FAILED_CHECKS" -eq 0 ]]; then
	exit 0
fi
exit 1
