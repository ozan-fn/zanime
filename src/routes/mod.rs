use actix_web::web;

use crate::{
    handlers::{anilist, anime, catalog, comments, episode_meta, episodes, fetch, hls, search, servers, skiptimes, sources, stats, stream, subid_batch, subid_result, subid_status},
    spa,
};

pub fn configure(cfg: &mut web::ServiceConfig) {
    cfg.service(
        web::scope("/api")
            .service(search)
            .service(catalog)
            .service(anime)
            .service(episodes)
            .service(servers)
            .service(sources)
            .service(stream)
            .service(skiptimes)
            .service(comments)
            .service(episode_meta)
            .service(anilist)
            .service(stats)
            .service(subid_batch)
            .service(subid_status)
            .service(subid_result)
            // dua bentuk: tanpa ekstensi (gambar/sub) dan dengan nama+ekstensi (segmen).
            .service(web::resource("/fetch").route(web::get().to(fetch)))
            .service(web::resource("/fetch/{name}").route(web::get().to(fetch)))
            .service(hls),
    );
    cfg.route("/{filename:.*}", web::get().to(spa::index));
    cfg.route("/{filename:.*}", web::head().to(spa::index));
}
