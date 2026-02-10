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

/// 注入“打开数据库配置目录”按钮到设置页的“背景设置/其他设置”卡片。
/// 仅桌面端运行时注入，不修改 webui 源码。
const INJECT_DB_CONFIG_BUTTON_JS: &str = r#"
(function() {
  if (window.__PTNEXUS_DB_BUTTON_WATCHER__) return;
  window.__PTNEXUS_DB_BUTTON_WATCHER__ = true;

  function ensureButton() {
    var headers = Array.from(document.querySelectorAll('.card-header .header-content h3'));
    var target = headers.find(function(h) {
      var t = (h.textContent || '').trim();
      return t === '其他设置';
    });
    if (!target) return false;

    var card = target.closest('.settings-card');
    if (!card) return false;

    if (card.querySelector('.ptnexus-open-db-config-btn')) return true;

    var form = card.querySelector('.settings-form');
    if (!form) return false;

    var formItem = document.createElement('div');
    formItem.className = 'el-form-item form-item ptnexus-db-config-item';

    var label = document.createElement('label');
    label.className = 'el-form-item__label';
    label.textContent = '数据库配置文件';

    var content = document.createElement('div');
    content.className = 'el-form-item__content';

    var btn = document.createElement('button');
    btn.type = 'button';
    btn.className = 'el-button el-button--success is-plain ptnexus-open-db-config-btn';
    btn.innerHTML = '<span>打开数据库配置目录</span>';
    btn.addEventListener('click', function() {
      try {
        window.__TAURI_INTERNALS__.invoke('open_app_data_dir');
      } catch (e) {
        alert('打开目录失败，请手动前往应用数据目录修改 runtime.env。');
      }
    });

    var hint = document.createElement('div');
    hint.className = 'password-hint';
    hint.innerHTML = '<span class="el-text el-text--small is-info">默认 sqlite；如需 MySQL/PostgreSQL，请编辑 runtime.env 后重启应用。</span>';

    content.appendChild(btn);
    content.appendChild(hint);
    formItem.appendChild(label);
    formItem.appendChild(content);

    var spacer = form.querySelector('.form-spacer');
    if (spacer && spacer.parentNode) {
      spacer.parentNode.insertBefore(formItem, spacer);
    } else {
      form.appendChild(formItem);
    }

    return true;
  }

  function startObserver() {
    var observer = new MutationObserver(function() {
      ensureButton();
    });
    observer.observe(document.documentElement || document.body, {
      childList: true,
      subtree: true,
    });

    window.addEventListener('hashchange', ensureButton, true);
    window.addEventListener('popstate', ensureButton, true);

    var originalPushState = history.pushState;
    var originalReplaceState = history.replaceState;
    history.pushState = function() {
      var ret = originalPushState.apply(this, arguments);
      setTimeout(ensureButton, 0);
      return ret;
    };
    history.replaceState = function() {
      var ret = originalReplaceState.apply(this, arguments);
      setTimeout(ensureButton, 0);
      return ret;
    };

    setInterval(ensureButton, 1500);
  }

  var warmupTries = 0;
  var warmupTimer = setInterval(function() {
    warmupTries += 1;
    var ok = ensureButton();
    if (ok || warmupTries > 80) {
      clearInterval(warmupTimer);
      startObserver();
      ensureButton();
    }
  }, 250);
})();
"#;

