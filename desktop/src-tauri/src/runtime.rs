use std::collections::{HashMap, HashSet};
use std::fs::{self, OpenOptions};
use std::net::TcpListener;
use std::path::{Path, PathBuf};
use std::process::{Child, Command, Stdio};
#[cfg(target_os = "windows")]
use std::os::windows::process::CommandExt;
use std::sync::{Arc, Mutex};
use std::thread;
use std::time::{Duration, Instant};

use tauri::path::BaseDirectory;
use tauri::{AppHandle, Emitter, Manager};
use tauri::WebviewWindow;

/// 注入到前端页面的 JS 脚本，用于拦截 window.open 和 <a target="_blank"> 等外部链接，
/// 将它们路由到 Rust 端的 open_external 命令，在系统浏览器中打开。
const EXTERNAL_LINK_INTERCEPT_JS: &str = r#"
(function() {
  if (window.__PTNEXUS_LINK_INTERCEPTOR__) return;
  window.__PTNEXUS_LINK_INTERCEPTOR__ = true;

  function isExternal(url) {
    try {
      var u = new URL(url, location.href);
      return u.hostname !== '127.0.0.1' && u.hostname !== 'localhost';
    } catch(e) { return false; }
  }

  // 拦截 window.open
  var origOpen = window.open;
  window.open = function(url) {
    if (url && isExternal(String(url))) {
      try {
        var u = new URL(String(url), location.href);
        window.__TAURI_INTERNALS__.invoke('open_external', { url: u.href });
      } catch(e) {}
      return null;
    }
    return origOpen.apply(this, arguments);
  };

  // 拦截 <a> 元素点击（含 target="_blank" 和普通外部链接）
  document.addEventListener('click', function(e) {
    var el = e.target;
    while (el && el.tagName !== 'A') el = el.parentElement;
    if (!el) return;
    var href = el.getAttribute('href') || el.href;
    if (!href) return;
    if (isExternal(href)) {
      e.preventDefault();
      e.stopPropagation();
      try {
        var u = new URL(href, location.href);
        window.__TAURI_INTERNALS__.invoke('open_external', { url: u.href });
      } catch(ex) {}
    }
  }, true);
})();
"#;

pub struct RuntimeManager {
    processes: Arc<Mutex<Vec<Child>>>,
}

