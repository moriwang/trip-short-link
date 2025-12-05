import Foundation
import AppKit

class ProxyManager: ObservableObject {
    @Published var isRunning = false
    @Published var isStarting = false
    @Published var mappingsCount = 0
    @Published var requestCount: Int = 0
    @Published var lastError: String? = nil
    
    private var process: Process?
    private var statusTimer: Timer?
    private var fileMonitor: DispatchSourceFileSystemObject?
    
    let proxyPort = "8172"
    private let pacUrl = "http://127.0.0.1:8172/proxy.pac"
    
    // 固定路径：App Bundle 内
    private var binaryPath: String {
        Bundle.main.bundleURL
            .appendingPathComponent("Contents/MacOS/trip-short-link")
            .path
    }
    
    private var configPath: String {
        Bundle.main.bundleURL
            .appendingPathComponent("Contents/Resources/config.json")
            .path
    }
    
    init() {
        cleanupLegacy()
    }
    
    deinit {
        stopFileMonitor()
    }
    
    // MARK: - Service Control
    
    func start() {
        guard !isStarting && !isRunning else { return }
        
        let fm = FileManager.default
        guard fm.isExecutableFile(atPath: binaryPath) else {
            lastError = "找不到 trip-short-link"
            return
        }
        guard fm.fileExists(atPath: configPath) else {
            lastError = "找不到 config.json"
            return
        }
        
        isStarting = true
        lastError = nil
        
        let task = Process()
        task.executableURL = URL(fileURLWithPath: binaryPath)
        task.currentDirectoryURL = URL(fileURLWithPath: (binaryPath as NSString).deletingLastPathComponent)
        
        var env = ProcessInfo.processInfo.environment
        env["PORT"] = proxyPort
        env["CONFIG_FILE"] = configPath
        task.environment = env
        
        task.terminationHandler = { [weak self] _ in
            DispatchQueue.main.async {
                self?.isRunning = false
                self?.isStarting = false
                self?.requestCount = 0
                self?.disableSystemProxy()
                self?.stopFileMonitor()
            }
        }
        
        do {
            try task.run()
            self.process = task
            
            DispatchQueue.main.asyncAfter(deadline: .now() + 1.5) { [weak self] in
                self?.verifyServiceStarted()
            }
        } catch {
            lastError = "启动失败: \(error.localizedDescription)"
            isStarting = false
        }
    }
    
    private func verifyServiceStarted() {
        guard let url = URL(string: "http://127.0.0.1:\(proxyPort)/check") else {
            isStarting = false
            lastError = "无法验证服务状态"
            return
        }
        
        URLSession.shared.dataTask(with: url) { [weak self] data, response, _ in
            DispatchQueue.main.async {
                guard let self = self else { return }
                self.isStarting = false
                
                if let _ = data, (response as? HTTPURLResponse)?.statusCode == 200 {
                    self.isRunning = true
                    self.enableSystemProxy()
                    self.startStatusChecker()
                    self.startFileMonitor()
                } else {
                    self.lastError = "服务启动失败"
                    self.process?.terminate()
                    self.process = nil
                }
            }
        }.resume()
    }
    
    func stop() {
        stopFileMonitor()
        process?.terminate()
        process = nil
        disableSystemProxy()
        statusTimer?.invalidate()
        statusTimer = nil
        isRunning = false
        isStarting = false
        requestCount = 0
    }
    
    func reloadConfig() {
        guard let pid = process?.processIdentifier else { return }
        let task = Process()
        task.executableURL = URL(fileURLWithPath: "/bin/kill")
        task.arguments = ["-USR1", String(pid)]
        try? task.run()
    }
    
    // MARK: - File Monitor
    
    private func startFileMonitor() {
        let fd = open(configPath, O_EVTONLY)
        guard fd >= 0 else { return }
        
        let source = DispatchSource.makeFileSystemObjectSource(
            fileDescriptor: fd,
            eventMask: [.write, .rename],
            queue: .main
        )
        
        source.setEventHandler { [weak self] in
            self?.reloadConfig()
        }
        source.setCancelHandler { close(fd) }
        source.resume()
        self.fileMonitor = source
    }
    
    private func stopFileMonitor() {
        fileMonitor?.cancel()
        fileMonitor = nil
    }
    
    // MARK: - System Proxy
    
    private func getActiveNetworkService() -> String? {
        let task = Process()
        task.executableURL = URL(fileURLWithPath: "/usr/sbin/networksetup")
        task.arguments = ["-listallnetworkservices"]
        
        let pipe = Pipe()
        task.standardOutput = pipe
        
        do {
            try task.run()
            task.waitUntilExit()
            let data = pipe.fileHandleForReading.readDataToEndOfFile()
            if let output = String(data: data, encoding: .utf8) {
                return output.components(separatedBy: .newlines)
                    .first { $0.contains("Wi-Fi") || $0.contains("Ethernet") }?
                    .replacingOccurrences(of: "*", with: "")
                    .trimmingCharacters(in: .whitespaces)
            }
        } catch {}
        return nil
    }
    
    private func enableSystemProxy() {
        guard let service = getActiveNetworkService() else { return }
        runNetworkSetup(["-setautoproxyurl", service, pacUrl])
        runNetworkSetup(["-setautoproxystate", service, "on"])
    }
    
    private func disableSystemProxy() {
        guard let service = getActiveNetworkService() else { return }
        runNetworkSetup(["-setautoproxystate", service, "off"])
    }
    
    private func runNetworkSetup(_ args: [String]) {
        let task = Process()
        task.executableURL = URL(fileURLWithPath: "/usr/sbin/networksetup")
        task.arguments = args
        try? task.run()
        task.waitUntilExit()
    }
    
    private func cleanupLegacy() {
        let task = Process()
        task.executableURL = URL(fileURLWithPath: "/usr/bin/pkill")
        task.arguments = ["-f", "trip-short-link"]
        try? task.run()
    }
    
    // MARK: - Status Checker
    
    private func startStatusChecker() {
        checkHealth()
        statusTimer = Timer.scheduledTimer(withTimeInterval: 2.0, repeats: true) { [weak self] _ in
            self?.checkHealth()
        }
    }
    
    private func checkHealth() {
        guard let url = URL(string: "http://127.0.0.1:\(proxyPort)/check") else { return }
        
        URLSession.shared.dataTask(with: url) { [weak self] data, _, _ in
            guard let data = data,
                  let json = try? JSONSerialization.jsonObject(with: data) as? [String: Any] else { return }
            
            DispatchQueue.main.async {
                if let mappings = json["mappings"] as? [String: Any] {
                    self?.mappingsCount = mappings["total"] as? Int ?? 0
                }
                if let count = json["request_count"] as? Int {
                    self?.requestCount = count
                }
            }
        }.resume()
    }
}
