#!/bin/bash
set -e

APP_NAME="Trip Shorts"
BUNDLE_ID="com.trip.shorts"

# 从 git tag 获取版本号，如果没有则使用 dev
if git describe --tags --exact-match HEAD >/dev/null 2>&1; then
    VERSION=$(git describe --tags --exact-match HEAD | sed 's/^v//')
else
    VERSION="dev"
fi

GREEN='\033[0;32m'
BLUE='\033[0;34m'
RED='\033[0;31m'
NC='\033[0m'

echo -e "${BLUE}[1/5] 编译 Go 服务...${NC}"
go build -o trip-short-link main.go

echo -e "${BLUE}[2/5] 编译 Swift App...${NC}"
cd mac_app
swift build -c release
SWIFT_BIN=$(swift build -c release --show-bin-path)/ShortLinkProxy
cd ..

echo -e "${BLUE}[3/5] 生成图标...${NC}"
if [ ! -f "AppIcon.icns" ]; then
    swift generate_icon.swift
fi

echo -e "${BLUE}[4/5] 创建 App Bundle...${NC}"

APP_DIR="$APP_NAME.app"
rm -rf "$APP_DIR"
mkdir -p "$APP_DIR/Contents/MacOS"
mkdir -p "$APP_DIR/Contents/Resources"

# 复制可执行文件
cp "$SWIFT_BIN" "$APP_DIR/Contents/MacOS/TripShorts"
cp trip-short-link "$APP_DIR/Contents/MacOS/"

# 复制配置文件（如果存在）
if [ -f config.json ]; then
    cp config.json "$APP_DIR/Contents/Resources/"
else
    echo -e "${RED}警告: config.json 不存在，请手动添加到 $APP_DIR/Contents/Resources/${NC}"
fi

# 复制图标
if [ -f AppIcon.icns ]; then
    cp AppIcon.icns "$APP_DIR/Contents/Resources/"
fi

# Info.plist
cat > "$APP_DIR/Contents/Info.plist" << EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>CFBundleName</key>
    <string>${APP_NAME}</string>
    <key>CFBundleDisplayName</key>
    <string>${APP_NAME}</string>
    <key>CFBundleIdentifier</key>
    <string>${BUNDLE_ID}</string>
    <key>CFBundleVersion</key>
    <string>${VERSION}</string>
    <key>CFBundleShortVersionString</key>
    <string>${VERSION}</string>
    <key>CFBundleExecutable</key>
    <string>TripShorts</string>
    <key>CFBundlePackageType</key>
    <string>APPL</string>
    <key>LSMinimumSystemVersion</key>
    <string>14.0</string>
    <key>LSUIElement</key>
    <true/>
    <key>NSHighResolutionCapable</key>
    <true/>
    <key>CFBundleIconFile</key>
    <string>AppIcon</string>
</dict>
</plist>
EOF

echo -e "${BLUE}[5/5] 完成${NC}"
echo -e "${GREEN}✓ 构建完成！${NC}"
echo ""
echo "App 位置: $(pwd)/$APP_DIR"
echo ""
echo "App 结构:"
echo "  $APP_DIR/"
echo "  └── Contents/"
echo "      ├── MacOS/"
echo "      │   ├── TripShorts        (Swift UI)"
echo "      │   └── trip-short-link   (Go 代理服务)"
echo "      ├── Resources/"
echo "      │   └── config.json       (配置文件)"
echo "      └── Info.plist"
echo ""
echo "使用方式:"
echo "  双击运行，或拖到 /Applications"
echo ""
echo "更新配置:"
echo "  直接编辑 $APP_DIR/Contents/Resources/config.json"
echo "  修改后自动生效，无需重启"