impl RuntimeManager {
    pub fn bootstrap(app: &AppHandle) -> Result<Self, String> {
        ensure_ports_available(&[5274, 5275, 5276])?;

        let runtime_root = resolve_runtime_root(app)?;
        let changelog_path = resolve_changelog_path(app, &runtime_root);

        let data_dir = app
            .path()
            .app_data_dir()
            .map_err(|e| format!("解析应用数据目录失败: {e}"))?;
        fs::create_dir_all(data_dir.join("tmp")).map_err(|e| format!("创建应用数据目录失败: {e}"))?;
        let logs_dir = data_dir.join("logs");
        fs::create_dir_all(&logs_dir).map_err(|e| format!("创建日志目录失败: {e}"))?;

        // 首次运行时，把模板配置复制到用户可写目录，方便后续修改 DB/端口等运行参数。
        let bundled_env_example = if runtime_root.join("data").join("runtime.env.example").exists() {
            runtime_root.join("data").join("runtime.env.example")
        } else {
            runtime_root
                .join("_up_")
                .join("runtime")
                .join("data")
                .join("runtime.env.example")
        };
        let local_env_example = data_dir.join("runtime.env.example");
        if bundled_env_example.exists() && !local_env_example.exists() {
            let _ = fs::copy(&bundled_env_example, &local_env_example);
        }

        let updater_dir = runtime_root.join("updater");
        let batch_dir = runtime_root.join("batch");
        let server_dir = runtime_root.join("server");

        let updater_exe = updater_dir.join(exe_name("updater"));
        let batch_exe = batch_dir.join(exe_name("batch"));

        ensure_exists(&updater_exe)?;
        ensure_exists(&batch_exe)?;
        ensure_exists(&server_dir.join("dist").join("index.html"))?;

        let (server_program, server_args, server_workdir) = resolve_server_launcher(&server_dir)?;

        let mut processes = Vec::new();
        let mut common_env = build_runtime_env(&data_dir, &server_dir, &changelog_path);

        apply_host_env_overrides(
            &mut common_env,
            &[
                "DB_TYPE",
                "MYSQL_HOST",
                "MYSQL_PORT",
                "MYSQL_USER",
                "MYSQL_PASSWORD",
                "MYSQL_DATABASE",
                "POSTGRES_HOST",
                "POSTGRES_PORT",
                "POSTGRES_USER",
                "POSTGRES_PASSWORD",
                "POSTGRES_DATABASE",
                "SERVER_HOST",
                "SERVER_PORT",
                "BATCH_PORT",
                "UPDATER_PORT",
                "GO_SERVICE_URL",
                "CORE_API_URL",
            ],
        );
        merge_env_file(&mut common_env, &data_dir.join("runtime.env"))?;

        let server = spawn_process(
            &server_program,
            &server_workdir,
            &common_env,
            &server_args,
            "server",
            &logs_dir,
        )?;
        let mut server = server;
        wait_for_http_with_process_state(
            "server",
            &mut server,
            "127.0.0.1",
            5275,
            Duration::from_secs(30),
            &logs_dir,
        )?;
        processes.push(server);

        let batch = spawn_process(
            &batch_exe,
            &batch_dir,
            &common_env,
            &[],
            "batch",
            &logs_dir,
        )?;
        let mut batch = batch;
        wait_for_http_with_process_state(
            "batch",
            &mut batch,
            "127.0.0.1",
            5276,
            Duration::from_secs(30),
            &logs_dir,
        )?;
        processes.push(batch);

        let updater = spawn_process(
            &updater_exe,
            &updater_dir,
            &common_env,
            &[],
            "updater",
            &logs_dir,
        )?;
        let mut updater = updater;
        wait_for_http_with_process_state(
            "updater",
            &mut updater,
            "127.0.0.1",
            5274,
            Duration::from_secs(30),
            &logs_dir,
        )?;
        processes.push(updater);

        if let Some(window) = app.get_webview_window("main") {
            let _ = window.eval("window.location.replace('http://127.0.0.1:5274')");
            let _ = app.emit("runtime-ready", true);

            // 页面导航后注入外部链接拦截脚本
            inject_external_link_interceptor(window);
        }

        Ok(Self {
            processes: Arc::new(Mutex::new(processes)),
        })
    }

    pub fn shutdown_all(&self) {
        let mut children = match self.processes.lock() {
            Ok(guard) => guard,
            Err(_) => return,
        };

        for child in children.iter_mut() {
            let _ = child.kill();
        }
        children.clear();
    }
}

impl Drop for RuntimeManager {
    fn drop(&mut self) {
        self.shutdown_all();
    }
}

fn resolve_runtime_root(app: &AppHandle) -> Result<PathBuf, String> {
    let candidates = candidate_runtime_roots(app);
    for candidate in &candidates {
        if is_runtime_root(candidate) {
            return Ok(candidate.clone());
        }
    }

    let mut checked = String::new();
    for path in candidates {
        checked.push_str(&format!("\n- {}", path.display()));
    }

    Err(format!(
        "未找到可用运行目录。已检查: {checked}\n\n支持两种布局：\n1) <安装目录>/_up_/runtime/{{server,batch,updater}}\n2) <安装目录>/{{server,batch,updater}}"
    ))
}

fn candidate_runtime_roots(app: &AppHandle) -> Vec<PathBuf> {
    let mut candidates = Vec::new();

    if let Ok(exe_path) = std::env::current_exe() {
        if let Some(exe_dir) = exe_path.parent() {
            push_layout_candidates(&mut candidates, exe_dir);
            if let Some(parent_dir) = exe_dir.parent() {
                push_layout_candidates(&mut candidates, parent_dir);
            }
        }
    }

    if let Ok(exe_dir) = app.path().executable_dir() {
        push_layout_candidates(&mut candidates, &exe_dir);
        if let Some(parent_dir) = exe_dir.parent() {
            push_layout_candidates(&mut candidates, parent_dir);
        }
    }

    if let Ok(path) = app.path().resolve("runtime", BaseDirectory::Resource) {
        candidates.push(path.clone());
        if let Some(parent) = path.parent() {
            push_layout_candidates(&mut candidates, parent);
        }
    }
    if let Ok(path) = app.path().resolve("_up_/runtime", BaseDirectory::Resource) {
        candidates.push(path.clone());
        if let Some(parent) = path.parent() {
            push_layout_candidates(&mut candidates, parent);
        }
    }

    dedup_paths(candidates)
}

