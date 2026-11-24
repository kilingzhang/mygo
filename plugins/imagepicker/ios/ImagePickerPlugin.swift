import AVFoundation
import ImageIO
import Photos
import PhotosUI
import UIKit
import UniformTypeIdentifiers

final class ImagePickerPlugin: NSObject, MyGoPlugin, PHPickerViewControllerDelegate, UIImagePickerControllerDelegate, UINavigationControllerDelegate {
    private static let tempRootName = "mygo-imagepicker"
    private static let tempRetentionSeconds: TimeInterval = 24 * 60 * 60

    private var picking: PluginReply?
    private var capturing: PluginReply?

    func call(_ method: String, _ args: [String: Any], _ reply: @escaping PluginReply) {
        switch method {
        case "pick":
            Self.pruneExpiredTempDirsAsync()
            picking = reply
            var cfg = PHPickerConfiguration()
            cfg.preferredAssetRepresentationMode = .compatible
            switch args["kind"] as? String {
            case "videos": cfg.filter = .videos
            case "any": cfg.filter = nil
            default: cfg.filter = .images
            }
            let limit = args["limit"] as? Int ?? 1
            cfg.selectionLimit = limit < 0 ? 0 : limit
            let vc = PHPickerViewController(configuration: cfg)
            vc.delegate = self
            PluginHost.present(vc)
        case "capture":
            guard UIImagePickerController.isSourceTypeAvailable(.camera) else {
                reply(.failure(PluginFailure("NoCamera", "the device has no camera")))
                return
            }
            Self.pruneExpiredTempDirsAsync()
            capturing = reply
            let vc = UIImagePickerController()
            vc.sourceType = .camera
            let available = UIImagePickerController.availableMediaTypes(for: .camera) ?? []
            switch args["kind"] as? String {
            case "videos":
                if available.contains(UTType.movie.identifier) {
                    vc.mediaTypes = [UTType.movie.identifier]
                }
            case "any":
                let wanted = [UTType.image.identifier, UTType.movie.identifier].filter { available.contains($0) }
                if !wanted.isEmpty {
                    vc.mediaTypes = wanted
                }
            default:
                break
            }
            vc.delegate = self
            PluginHost.present(vc)
        default:
            reply(.failure(PluginFailure("NoMethod", method)))
        }
    }

    private static func tempRootURL() -> URL {
        URL(fileURLWithPath: NSTemporaryDirectory(), isDirectory: true)
            .appendingPathComponent(tempRootName, isDirectory: true)
    }

    private static func makeBatchDir() -> URL {
        let dir = tempRootURL().appendingPathComponent(UUID().uuidString, isDirectory: true)
        try? FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        return dir
    }

    private static func pruneExpiredTempDirsAsync() {
        let root = tempRootURL()
        let cutoff = Date().addingTimeInterval(-tempRetentionSeconds)
        DispatchQueue.global(qos: .utility).async {
            let fm = FileManager.default
            guard let items = try? fm.contentsOfDirectory(
                at: root,
                includingPropertiesForKeys: [.contentModificationDateKey],
                options: [.skipsHiddenFiles]
            ) else { return }
            for item in items {
                let values = try? item.resourceValues(forKeys: [.contentModificationDateKey])
                if let modified = values?.contentModificationDate, modified < cutoff {
                    try? fm.removeItem(at: item)
                }
            }
        }
    }

    private static func preferredTypeIdentifier(for provider: NSItemProvider) -> String? {
        // Live Photos can conform to both image and movie types; keep them as still images.
        if provider.canLoadObject(ofClass: PHLivePhoto.self),
           provider.hasItemConformingToTypeIdentifier(UTType.image.identifier) {
            return UTType.image.identifier
        }
        if provider.hasItemConformingToTypeIdentifier(UTType.movie.identifier) {
            return UTType.movie.identifier
        }
        if provider.hasItemConformingToTypeIdentifier(UTType.image.identifier) {
            return UTType.image.identifier
        }
        return provider.registeredTypeIdentifiers.first
    }

    private static func convertHEICToJPEGIfNeeded(_ fileURL: URL) -> URL {
        let ext = fileURL.pathExtension.lowercased()
        guard ext == "heic" || ext == "heif" else { return fileURL }
        guard let src = CGImageSourceCreateWithURL(fileURL as CFURL, nil) else { return fileURL }
        let jpgURL = fileURL.deletingPathExtension().appendingPathExtension("jpg")
        guard let dest = CGImageDestinationCreateWithURL(jpgURL as CFURL, UTType.jpeg.identifier as CFString, 1, nil) else {
            return fileURL
        }
        let options = [kCGImageDestinationLossyCompressionQuality: 0.92] as CFDictionary
        CGImageDestinationAddImageFromSource(dest, src, 0, options)
        guard CGImageDestinationFinalize(dest) else {
            try? FileManager.default.removeItem(at: jpgURL)
            return fileURL
        }
        try? FileManager.default.removeItem(at: fileURL)
        return jpgURL
    }

