#!/bin/sh
# ImgYoung 一行安装脚本(Linux / macOS):下载发布版、校验 sha256、装入 PATH;重复执行即升级。
#
#   curl -fsSL https://raw.githubusercontent.com/x-haose/imgyoung/main/install.sh | sh
#
# 选项(经 sh -s -- 传入,如 ... | sh -s -- --service):
#   --version X.Y.Z   安装指定版本,前缀 v 可带可不带;默认最新正式版
#   --dir DIR         二进制安装目录;默认 root 为 /usr/local/bin,其他用户为 ~/.local/bin
#   --service         仅 Linux,需 root:同时安装并启动 systemd 服务 imgyoung(数据在 /var/lib/imgyoung)。
#                     此时 --dir 须为绝对路径且不在 /home、/root 下;服务文件每次按发布包重写,
#                     本机定制用 systemctl edit imgyoung 写成 drop-in
# 环境变量:
#   IMGYOUNG_RELEASES 发布页地址,默认 https://github.com/x-haose/imgyoung/releases;
#                     改用镜像站或内网分发时设置它,目录结构须与 GitHub Releases 一致
set -eu

RELEASES="${IMGYOUNG_RELEASES:-https://github.com/x-haose/imgyoung/releases}"
UNIT_PATH=/etc/systemd/system/imgyoung.service

version=""
dir=""
service=0
tmp=""

# fail 打印错误并以非零状态退出。
fail() {
	printf '错误:%s\n' "$*" >&2
	exit 1
}

# info 打印进度信息。
info() {
	printf '%s\n' "$*"
}

# need 确认命令存在。
need() {
	command -v "$1" >/dev/null 2>&1 || fail "需要 $1,请先安装"
}

# valid_version 判断版本标签是否形如 v1.2.3 或 v1.2.3-rc.1。
valid_version() {
	printf '%s\n' "$1" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$'
}

# parse_args 解析命令行选项。
parse_args() {
	while [ $# -gt 0 ]; do
		case "$1" in
		--version)
			[ $# -ge 2 ] || fail "--version 需要一个版本号,如 1.0.0"
			version="v${2#v}"
			valid_version "$version" || fail "版本号 $2 无效,应形如 1.0.0"
			shift 2
			;;
		--dir)
			[ $# -ge 2 ] || fail "--dir 需要一个目录"
			dir="$2"
			shift 2
			;;
		--service)
			service=1
			shift
			;;
		*)
			fail "未知选项 $1"
			;;
		esac
	done
}

# detect_platform 输出发布包命名所用的 <系统>_<架构>。
detect_platform() {
	case "$(uname -s)" in
	Linux) os=linux ;;
	Darwin) os=darwin ;;
	*) fail "不支持的系统 $(uname -s);Windows 请到 $RELEASES 下载 zip 包" ;;
	esac
	case "$(uname -m)" in
	x86_64 | amd64) arch=amd64 ;;
	aarch64 | arm64) arch=arm64 ;;
	*) fail "不支持的架构 $(uname -m);发布包只有 amd64 与 arm64" ;;
	esac
	printf '%s_%s' "$os" "$arch"
}

# resolve_version 未指定版本时,跟随 <发布页>/latest 的跳转取最新正式版标签。
resolve_version() {
	if [ -n "$version" ]; then
		return
	fi
	url=$(curl -fsSLI -o /dev/null -w '%{url_effective}' "$RELEASES/latest") ||
		fail "无法获取最新版本;可用 --version 指定"
	version="${url##*/}"
	valid_version "$version" || fail "无法从 $url 解析出版本号;可用 --version 指定"
}

# sha256_of 输出文件的 sha256 十六进制摘要。
sha256_of() {
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$1" | cut -d ' ' -f 1
	elif command -v shasum >/dev/null 2>&1; then
		shasum -a 256 "$1" | cut -d ' ' -f 1
	else
		fail "需要 sha256sum 或 shasum 来校验下载内容"
	fi
}

# download_and_verify 下载发布包与校验和清单,核对 sha256 后解包到临时目录。
download_and_verify() {
	archive="imgyoung_${version#v}_$1.tar.gz"
	base="$RELEASES/download/$version"
	info "下载 $archive($version)"
	curl -fsSL -o "$tmp/$archive" "$base/$archive" || fail "下载 $base/$archive 失败"
	curl -fsSL -o "$tmp/checksums.txt" "$base/checksums.txt" || fail "下载校验和清单失败"
	expected=$(awk -v f="$archive" '$2 == f { print $1 }' "$tmp/checksums.txt")
	[ -n "$expected" ] || fail "校验和清单里没有 $archive"
	actual=$(sha256_of "$tmp/$archive")
	[ "$expected" = "$actual" ] || fail "$archive 的 sha256 不符(期望 $expected,实际 $actual),已中止"
	tar -xzf "$tmp/$archive" -C "$tmp" || fail "解包 $archive 失败"
}