fn resolve_changelog_path(app: &AppHandle, runtime_root: &Path) -> PathBuf {
    let mut candidates = Vec::new();
    candidates.push(runtime_root.join("CHANGELOG.json"));
    if let Some(parent) = runtime_root.parent() {
        candidates.push(parent.join("CHANGELOG.json"));
    }

    if let Ok(exe_path) = std::env::current_exe() {
        if let Some(exe_dir) = exe_path.parent() {
            candidates.push(exe_dir.join("CHANGELOG.json"));
            candidates.push(exe_dir.join("_up_").join("CHANGELOG.json"));
            if let Some(parent_dir) = exe_dir.parent() {
                candidates.push(parent_dir.join("CHANGELOG.json"));
                candidates.push(parent_dir.join("_up_").join("CHANGELOG.json"));
            }
        }
    }

    if let Ok(exe_dir) = app.path().executable_dir() {
        candidates.push(exe_dir.join("CHANGELOG.json"));
        candidates.push(exe_dir.join("_up_").join("CHANGELOG.json"));
        if let Some(parent_dir) = exe_dir.parent() {
            candidates.push(parent_dir.join("CHANGELOG.json"));
            candidates.push(parent_dir.join("_up_").join("CHANGELOG.json"));
        }
    }

    if let Ok(path) = app.path().resolve("CHANGELOG.json", BaseDirectory::Resource) {
        candidates.push(path);
    }

    let candidates = dedup_paths(candidates);
    for candidate in &candidates {
        if candidate.exists() {
            return candidate.clone();
        }
    }

    candidates
        .into_iter()
        .next()
        .unwrap_or_else(|| PathBuf::from("CHANGELOG.json"))
}

fn dedup_paths(paths: Vec<PathBuf>) -> Vec<PathBuf> {
    let mut dedup = Vec::new();
    let mut seen = HashSet::new();

    for path in paths {
        let key = path.to_string_lossy().to_string();
        if seen.insert(key) {
            dedup.push(path);
        }
    }

    dedup
}

fn push_layout_candidates(candidates: &mut Vec<PathBuf>, base_dir: &Path) {
    // 优先支持扁平布局（用户更容易查看和编辑）
    candidates.push(base_dir.to_path_buf());
    candidates.push(base_dir.join("_up_"));
    // 再兼容旧的 runtime 子目录布局
    candidates.push(base_dir.join("runtime"));
    candidates.push(base_dir.join("_up_").join("runtime"));
}


fn is_runtime_root(root: &Path) -> bool {
    root.join("updater").join(exe_name("updater")).exists()
        && root.join("batch").join(exe_name("batch")).exists()
        && root.join("server").join("dist").join("index.html").exists()
}

fn resolve_server_launcher(server_dir: &Path) -> Result<(PathBuf, Vec<String>, PathBuf), String> {
    let server_exe = server_dir.join(exe_name("server"));
    if server_exe.exists() {
        return Ok((server_exe, vec![], server_dir.to_path_buf()));
    }

    let python_exe = server_dir.join("python").join(exe_name("python"));
    let app_entry = server_dir.join("app.py");

    if python_exe.exists() && app_entry.exists() {
        return Ok((
            python_exe,
            vec!["-u".to_string(), app_entry.to_string_lossy().to_string()],
            server_dir.to_path_buf(),
        ));
    }

    Err(format!(
        "未找到 server 启动入口：{} 或 {} + {}",
        server_exe.display(),
        python_exe.display(),
        app_entry.display()
    ))
}

