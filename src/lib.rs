pub mod cache;
pub mod config;
pub mod error;
pub mod handlers;
pub mod models;
pub mod routes;
pub mod spa;
pub mod stats;
pub mod upstream;

use actix_web::{
    dev::{ServiceFactory, ServiceRequest, ServiceResponse},
    web, App, Error,
};

pub fn create_app(
    cfg: config::Config,
    http: reqwest::Client,
    subjobs: web::Data<upstream::SubJobs>,
) -> App<impl ServiceFactory<ServiceRequest, Config = (), Response = ServiceResponse, Error = Error, InitError = ()>> {
    App::new()
        .app_data(web::Data::new(cfg))
        .app_data(web::Data::new(http))
        .app_data(subjobs)
        .configure(routes::configure)
}
