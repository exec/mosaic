import AppKit

// Wails passes the packaged executable to this macOS-only post-build hook.
let executable = URL(fileURLWithPath: CommandLine.arguments[1])
let app = executable.deletingLastPathComponent().deletingLastPathComponent().deletingLastPathComponent()
guard app.pathExtension == "app" else {
    print("Skipping macOS icon for an unpackaged executable")
    exit(0)
}
let root = URL(fileURLWithPath: #filePath).deletingLastPathComponent().deletingLastPathComponent()
let source = NSBitmapImageRep(data: try Data(contentsOf: root.appendingPathComponent("build/appicon.png")))!
guard source.pixelsWide == 1024, source.pixelsHigh == 1024, let image = source.cgImage else {
    fatalError("Expected a 1024 by 1024 source icon")
}
let temporary = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
let iconset = temporary.appendingPathComponent("Mosaic.iconset")
try FileManager.default.createDirectory(at: iconset, withIntermediateDirectories: true)
defer { try? FileManager.default.removeItem(at: temporary) }

for points in [16, 32, 128, 256, 512] {
    for scale in [1, 2] {
        let pixels = points * scale
        let context = CGContext(data: nil, width: pixels, height: pixels, bitsPerComponent: 8,
            bytesPerRow: pixels * 4, space: CGColorSpace(name: CGColorSpace.sRGB)!,
            bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue)!
        let factor = CGFloat(pixels) / 1024
        context.interpolationQuality = .high
        // Legacy ICNS artwork sits on an 824px tile within a 1024px canvas.
        // Full-bleed layers belong to Icon Composer, not a flattened Dock icon.
        context.setShadow(offset: CGSize(width: 0, height: -8 * factor), blur: 16 * factor,
            color: CGColor(gray: 0, alpha: 0.22))
        context.draw(image, in: CGRect(x: 100 * factor, y: 100 * factor, width: 824 * factor, height: 824 * factor))
        let bitmap = NSBitmapImageRep(cgImage: context.makeImage()!)
        // Catch accidental full-bleed exports at every standard/Retina size.
        precondition(bitmap.pixelsWide == pixels && bitmap.pixelsHigh == pixels)
        precondition(bitmap.colorAt(x: 0, y: pixels / 2)!.alphaComponent < 0.01)
        precondition(bitmap.colorAt(x: 0, y: 0)!.alphaComponent < 0.01)
        precondition(bitmap.colorAt(x: pixels / 2, y: pixels / 2)!.alphaComponent > 0.99)
        let suffix = scale == 2 ? "@2x" : ""
        let output = iconset.appendingPathComponent("icon_\(points)x\(points)\(suffix).png")
        try bitmap.representation(using: .png, properties: [:])!.write(to: output)
    }
}

func run(_ command: String, _ arguments: [String]) throws {
    let process = Process()
    process.executableURL = URL(fileURLWithPath: command)
    process.arguments = arguments
    try process.run()
    process.waitUntilExit()
    guard process.terminationStatus == 0 else {
        throw NSError(domain: command, code: Int(process.terminationStatus))
    }
}
let destination = app.appendingPathComponent("Contents/Resources/iconfile.icns")
try run("/usr/bin/iconutil", ["-c", "icns", iconset.path, "-o", destination.path])
// Replacing a resource invalidates Wails's ad-hoc signature. Release signing follows later.
try run("/usr/bin/codesign", ["--force", "--sign", "-", "--preserve-metadata=entitlements,requirements,flags,runtime", app.path])
try run("/usr/bin/codesign", ["--verify", "--strict", app.path])
print("Installed macOS icon: 824px artwork, 100px margin, all standard/Retina sizes")
