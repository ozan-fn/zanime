use actix_web::{web, HttpServer};

use zanime::{config::Config, upstream};

/// Log mem + cpu proses tiap 2 detik. Refresh ditarget ke pid sendiri biar murah.
fn spawn_resource_log() {
    actix_web::rt::spawn(async move {
        let Ok(pid) = sysinfo::get_current_pid() else { return };            let mut sys = sysinfo::System::new();
            let mut tick = tokio::time::interval(std::time::Duration::from_secs(2));
            loop {
            tick.tick().await;
            sys.refresh_processes_specifics(
                sysinfo::ProcessesToUpdate::Some(&[pid]),
                true,
                sysinfo::ProcessRefreshKind::nothing().with_cpu().with_memory(),
            );
            sys.refresh_memory();
            if let Some(p) = sys.process(pid) {
                // Nilai yang sama dipakai navbar (/api/stats); puncak dilacak di sana.
                zanime::stats::set(p.memory(), p.cpu_usage().into());
                let (mem, peak_mem, cpu, peak_cpu) = zanime::stats::snapshot();
                log::info!("res proc_mem={mem}MB peak_mem={peak_mem}MB proc_cpu={cpu:.1}% peak_cpu={peak_cpu:.1}%");
            }
        }
    });
}
#[actix_web::main]
async fn main() -> std::io::Result<()> {
    env_logger::Builder::from_env(env_logger::Env::default().default_filter_or("info")).init();
    spawn_resource_log();
    let cfg = Config::default();
    zanime::cache::init(&cfg.redis_url).await;
    let http = upstream::client(&cfg).map_err(|e| std::io::Error::other(e.to_string()))?;
    let subjobs = web::Data::new(upstream::SubJobs::new());
    let (host, port) = cfg.bind.clone();
    log::info!("listen http://{host}:{port}");
    HttpServer::new(move || zanime::create_app(cfg.clone(), http.clone(), subjobs.clone()).wrap(actix_cors::Cors::permissive()))
        .bind((host.as_str(), port))?
        .run()
        .await
}
