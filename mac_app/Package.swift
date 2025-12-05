// swift-tools-version: 5.9
import PackageDescription

let package = Package(
    name: "ShortLinkProxy",
    platforms: [
        .macOS(.v14)
    ],
    products: [
        .executable(name: "ShortLinkProxy", targets: ["ShortLinkProxy"])
    ],
    targets: [
        .executableTarget(
            name: "ShortLinkProxy",
            path: "Sources/ShortLinkProxy"
        )
    ]
)

