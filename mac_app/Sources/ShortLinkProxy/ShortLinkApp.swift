import SwiftUI

@main
struct TripShortsApp: App {
    @StateObject private var manager = ProxyManager()
    
    var body: some Scene {
        MenuBarExtra {
            MainView()
                .environmentObject(manager)
                .onAppear {
                    if !manager.isRunning && !manager.isStarting {
                        manager.start()
                    }
                }
        } label: {
            HStack(spacing: 4) {
                Text("🩳")
                    .font(.system(size: 12))
                
                if manager.isRunning && manager.requestCount > 0 {
                    Text("\(manager.requestCount)")
                        .font(.system(size: 11, weight: .medium))
                        .monospacedDigit()
                }
            }
        }
        .menuBarExtraStyle(.window)
    }
}

struct MainView: View {
    @EnvironmentObject var manager: ProxyManager
    
    var body: some View {
        VStack(spacing: 0) {
            // 标题 + 退出
            HStack {
                Text("Trip Shorts")
                    .font(.system(size: 13, weight: .semibold))
                
                Spacer()
                
                Button {
                    manager.stop()
                    NSApplication.shared.terminate(nil)
                } label: {
                    Text("Quit")
                        .font(.system(size: 10, weight: .medium))
                        .foregroundStyle(.red)
                }
                .buttonStyle(.plain)
            }
            .padding(.horizontal, 14)
            .padding(.top, 10)
            .padding(.bottom, 8)
            
            Divider()
            
            // 加载中 / 统计
            if manager.isStarting {
                HStack(spacing: 8) {
                    ProgressView()
                        .scaleEffect(0.55)
                    Text("Loading...")
                        .font(.system(size: 11))
                        .foregroundStyle(.secondary)
                }
                .padding(.vertical, 12)
            } else if manager.isRunning {
                HStack(spacing: 0) {
                    StatItem(value: "\(manager.requestCount)", label: "转发数")
                    StatItem(value: "\(manager.mappingsCount)", label: "规则数")
                }
                .padding(.vertical, 10)
            }
            
            // 错误
            if let error = manager.lastError {
                Divider()
                Text(error)
                    .font(.system(size: 11))
                    .foregroundStyle(.orange)
                    .padding(.horizontal, 14)
                    .padding(.vertical, 10)
                    .frame(maxWidth: .infinity, alignment: .leading)
            }
        }
        .frame(width: 160)
    }
}

// MARK: - Components

struct StatItem: View {
    let value: String
    let label: String
    
    var body: some View {
        VStack(spacing: 2) {
            Text(value)
                .font(.system(size: 15, weight: .semibold, design: .rounded))
                .monospacedDigit()
            Text(label)
                .font(.system(size: 10))
                .foregroundStyle(.tertiary)
        }
        .frame(maxWidth: .infinity)
    }
}
