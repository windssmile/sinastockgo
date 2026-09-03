#!/bin/sh
# 打包成 macOS 的 sinago.app，并封成拖拽安装的 dmg（darwin-arm64）。
#
# TUI 自己不带窗口，所以 .app 只是个启动器：真正显示的是 Terminal 的窗口。
# 代价是 Dock 上跳出来的图标是 Terminal 的，不是我们的——想彻底避开这点
# 只能自带一个终端模拟器，不值当。
set -eu

cd "$(dirname "$0")"
APP="sinago.app"
RES="$APP/Contents/Resources"
VERSION="${VERSION:-1.0}"
DMG="sinago-$VERSION-darwin-arm64.dmg"

rm -rf "$APP"
mkdir -p "$APP/Contents/MacOS" "$RES"

GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags="-s -w" -o "$RES/sinago" .

cat > "$APP/Contents/Info.plist" <<'PLIST'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>CFBundleName</key>            <string>sinago</string>
  <key>CFBundleDisplayName</key>     <string>sinago 行情</string>
  <key>CFBundleIdentifier</key>      <string>local.sinago</string>
  <key>CFBundleVersion</key>         <string>__VERSION__</string>
  <key>CFBundleShortVersionString</key><string>__VERSION__</string>
  <key>CFBundlePackageType</key>     <string>APPL</string>
  <key>CFBundleExecutable</key>      <string>launcher</string>
  <key>CFBundleIconFile</key>        <string>icon</string>
  <!-- 启动器本身不显示界面，别在 Dock 上留一个空图标 -->
  <key>LSUIElement</key>             <true/>
</dict></plist>
PLIST
# 版本号只在脚本顶部写一次，plist 和 dmg 文件名共用，别两处各改各的。
sed -i '' "s/__VERSION__/$VERSION/g" "$APP/Contents/Info.plist"

cat > "$APP/Contents/MacOS/launcher" <<'SH'
#!/bin/sh
BIN="$(cd "$(dirname "$0")/../Resources" && pwd)/sinago"
# 用 osascript 而不是 `open -a Terminal`：前者能指定窗口大小并复用 Terminal，
# 后者每次双击都新开一个窗口，且没法设尺寸——12 列在小窗口里会被截断。
osascript <<APPLESCRIPT
tell application "Terminal"
  activate
  set w to do script "clear; exec '$BIN'"
  set number of columns of front window to 150
  set number of rows of front window to 40
  set custom title of front window to "sinago 行情"
end tell
APPLESCRIPT
SH
chmod +x "$APP/Contents/MacOS/launcher"

# 图标：没有 Pillow 就跳过，用系统默认图标，不影响能不能跑。
if python3 -c "import PIL" 2>/dev/null; then
  python3 make-icon.py "$RES/icon.icns"
fi

# 本地自签，省掉「已损坏，无法打开」的 Gatekeeper 拦截。
codesign --force --deep --sign - "$APP" 2>/dev/null || true

# dmg：用系统自带的 hdiutil，不引 create-dmg 之类的额外依赖。装了 Applications
# 软链就是 macOS 上「拖过去即安装」的标准姿势，不必自己画背景图摆图标。
STAGE="$(mktemp -d)"
trap 'rm -rf "$STAGE"' EXIT
cp -R "$APP" "$STAGE/"
ln -s /Applications "$STAGE/Applications"

# 自签的包从别的机器下载会被 Gatekeeper 隔离，留一行解法比让人自己搜省事。
cat > "$STAGE/使用说明.txt" <<TXT
sinago 行情 $VERSION (darwin-arm64)

安装：把 sinago.app 拖到旁边的 Applications 文件夹。

首次打开若提示「已损坏」或「无法验证开发者」，是本地自签包被
Gatekeeper 隔离所致，终端执行一次即可：

  xattr -dr com.apple.quarantine /Applications/sinago.app

按键：a 添加  d 删除  J/K 调序  Enter 详情  n 到价提醒  f 闪烁  r 刷新  q 退出

到价提醒可以填绝对价（2.09），也可以直接填涨跌幅（+5% / -2%），
百分比以昨收为准，跟表里「涨跌幅」列同口径。

到价提醒走 macOS 系统通知，会挂在「脚本编辑器」名下。第一次不弹的话，
去 系统设置 › 通知 › 脚本编辑器，把「允许通知」打开。
自选和日志在 ~/Library/Application Support/sinago/
TXT

rm -f "$DMG"
hdiutil create -quiet -volname "sinago $VERSION" -srcfolder "$STAGE" \
  -fs HFS+ -format UDZO -ov "$DMG"

echo "已生成 $(pwd)/$DMG —— 双击打开后把 sinago.app 拖进 Applications"
