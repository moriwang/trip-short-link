#!/usr/bin/env swift
// 生成 App 图标（🩳 emoji）

import AppKit
import Foundation

let emoji = "🩳"
let iconsetDir = "AppIcon.iconset"
let sizes: [(Int, Int)] = [
    (16, 1), (16, 2),
    (32, 1), (32, 2),
    (128, 1), (128, 2),
    (256, 1), (256, 2),
    (512, 1), (512, 2)
]

// 创建目录
let fm = FileManager.default
try? fm.removeItem(atPath: iconsetDir)
try! fm.createDirectory(atPath: iconsetDir, withIntermediateDirectories: true)

print("生成图标...")

for (size, scale) in sizes {
    let actualSize = size * scale
    
    // 创建图像
    let image = NSImage(size: NSSize(width: actualSize, height: actualSize))
    image.lockFocus()
    
    // 绘制 emoji
    let fontSize = CGFloat(actualSize) * 0.78
    let font = NSFont.systemFont(ofSize: fontSize)
    let attrs: [NSAttributedString.Key: Any] = [.font: font]
    let str = emoji as NSString
    let textSize = str.size(withAttributes: attrs)
    let x = (CGFloat(actualSize) - textSize.width) / 2
    let y = (CGFloat(actualSize) - textSize.height) / 2
    str.draw(at: NSPoint(x: x, y: y), withAttributes: attrs)
    
    image.unlockFocus()
    
    // 保存为 PNG
    guard let tiff = image.tiffRepresentation,
          let bitmap = NSBitmapImageRep(data: tiff),
          let png = bitmap.representation(using: .png, properties: [:]) else {
        print("  ✗ 生成失败: \(size)x\(size)@\(scale)x")
        continue
    }
    
    let filename = scale == 1 
        ? "icon_\(size)x\(size).png" 
        : "icon_\(size)x\(size)@2x.png"
    let path = "\(iconsetDir)/\(filename)"
    try! png.write(to: URL(fileURLWithPath: path))
    print("  ✓ \(filename)")
}

// 转换为 icns
print("\n转换为 icns...")
let process = Process()
process.executableURL = URL(fileURLWithPath: "/usr/bin/iconutil")
process.arguments = ["-c", "icns", iconsetDir, "-o", "AppIcon.icns"]
try! process.run()
process.waitUntilExit()

if process.terminationStatus == 0 {
    try? fm.removeItem(atPath: iconsetDir)
    print("✓ AppIcon.icns 生成成功")
} else {
    print("✗ icns 生成失败")
}