fn build_runtime_env(
    data_dir: &Path,
    server_dir: &Path,
    changelog_path: &Path,
) -> HashMap<String, String> {
    let mut envs = HashMap::new();

    let server_host = "127.0.0.1";
    let server_port = "5275";
    let batch_port = "5276";
    let updater_port = "5274";

    let static_dir = server_dir.join("dist");
    let global_mappings = server_dir.join("configs").join("global_mappings.yaml");
    let sites_data = server_dir.join("sites_data.json");
    let bdinfo_dir = server_dir.join("bdinfo");

    envs.insert("DEV_ENV".to_string(), "false".to_string());
    envs.insert("FLASK_DEBUG".to_string(), "false".to_string());
    envs.insert("PYTHONUTF8".to_string(), "1".to_string());
    envs.insert(
        "PYTHONPATH".to_string(),
        server_dir.to_string_lossy().to_string(),
    );
    envs.insert("SERVER_HOST".to_string(), server_host.to_string());
    envs.insert("SERVER_PORT".to_string(), server_port.to_string());

    envs.insert("UPDATER_PORT".to_string(), updater_port.to_string());
    envs.insert("BATCH_PORT".to_string(), batch_port.to_string());
    envs.insert("BATCH_ENHANCER_PORT".to_string(), batch_port.to_string());

    envs.insert(
        "GO_SERVICE_URL".to_string(),
        format!("http://{server_host}:{batch_port}"),
    );
    envs.insert(
        "CORE_API_URL".to_string(),
        format!("http://{server_host}:{server_port}"),
    );

    envs.insert(
        "PTNEXUS_BASE_DIR".to_string(),
        server_dir.to_string_lossy().to_string(),
    );
    envs.insert(
        "PTNEXUS_DATA_DIR".to_string(),
        data_dir.to_string_lossy().to_string(),
    );
    envs.insert(
        "PTNEXUS_STATIC_DIR".to_string(),
        static_dir.to_string_lossy().to_string(),
    );
    envs.insert(
        "PTNEXUS_GLOBAL_MAPPINGS".to_string(),
        global_mappings.to_string_lossy().to_string(),
    );
    envs.insert(
        "PTNEXUS_SITES_DATA_FILE".to_string(),
        sites_data.to_string_lossy().to_string(),
    );
    envs.insert(
        "PTNEXUS_BDINFO_DIR".to_string(),
        bdinfo_dir.to_string_lossy().to_string(),
    );
    envs.insert(
        "PTNEXUS_BDINFO_PATH".to_string(),
        bdinfo_dir.join(exe_name("BDInfo")).to_string_lossy().to_string(),
    );

    envs.insert(
        "TEMP_DIR".to_string(),
        data_dir.join("tmp").to_string_lossy().to_string(),
    );
    envs.insert(
        "CONFIG_FILE".to_string(),
        data_dir.join("config.json").to_string_lossy().to_string(),
    );

    envs.insert(
        "UPDATE_DIR".to_string(),
        data_dir.join("updates").to_string_lossy().to_string(),
    );
    envs.insert(
        "REPO_DIR".to_string(),
        data_dir
            .join("updates")
            .join("repo")
            .to_string_lossy()
            .to_string(),
    );
    envs.insert(
        "LOCAL_CONFIG_FILE".to_string(),
        changelog_path.to_string_lossy().to_string(),
    );

    envs
}

fn apply_host_env_overrides(envs: &mut HashMap<String, String>, keys: &[&str]) {
    for key in keys {
        if let Ok(value) = std::env::var(key) {
            if !value.trim().is_empty() {
                envs.insert((*key).to_string(), value);
            }
        }
    }
}

fn merge_env_file(envs: &mut HashMap<String, String>, env_file: &Path) -> Result<(), String> {
    if !env_file.exists() {
        return Ok(());
    }

    let content = fs::read_to_string(env_file)
        .map_err(|e| format!("读取 runtime.env 失败 ({}): {e}", env_file.display()))?;

    for (index, raw_line) in content.lines().enumerate() {
        let line = raw_line.trim();
        if line.is_empty() || line.starts_with('#') {
            continue;
        }

        let Some((key, raw_value)) = line.split_once('=') else {
            return Err(format!(
                "runtime.env 第 {} 行格式错误，期望 KEY=VALUE",
                index + 1
            ));
        };

        let key = key.trim();
        if key.is_empty() {
            return Err(format!("runtime.env 第 {} 行键名为空", index + 1));
        }

        let mut value = raw_value.trim().to_string();
        if (value.starts_with('"') && value.ends_with('"'))
            || (value.starts_with('\'') && value.ends_with('\''))
        {
            if value.len() >= 2 {
                value = value[1..value.len() - 1].to_string();
            }
        }

        envs.insert(key.to_string(), value);
    }

    Ok(())
}