/// 注入启动遮罩，尽可能覆盖 WebUI 初始化阶段，减少白屏观感。
const STARTUP_OVERLAY_JS: &str = r#"
(function() {
  if (window.__PTNEXUS_STARTUP_OVERLAY__) return;
  window.__PTNEXUS_STARTUP_OVERLAY__ = true;

  function removeOverlay() {
    var overlay = document.getElementById('ptnexus-startup-overlay');
    if (overlay && overlay.parentNode) overlay.parentNode.removeChild(overlay);
    var style = document.getElementById('ptnexus-startup-style');
    if (style && style.parentNode) style.parentNode.removeChild(style);
  }

  function appReady() {
    var app = document.getElementById('app');
    if (!app) return false;
    // 登录页已渲染
    if (app.querySelector('.login-page, .login-card')) return true;
    // 主应用页已渲染
    if (app.querySelector('.app-container, .layout-container, .main-container, .home-container, .main-nav')) return true;
    return false;
  }

  function createOverlay() {
    if (document.getElementById('ptnexus-startup-overlay')) return;
    // 如果 Vue 应用已经渲染了有效内容，不再创建遮罩
    if (appReady()) return;

    var style = document.createElement('style');
    style.id = 'ptnexus-startup-style';
    style.textContent = [
      '#ptnexus-startup-overlay{position:fixed;inset:0;z-index:2147483647;display:flex;align-items:center;justify-content:center;background:#f5f7fa;color:#303133;font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;}',
      '#ptnexus-startup-overlay .box{text-align:center;padding:28px 32px;border-radius:14px;box-shadow:0 8px 32px rgba(0,0,0,.08);background:#fff;min-width:320px;}',
      '#ptnexus-startup-overlay .title{font-size:22px;font-weight:600;margin-bottom:10px;}',
      '#ptnexus-startup-overlay .desc{font-size:14px;color:#606266;}',
      '#ptnexus-startup-overlay .dot::after{content:"";display:inline-block;animation:ptnexus-dot 1.2s steps(3,end) infinite;}',
      '@keyframes ptnexus-dot{0%{content:""}33%{content:"."}66%{content:".."}100%{content:"..."}}'
    ].join('');
    document.head && document.head.appendChild(style);

    var overlay = document.createElement('div');
    overlay.id = 'ptnexus-startup-overlay';
    overlay.innerHTML = '<div class="box"><div class="title">PT Nexus 启动中<span class="dot"></span></div><div class="desc">正在初始化页面，请稍候。</div></div>';
    (document.body || document.documentElement).appendChild(overlay);
  }

  function tick() {
    if (appReady()) {
      removeOverlay();
      clearInterval(timer);
    } else {
      createOverlay();
    }
  }

  var timer = setInterval(tick, 250);
  tick();
  setTimeout(function(){ clearInterval(timer); removeOverlay(); }, 20000);
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
        let local_runtime_env = data_dir.join("runtime.env");

        if bundled_env_example.exists() && !local_env_example.exists() {
            let _ = fs::copy(&bundled_env_example, &local_env_example);
        }

        // 用户目录若不存在 runtime.env，则直接从模板创建一份可编辑配置。
        if bundled_env_example.exists() && !local_runtime_env.exists() {
            let _ = fs::copy(&bundled_env_example, &local_runtime_env);
        }

        let updater_dir = runtime_root.join("updater");
        let batch_dir = runtime_root.join("batch");
        let server_dir = runtime_root.join("server");

        let updater_exe = updater_dir.join(exe_name("updater"));
        let batch_exe = batch_dir.join(exe_name("batch"));

        ensure_exists(&updater_exe)?;
        ensure_exists(&batch_exe)?;
        ensure_exists(&server_dir.join("dist").join("index.html"))?;

        let (background_runner_program, background_runner_args, background_runner_workdir) =
            resolve_background_runner_launcher(&server_dir)?;
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
                "PTNEXUS_EMBED_BG_IN_APP",
            ],
        );
        merge_env_file(&mut common_env, &data_dir.join("runtime.env"))?;

        let background_runner = spawn_process(
            &background_runner_program,
            &background_runner_workdir,
            &common_env,
            &background_runner_args,
            "background_runner",
            &logs_dir,
        )?;
        let mut background_runner = background_runner;
        wait_for_process_running(
            "background_runner",
            &mut background_runner,
            Duration::from_secs(10),
            &logs_dir,
        )?;
        processes.push(background_runner);

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
            inject_external_link_interceptor(&window);
            inject_startup_overlay(&window);
            inject_db_config_button(&window);
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