    private static func remuxMOVToMP4IfCompatible(_ fileURL: URL, completion: @escaping (URL) -> Void) {
        let ext = fileURL.pathExtension.lowercased()
        guard ext == "mov" || ext == "qt" else {
            completion(fileURL)
            return
        }
        let asset = AVURLAsset(url: fileURL)
        guard let session = AVAssetExportSession(asset: asset, presetName: AVAssetExportPresetPassthrough) else {
            completion(fileURL)
            return
        }
        let mp4URL = fileURL.deletingPathExtension().appendingPathExtension("mp4")
        try? FileManager.default.removeItem(at: mp4URL)
        session.determineCompatibleFileTypes { types in
            guard types.contains(.mp4) else {
                completion(fileURL)
                return
            }
            session.outputURL = mp4URL
            session.outputFileType = .mp4
            session.shouldOptimizeForNetworkUse = true
            session.exportAsynchronously {
                let fm = FileManager.default
                if session.status == .completed,
                   let attrs = try? fm.attributesOfItem(atPath: mp4URL.path),
                   let size = attrs[.size] as? NSNumber,
                   size.int64Value > 0 {
                    try? fm.removeItem(at: fileURL)
                    completion(mp4URL)
                } else {
                    try? fm.removeItem(at: mp4URL)
                    completion(fileURL)
                }
            }
        }
    }

    private static func normalizeMediaFile(at fileURL: URL, completion: @escaping (URL) -> Void) {
        let imageFixed = convertHEICToJPEGIfNeeded(fileURL)
        remuxMOVToMP4IfCompatible(imageFixed, completion: completion)
    }

    private static func copyPickedFile(
        from provider: NSItemProvider,
        typeIdentifier: String,
        fallbackIdentifier: String?,
        dir: URL,
        index: Int,
        completion: @escaping (Result<String, Error>) -> Void
    ) {
        provider.loadFileRepresentation(forTypeIdentifier: typeIdentifier) { url, error in
            if let url {
                let name = url.lastPathComponent.isEmpty ? "media" : url.lastPathComponent
                let to = dir.appendingPathComponent("\(index)-\(name)")
                do {
                    try FileManager.default.copyItem(at: url, to: to)
                    normalizeMediaFile(at: to) { normalized in
                        completion(.success(normalized.path))
                    }
                } catch {
                    completion(.failure(error))
                }
                return
            }
            if let fallback = fallbackIdentifier, fallback != typeIdentifier {
                copyPickedFile(
                    from: provider,
                    typeIdentifier: fallback,
                    fallbackIdentifier: nil,
                    dir: dir,
                    index: index,
                    completion: completion
                )
                return
            }
            completion(.failure(error ?? PluginFailure("Failed", "could not load selected media")))
        }
    }

    func picker(_ vc: PHPickerViewController, didFinishPicking results: [PHPickerResult]) {
        vc.dismiss(animated: true)
        guard let reply = picking else { return }
        picking = nil
        guard !results.isEmpty else {
            reply(.success(["paths": [String]()]))
            return
        }
        let group = DispatchGroup()
        let dir = Self.makeBatchDir()
        var paths = [String?](repeating: nil, count: results.count)
        var firstError: String?
        let lock = NSLock()
        for (i, r) in results.enumerated() {
            let p = r.itemProvider
            guard let type = Self.preferredTypeIdentifier(for: p) else { continue }
            let fallback = p.registeredTypeIdentifiers.first
            group.enter()
            Self.copyPickedFile(from: p, typeIdentifier: type, fallbackIdentifier: fallback, dir: dir, index: i) { result in
                defer { group.leave() }
                lock.lock()
                switch result {
                case .success(let path):
                    paths[i] = path
                case .failure(let error):
                    if firstError == nil {
                        firstError = error.localizedDescription
                    }
                }
                lock.unlock()
            }
        }
        group.notify(queue: .main) {
            let picked = paths.compactMap { $0 }
            if picked.isEmpty, let firstError {
                reply(.failure(PluginFailure("Failed", firstError)))
            } else {
                reply(.success(["paths": picked]))
            }
        }
    }

    func imagePickerController(_ vc: UIImagePickerController, didFinishPickingMediaWithInfo info: [UIImagePickerController.InfoKey: Any]) {
        vc.dismiss(animated: true)
        guard let reply = capturing else { return }
        capturing = nil
        if let mediaURL = info[.mediaURL] as? URL {
            let dir = Self.makeBatchDir()
            let name = mediaURL.lastPathComponent.isEmpty ? "video.mov" : mediaURL.lastPathComponent
            let dest = dir.appendingPathComponent(name)
            DispatchQueue.global(qos: .userInitiated).async {
                do {
                    try FileManager.default.copyItem(at: mediaURL, to: dest)
                    Self.remuxMOVToMP4IfCompatible(dest) { finalURL in
                        reply(.success(["path": finalURL.path]))
                    }
                } catch {
                    reply(.failure(PluginFailure("Failed", error.localizedDescription)))
                }
            }
            return
        }
        guard let image = info[.originalImage] as? UIImage, let jpeg = image.jpegData(compressionQuality: 0.9) else {
            reply(.failure(PluginFailure("Failed", "no photo")))
            return
        }
        let path = Self.makeBatchDir().appendingPathComponent(UUID().uuidString + ".jpg").path
        do {
            try jpeg.write(to: URL(fileURLWithPath: path))
            reply(.success(["path": path]))
        } catch {
            reply(.failure(PluginFailure("Failed", error.localizedDescription)))
        }
    }

    func imagePickerControllerDidCancel(_ vc: UIImagePickerController) {
        vc.dismiss(animated: true)
        capturing?(.success(["path": ""]))
        capturing = nil
    }
}