fn spawn_process(
    executable: &Path,
    working_dir: &Path,
    envs: &HashMap<String, String>,
    args: &[String],
    process_name: &str,
    logs_dir: &Path,
) -> Result<Child, String> {
    let stdout_log = logs_dir.join(format!("{process_name}.stdout.log"));
    let stderr_log = logs_dir.join(format!("{process_name}.stderr.log"));

    let stdout_file = OpenOptions::new()
        .create(true)
        .append(true)
        .open(&stdout_log)
        .map_err(|e| format!("打开日志文件失败 {}: {e}", stdout_log.display()))?;

    let stderr_file = OpenOptions::new()
        .create(true)
        .append(true)
        .open(&stderr_log)
        .map_err(|e| format!("打开日志文件失败 {}: {e}", stderr_log.display()))?;

    let mut cmd = Command::new(executable);
    cmd.args(args)
        .current_dir(working_dir)
        .stdin(Stdio::null())
        .stdout(Stdio::from(stdout_file))
        .stderr(Stdio::from(stderr_file));

    // Windows 上隐藏子进程的终端窗口，避免弹出三个黑框
    #[cfg(target_os = "windows")]
    {
        const CREATE_NO_WINDOW: u32 = 0x08000000;
        cmd.creation_flags(CREATE_NO_WINDOW);
    }

    for (key, value) in envs {
        cmd.env(key, value);
    }

    cmd.spawn()
        .map_err(|e| format!("启动进程失败 {}: {e}", executable.display()))
}


fn ensure_ports_available(ports: &[u16]) -> Result<(), String> {
    for port in ports {
        if TcpListener::bind(("127.0.0.1", *port)).is_err() {
            return Err(format!("端口 {port} 被占用，请先释放后再启动应用。"));
        }
    }
    Ok(())
}

fn wait_for_http_with_process_state(
    process_name: &str,
    child: &mut Child,
    host: &str,
    port: u16,
    timeout: Duration,
    logs_dir: &Path,
) -> Result<(), String> {
    let begin = Instant::now();
    let stdout_log = logs_dir.join(format!("{process_name}.stdout.log"));
    let stderr_log = logs_dir.join(format!("{process_name}.stderr.log"));

    loop {
        if std::net::TcpStream::connect((host, port)).is_ok() {
            return Ok(());
        }

        match child.try_wait() {
            Ok(Some(status)) => {
                let stderr_tail = read_log_tail(&stderr_log, 40);
                if stderr_tail.is_empty() {
                    return Err(format!(
                        "进程 {process_name} 已退出（状态: {status}），服务 {host}:{port} 未就绪。\n请查看日志：{}",
                        stderr_log.display()
                    ));
                }

                return Err(format!(
                    "进程 {process_name} 已退出（状态: {status}），服务 {host}:{port} 未就绪。\n日志：{}\n\n最近 stderr 输出：\n{stderr_tail}",
                    stderr_log.display()
                ));
            }
            Ok(None) => {}
            Err(e) => {
                return Err(format!(
                    "检查进程 {process_name} 运行状态失败: {e}。\n请查看日志：{}, {}",
                    stdout_log.display(),
                    stderr_log.display()
                ));
            }
        }

        if begin.elapsed() > timeout {
            return Err(format!(
                "等待服务 {host}:{port} 超时。\n请查看日志：{}, {}",
                stdout_log.display(),
                stderr_log.display()
            ));
        }

        thread::sleep(Duration::from_millis(250));
    }
}

fn read_log_tail(path: &Path, max_lines: usize) -> String {
    let Ok(content) = fs::read_to_string(path) else {
        return String::new();
    };

    let mut lines: Vec<&str> = content.lines().rev().take(max_lines).collect();
    lines.reverse();
    lines.join("\n")
}

/// 在新页面加载完成后注入外部链接拦截 JS。
/// 因为 window.location.replace 会销毁当前页面上下文，所以需要等待新页面加载完成后再注入。
fn inject_external_link_interceptor(window: WebviewWindow) {
    thread::spawn(move || {
        // 等待新页面加载完成（SPA 首次渲染通常需要几秒）
        thread::sleep(Duration::from_secs(3));
        let _ = window.eval(EXTERNAL_LINK_INTERCEPT_JS);
    });
}

fn ensure_exists(path: &Path) -> Result<(), String> {
    if path.exists() {
        return Ok(());
    }
    Err(format!("缺少运行文件: {}", path.display()))
}

fn exe_name(name: &str) -> String {
    if cfg!(target_os = "windows") {
        format!("{name}.exe")
    } else {
        name.to_string()
    }
}
