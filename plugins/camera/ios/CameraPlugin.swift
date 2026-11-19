import AVFoundation
import UIKit

private final class PreviewView: UIView, AVCapturePhotoCaptureDelegate, AVCaptureMetadataOutputObjectsDelegate {
    override class var layerClass: AnyClass { AVCaptureVideoPreviewLayer.self }
    let viewID: Int
    let session = AVCaptureSession()
    let output = AVCapturePhotoOutput()
    let metadata = AVCaptureMetadataOutput()
    var waiting: PluginReply?
    private var lastCode: String = ""
    private var lastCodeAt: CFTimeInterval = 0

    init(id: Int) {
        self.viewID = id
        super.init(frame: .zero)
    }

    required init?(coder: NSCoder) { fatalError() }

    func start(front: Bool, scanCodes: Bool) {
        (layer as! AVCaptureVideoPreviewLayer).session = session
        (layer as! AVCaptureVideoPreviewLayer).videoGravity = .resizeAspectFill
        AVCaptureDevice.requestAccess(for: .video) { granted in
            guard granted else { return }
            let position: AVCaptureDevice.Position = front ? .front : .back
            guard let d = AVCaptureDevice.default(.builtInWideAngleCamera, for: .video, position: position),
                  let input = try? AVCaptureDeviceInput(device: d) else { return }
            self.session.beginConfiguration()
            if self.session.canAddInput(input) { self.session.addInput(input) }
            if self.session.canAddOutput(self.output) { self.session.addOutput(self.output) }
            if scanCodes && self.session.canAddOutput(self.metadata) {
                self.session.addOutput(self.metadata)
                self.metadata.setMetadataObjectsDelegate(self, queue: .main)
                let qrTypes: [AVMetadataObject.ObjectType] = [.qr]
                self.metadata.metadataObjectTypes = qrTypes.filter { self.metadata.availableMetadataObjectTypes.contains($0) }
            }
            self.session.commitConfiguration()
            DispatchQueue.global(qos: .userInitiated).async { self.session.startRunning() }
        }
    }

    func metadataOutput(_ output: AVCaptureMetadataOutput, didOutput metadataObjects: [AVMetadataObject], from connection: AVCaptureConnection) {
        for obj in metadataObjects {
            guard let readable = obj as? AVMetadataMachineReadableCodeObject,
                  let str = readable.stringValue, !str.isEmpty else { continue }
            let now = CACurrentMediaTime()
            if str == lastCode && now - lastCodeAt < 1.5 {
                continue
            }
            lastCode = str
            lastCodeAt = now
            PluginHost.emit("camera", "code", ["id": viewID, "value": str])
            break
        }
    }

    func stop() {
        DispatchQueue.global(qos: .userInitiated).async { self.session.stopRunning() }
    }

    func photoOutput(_ o: AVCapturePhotoOutput, didFinishProcessingPhoto photo: AVCapturePhoto, error: Error?) {
        guard let reply = waiting else { return }
        waiting = nil
        guard let data = photo.fileDataRepresentation() else {
            reply(.failure(PluginFailure("Failed", error?.localizedDescription ?? "no photo")))
            return
        }
        let path = NSTemporaryDirectory() + UUID().uuidString + ".jpg"
        do {
            try data.write(to: URL(fileURLWithPath: path))
            reply(.success(["path": path]))
        } catch {
            reply(.failure(PluginFailure("Failed", error.localizedDescription)))
        }
    }
}

final class CameraPlugin: MyGoPlugin {
    private var views: [Int: PreviewView] = [:]

    init() {
        PluginHost.registerView("camera") { [weak self] id, args in
            let v = PreviewView(id: id)
            self?.views[id] = v
            v.start(front: args["front"] as? Bool ?? false, scanCodes: args["scanCodes"] as? Bool ?? false)
            return v
        }
    }

    func call(_ method: String, _ args: [String: Any], _ reply: @escaping PluginReply) {
        guard let id = args["id"] as? Int, let v = views[id] else {
            return reply(.failure(PluginFailure("NoView")))
        }
        switch method {
        case "takePhoto":
            guard !v.session.outputs.isEmpty, v.session.isRunning else {
                return reply(.failure(PluginFailure("NoCamera", "the camera is not running")))
            }
            v.waiting = reply
            v.output.capturePhoto(with: AVCapturePhotoSettings(), delegate: v)
        case "dispose":
            v.stop()
            views[id] = nil
            reply(.success(nil))
        default:
            reply(.failure(PluginFailure("NoMethod", method)))
        }
    }
}
