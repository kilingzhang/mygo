import UIKit

final class SFSymbolsPlugin: MyGoPlugin {
    func call(_ method: String, _ args: [String: Any], _ reply: @escaping PluginReply) {
        reply(.failure(PluginFailure("NoMethod", method)))
    }

    func callSync(_ method: String, _ args: [String: Any]) -> Data? {
        guard method == "rasterize",
              let name = args["name"] as? String,
              let pixels = args["pixels"] as? Int, (8...512).contains(pixels),
              let natural = args["natural"] as? Bool,
              let weight = args["weight"] as? Int else { return nil }

        let weights: [UIImage.SymbolWeight] = [
            .ultraLight, .thin, .light, .regular, .medium, .semibold, .bold, .heavy, .black
        ]
        let symbolWeight = (-3...5).contains(weight) ? weights[weight + 3] : .regular
        let side = CGFloat(pixels)
        let config = UIImage.SymbolConfiguration(pointSize: side, weight: symbolWeight)
        guard let symbol = UIImage(systemName: name, withConfiguration: config) else { return Data() }

        let format = UIGraphicsImageRendererFormat()
        format.scale = 1
        format.preferredRange = .standard
        let size = CGSize(width: ceil(symbol.size.width), height: ceil(symbol.size.height))
        guard size.width > 0 && size.height > 0 else { return nil }
        let image = UIGraphicsImageRenderer(size: size, format: format).image { _ in
            symbol.withTintColor(.white, renderingMode: .alwaysOriginal).draw(at: .zero)
        }
        guard let source = image.cgImage, let ink = inkBounds(in: source) else { return nil }

        let fit = min(0.85 * side / ink.width, 0.85 * side / ink.height)
        let scale = natural ? min(fit, 0.92 / 1.25) : fit
        let rect = CGRect(x: (side - scale * ink.width) / 2 - scale * ink.minX,
                          y: (side - scale * ink.height) / 2 - scale * ink.minY,
                          width: scale * size.width, height: scale * size.height)
        return UIGraphicsImageRenderer(size: CGSize(width: side, height: side), format: format).pngData { ctx in
            ctx.cgContext.interpolationQuality = .high
            image.draw(in: rect)
        }
    }

    private func inkBounds(in image: CGImage) -> CGRect? {
        let width = image.width
        let height = image.height
        let space = CGColorSpaceCreateDeviceRGB()
        let format = CGImageAlphaInfo.premultipliedLast.rawValue | CGBitmapInfo.byteOrder32Big.rawValue
        guard let ctx = CGContext(data: nil, width: width, height: height, bitsPerComponent: 8,
                                  bytesPerRow: width * 4, space: space, bitmapInfo: format),
              let data = ctx.data?.assumingMemoryBound(to: UInt8.self) else { return nil }
        ctx.draw(image, in: CGRect(x: 0, y: 0, width: CGFloat(width), height: CGFloat(height)))

        var left = width
        var top = height
        var right = -1
        var bottom = -1
        for y in 0..<height {
            for x in 0..<width where data[(y * width + x) * 4 + 3] != 0 {
                left = min(left, x)
                top = min(top, y)
                right = max(right, x)
                bottom = max(bottom, y)
            }
        }
        guard right >= left && bottom >= top else { return nil }
        return CGRect(x: CGFloat(left), y: CGFloat(top),
                      width: CGFloat(right - left + 1), height: CGFloat(bottom - top + 1))
    }
}