# resolve_dir 未指定 --dir 时确定默认安装目录:root 为 /usr/local/bin,其他用户为 ~/.local/bin。
resolve_dir() {
	[ -z "$dir" ] || return 0
	if [ "$(id -u)" -eq 0 ]; then
		dir=/usr/local/bin
	else
		[ -n "${HOME:-}" ] || fail "未设置 HOME,无法确定默认安装目录;用 --dir 指定"
		dir="$HOME/.local/bin"
	fi
}

# install_binary 把二进制装入目标目录。
install_binary() {
	mkdir -p "$dir" || fail "无法创建 $dir"
	[ -w "$dir" ] || fail "$dir 不可写;用 sudo 执行,或用 --dir 指定一个可写目录"
	install -m 0755 "$tmp/imgyoung" "$dir/imgyoung" || fail "无法写入 $dir/imgyoung"
	installed=$("$dir/imgyoung" -version) || fail "$dir/imgyoung 无法运行,检查系统与架构是否与发布包一致"
	info "已安装 $dir/imgyoung($installed)"
}

# check_service 下载前确认 --service 的前提,不满足时不动系统。
check_service() {
	[ "$(uname -s)" = Linux ] || fail "--service 只支持 Linux(systemd)"
	[ "$(id -u)" -eq 0 ] || fail "--service 需要 root 权限"
	need systemctl
	case "$dir" in
	/home | /home/* | /root | /root/* | /run/user | /run/user/*)
		fail "--service 的二进制不能装在 $dir:服务启用了 ProtectHome,进程看不到家目录"
		;;
	/*) ;;
	*) fail "--service 的 --dir 须为绝对路径,systemd 不接受相对路径" ;;
	esac
	case "$dir" in
	*[!A-Za-z0-9._/-]*) fail "--service 的 --dir 只能由字母、数字与 . _ - / 组成,其他字符会破坏服务文件的生成,或被 systemd 另行解析" ;;
	esac
}

# install_service 安装 systemd 服务并启动;已在运行时重启以换上新版本。
# 服务文件按发布包重写,与现有内容不同时提示,本机改动不会悄无声息地丢掉。
install_service() {
	unit="$tmp/imgyoung.service"
	sed "s|^ExecStart=.*|ExecStart=$dir/imgyoung|" "$tmp/deploy/imgyoung.service" >"$unit" ||
		fail "生成服务文件失败"
	rewritten=0
	if [ -f "$UNIT_PATH" ] && [ "$(cat "$unit")" != "$(cat "$UNIT_PATH")" ]; then
		rewritten=1
	fi
	install -m 0644 "$unit" "$UNIT_PATH" || fail "无法写入 $UNIT_PATH"
	if [ "$rewritten" -eq 1 ]; then
		info "已按发布包重写 $UNIT_PATH;本机定制请用 systemctl edit imgyoung 写成 drop-in,升级不会覆盖"
	fi
	systemctl daemon-reload || fail "systemctl daemon-reload 失败"
	systemctl enable --quiet imgyoung || fail "无法设置服务 imgyoung 开机启动"
	if systemctl is-active --quiet imgyoung; then
		systemctl restart imgyoung || fail "服务 imgyoung 重启失败,用 journalctl -u imgyoung 查看原因"
		info "服务 imgyoung 已重启为新版本"
	else
		systemctl start imgyoung || fail "服务 imgyoung 启动失败,用 journalctl -u imgyoung 查看原因"
		info "服务 imgyoung 已启动,数据目录 /var/lib/imgyoung"
	fi
}

# next_steps 提示安装后的下一步。
next_steps() {
	case ":$PATH:" in
	*":$dir:"*) ;;
	*) info "注意:$dir 不在 PATH 中,请把它加入 PATH,或用完整路径运行" ;;
	esac
	if [ "$service" -eq 0 ]; then
		info "运行 imgyoung 启动服务(数据写在当前目录的 data/,可用 IMGYOUNG_DATA_DIR 指定);"
		info "长期运行建议交给 systemd(加 --service)或 Docker 托管。"
	fi
	info "浏览器打开 http://<主机>:8721/,用 admin / imgyoung123 登录,首次登录会要求改密。"
}

main() {
	parse_args "$@"
	resolve_dir
	need curl
	need tar
	need install
	if [ "$service" -eq 1 ]; then
		check_service
	fi
	platform=$(detect_platform)
	resolve_version
	tmp=$(mktemp -d) || fail "无法创建临时目录"
	trap 'rm -rf "$tmp"' EXIT
	trap 'exit 130' INT TERM
	download_and_verify "$platform"
	install_binary
	if [ "$service" -eq 1 ]; then
		install_service
	fi
	next_steps
}

main "$@"