fn resolve_background_runner_launcher(
    server_dir: &Path,
) -> Result<(PathBuf, Vec<String>, PathBuf), String> {
    let runner_exe = server_dir.join(exe_name("background_runner"));
    if runner_exe.exists() {
        return Ok((runner_exe, vec![], server_dir.to_path_buf()));
    }

    let python_exe = server_dir.join("python").join(exe_name("python"));
    let entry = server_dir.join("background_runner.py");

    if python_exe.exists() && entry.exists() {
        return Ok((
            python_exe,
            vec!["-u".to_string(), entry.to_string_lossy().to_string()],
            server_dir.to_path_buf(),
        ));
    }

    Err(format!(
        "未找到 background_runner 启动入口：{} 或 {} + {}",
        runner_exe.display(),
        python_exe.display(),
        entry.display()
    ))
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
    let bdinfo_dir = server_dir.join("core").join("bdinfo");
    let bdinfo_windows_dir = bdinfo_dir.join("windows");
    let media_tools_dir = server_dir.join("tools");
    let mpv_dir = media_tools_dir.join("mpv");
    let ffmpeg_dir = media_tools_dir.join("ffmpeg").join("bin");
    let mpv_path = mpv_dir.join(exe_name("mpv"));
    let ffmpeg_path = ffmpeg_dir.join(exe_name("ffmpeg"));
    let ffprobe_path = ffmpeg_dir.join(exe_name("ffprobe"));

    envs.insert("DEV_ENV".to_string(), "false".to_string());
    envs.insert("FLASK_DEBUG".to_string(), "false".to_string());
    envs.insert("PTNEXUS_EMBED_BG_IN_APP".to_string(), "false".to_string());
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
        bdinfo_windows_dir
            .join(exe_name("BDInfo"))
            .to_string_lossy()
            .to_string(),
    );
    envs.insert(
        "PTNEXUS_BDINFO_SUBSTRACTOR_PATH".to_string(),
        bdinfo_windows_dir
            .join(exe_name("BDInfoDataSubstractor"))
            .to_string_lossy()
            .to_string(),
    );
    envs.insert(
        "PTNEXUS_MPV_PATH".to_string(),
        mpv_path.to_string_lossy().to_string(),
    );
    envs.insert(
        "PTNEXUS_FFMPEG_PATH".to_string(),
        ffmpeg_path.to_string_lossy().to_string(),
    );
    envs.insert(
        "PTNEXUS_FFPROBE_PATH".to_string(),
        ffprobe_path.to_string_lossy().to_string(),
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

    let path_separator = if cfg!(target_os = "windows") { ";" } else { ":" };
    let mut path_entries = vec![
        mpv_dir.to_string_lossy().to_string(),
        ffmpeg_dir.to_string_lossy().to_string(),
    ];
    if let Ok(existing_path) = std::env::var("PATH") {
        if !existing_path.trim().is_empty() {
            path_entries.push(existing_path);
        }
    }
    envs.insert("PATH".to_string(), path_entries.join(path_separator));

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

fn wait_for_process_running(
    process_name: &str,
    child: &mut Child,
    timeout: Duration,
    logs_dir: &Path,
) -> Result<(), String> {
    let begin = Instant::now();
    let stdout_log = logs_dir.join(format!("{process_name}.stdout.log"));
    let stderr_log = logs_dir.join(format!("{process_name}.stderr.log"));

    loop {
        match child.try_wait() {
            Ok(Some(status)) => {
                let stderr_tail = read_log_tail(&stderr_log, 40);
                if stderr_tail.is_empty() {
                    return Err(format!(
                        "进程 {process_name} 已退出（状态: {status}），未能保持运行。
请查看日志：{}",
                        stderr_log.display()
                    ));
                }
                return Err(format!(
                    "进程 {process_name} 已退出（状态: {status}），未能保持运行。
日志：{}

最近 stderr 输出：
{stderr_tail}",
                    stderr_log.display()
                ));
            }
            Ok(None) => {
                if begin.elapsed() >= timeout {
                    return Ok(());
                }
            }
            Err(e) => {
                return Err(format!(
                    "检查进程 {process_name} 运行状态失败: {e}。
请查看日志：{}, {}",
                    stdout_log.display(),
                    stderr_log.display()
                ));
            }
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
fn inject_external_link_interceptor(window: &WebviewWindow) {
    let window = window.clone();
    thread::spawn(move || {
        // 等待新页面加载完成（SPA 首次渲染通常需要几秒）
        thread::sleep(Duration::from_secs(3));
        let _ = window.eval(EXTERNAL_LINK_INTERCEPT_JS);
    });
}

fn inject_db_config_button(window: &WebviewWindow) {
    let window = window.clone();
    thread::spawn(move || {
        thread::sleep(Duration::from_secs(4));
        let _ = window.eval(INJECT_DB_CONFIG_BUTTON_JS);
    });
}

fn inject_startup_overlay(window: &WebviewWindow) {
    let window = window.clone();
    thread::spawn(move || {
        // 导航到业务页后立即尝试注入；若尚未就绪，脚本内部会自行重试。
        thread::sleep(Duration::from_millis(600));
        let _ = window.eval(STARTUP_OVERLAY_JS);
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
