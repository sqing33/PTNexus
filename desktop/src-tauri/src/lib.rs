mod runtime;

use runtime::RuntimeManager;
use tauri::{
    menu::{Menu, MenuItem},
    tray::{MouseButton, MouseButtonState, TrayIconBuilder, TrayIconEvent},
    AppHandle, Manager, RunEvent,
};

#[tauri::command]
fn ping() -> &'static str {
    "pong"
}

/// 供前端 JS 调用，用系统默认浏览器打开外部链接
#[tauri::command]
fn open_external(url: String) {
    let _ = open_url_in_browser(&url);
}

pub fn run() {
    tauri::Builder::default()
        .setup(|app| {
            let handle = app.handle().clone();

            // ── 系统托盘 ──
            let show_i = MenuItem::with_id(app, "show", "显示主界面", true, None::<&str>)?;
            let quit_i = MenuItem::with_id(app, "quit", "退出", true, None::<&str>)?;
            let menu = Menu::with_items(app, &[&show_i, &quit_i])?;

            TrayIconBuilder::new()
                .icon(app.default_window_icon().unwrap().clone())
                .tooltip("PT Nexus")
                .menu(&menu)
                .show_menu_on_left_click(false)
                .on_menu_event(|app, event| match event.id.as_ref() {
                    "show" => {
                        if let Some(w) = app.get_webview_window("main") {
                            let _ = w.show();
                            let _ = w.unminimize();
                            let _ = w.set_focus();
                        }
                    }
                    "quit" => {
                        stop_runtime(app);
                        app.exit(0);
                    }
                    _ => {}
                })
                .on_tray_icon_event(|tray, event| {
                    if let TrayIconEvent::Click {
                        button: MouseButton::Left,
                        button_state: MouseButtonState::Up,
                        ..
                    } = event
                    {
                        let app = tray.app_handle();
                        if let Some(w) = app.get_webview_window("main") {
                            let _ = w.show();
                            let _ = w.unminimize();
                            let _ = w.set_focus();
                        }
                    }
                })
                .build(app)?;

            // ── 外部链接拦截 ──
            // 通过 runtime.rs 在页面加载后注入 JS 脚本来处理
            // （拦截 window.open / <a target="_blank"> / <a href> 等所有外部链接）

            // ── 启动后端服务 ──
            let runtime = match RuntimeManager::bootstrap(&handle) {
                Ok(runtime) => runtime,
                Err(err) => {
                    write_bootstrap_error_log(&handle, &err);
                    show_bootstrap_error_dialog(&handle, &err);
                    return Ok(());
                }
            };

            app.manage(runtime);
            Ok(())
        })
        .invoke_handler(tauri::generate_handler![ping, open_external])
        .build(tauri::generate_context!())
        .expect("error while building tauri application")
        .run(|app_handle, event| match event {
            RunEvent::WindowEvent {
                event: tauri::WindowEvent::CloseRequested { api, .. },
                label,
                ..
            } => {
                if label == "main" {
                    api.prevent_close();
                    if let Some(w) = app_handle.get_webview_window("main") {
                        let _ = w.hide();
                    }
                }
            }
            RunEvent::ExitRequested { .. } => {
                stop_runtime(app_handle);
            }
            _ => {}
        });
}

/// 用系统默认浏览器打开 URL
fn open_url_in_browser(url: &str) -> std::io::Result<()> {
    #[cfg(target_os = "windows")]
    {
        std::process::Command::new("cmd")
            .args(["/C", "start", "", url])
            .spawn()?;
    }
    #[cfg(target_os = "macos")]
    {
        std::process::Command::new("open").arg(url).spawn()?;
    }
    #[cfg(target_os = "linux")]
    {
        std::process::Command::new("xdg-open").arg(url).spawn()?;
    }
    Ok(())
}

fn stop_runtime(app_handle: &AppHandle) {
    if let Some(runtime) = app_handle.try_state::<RuntimeManager>() {
        runtime.shutdown_all();
    }
}

fn write_bootstrap_error_log(app_handle: &AppHandle, error: &str) {
    let path = match app_handle.path().app_data_dir() {
        Ok(dir) => dir.join("bootstrap-error.log"),
        Err(_) => return,
    };

    let _ = std::fs::create_dir_all(
        path.parent()
            .unwrap_or_else(|| std::path::Path::new(".")),
    );
    let _ = std::fs::write(&path, error);
}

fn show_bootstrap_error_dialog(app_handle: &AppHandle, error: &str) {
    let Some(window) = app_handle.get_webview_window("main") else {
        return;
    };

    let message = build_bootstrap_user_message(app_handle, error);
    let js_message = serde_json::to_string(&message).unwrap_or_else(|_| {
        "\"启动失败，请查看 bootstrap-error.log 和 logs/*.stderr.log\"".to_string()
    });

    let script = format!(
        "(function() {{\n  const msg = {js_message};\n  alert(msg);\n  const title = document.querySelector('.title');\n  const desc = document.querySelector('.desc');\n  if (title) title.innerText = 'PT Nexus 启动自检失败';\n  if (desc) {{\n    desc.style.whiteSpace = 'pre-wrap';\n    desc.style.textAlign = 'left';\n    desc.innerText = msg;\n  }}\n}})();"
    );

    let _ = window.eval(&script);
}

fn build_bootstrap_user_message(app_handle: &AppHandle, error: &str) -> String {
    let data_dir = app_handle
        .path()
        .app_data_dir()
        .map(|p| p.to_string_lossy().to_string())
        .unwrap_or_else(|_| "<无法解析应用数据目录>".to_string());

    format!(
        "启动失败，请按以下路径自检：\n\n1) 主错误日志：{data_dir}\\bootstrap-error.log\n2) 服务日志目录：{data_dir}\\logs\\\n   - server.stderr.log\n   - batch.stderr.log\n   - updater.stderr.log\n\n错误详情：\n{error}"
    )
}
