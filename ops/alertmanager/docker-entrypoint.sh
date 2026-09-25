#!/bin/sh
# Alertmanager 容器入口：生成最终配置后 exec alertmanager。
#
# 1. 展开 /etc/alertmanager/alertmanager.yml 中的 ${VAR} / ${VAR:-default}
#    占位符（Alertmanager 本身不支持配置内 env 展开）；
# 2. 剪掉 "# AM_GATE_BEGIN <ENV>" .. "# AM_GATE_END" 门控块：仅当对应
#    环境变量非空时保留（避免空 bot_token/to 使配置校验失败）；
# 3. exec /bin/alertmanager，数据（silences/nflog）落在 /alertmanager 卷。
#
# 依赖镜像内 busybox awk（prom/alertmanager 官方镜像自带）。
set -eu

SRC=/etc/alertmanager/alertmanager.yml
DST=/alertmanager/alertmanager.generated.yml

if ! command -v awk >/dev/null 2>&1; then
	echo "alertmanager-entrypoint: awk 不可用，无法生成配置" >&2
	exit 1
fi

awk '
BEGIN { keep = 1 }
/^#[[:space:]]*AM_GATE_BEGIN[[:space:]]/ {
	gate = $0
	sub(/^.*AM_GATE_BEGIN[[:space:]]+/, "", gate)
	sub(/[[:space:]].*$/, "", gate)
	keep = (ENVIRON[gate] != "") ? 1 : 0
	next
}
/^#[[:space:]]*AM_GATE_END/ { keep = 1; next }
{
	if (!keep) next
	line = $0
	out = ""
	while ((i = index(line, "${")) > 0) {
		out = out substr(line, 1, i - 1)
		line = substr(line, i + 2)
		j = index(line, "}")
		if (j == 0) { out = out "${"; break }
		name = substr(line, 1, j - 1)
		line = substr(line, j + 1)
		def = ""
		if ((k = index(name, ":-")) > 0) {
			def = substr(name, k + 2)
			name = substr(name, 1, k - 1)
		}
		val = ENVIRON[name]
		if (val == "") val = def
		out = out val
	}
	print out line
}
' "$SRC" > "$DST"

echo "alertmanager-entrypoint: 配置已生成 $DST" >&2
exec /bin/alertmanager --config.file="$DST" --storage.path=/alertmanager "$@"
