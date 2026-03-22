use axum::{
    body::Bytes,
    extract::State,
    http::{HeaderMap, HeaderValue, StatusCode},
    response::IntoResponse,
    routing::post,
    Router,
};
use reqwest::Client;
use serde_json::{json, Value};
use std::{env, sync::Arc};

#[derive(Clone)]
struct AppState {
    daemon_url: String,
    client: Client,
}

#[tokio::main]
async fn main() {
    let daemon_url = env::var("CLERM_DAEMON_URL").unwrap_or_else(|_| "http://127.0.0.1:8181".to_string());
    let port = env::var("PORT").unwrap_or_else(|_| "8585".to_string());
    let state = Arc::new(AppState { daemon_url, client: Client::new() });

    let app = Router::new().route("/api", post(handle_api)).with_state(state);
    let listener = tokio::net::TcpListener::bind(format!("127.0.0.1:{port}")).await.unwrap();
    println!("rust sample listening on 127.0.0.1:{port}");
    axum::serve(listener, app).await.unwrap();
}

async fn handle_api(State(state): State<Arc<AppState>>, headers: HeaderMap, body: Bytes) -> impl IntoResponse {
    let content_type = headers
        .get("content-type")
        .and_then(|value| value.to_str().ok())
        .unwrap_or("")
        .split(';')
        .next()
        .unwrap_or("")
        .trim()
        .to_string();
    if content_type != "application/clerm" {
        return (StatusCode::UNSUPPORTED_MEDIA_TYPE, "expected Content-Type: application/clerm").into_response();
    }
    let target = headers
        .get("clerm-target")
        .and_then(|value| value.to_str().ok())
        .unwrap_or("internal.search")
        .to_string();

    let command: Value = match state
        .client
        .post(format!("{}/v1/requests/decode", state.daemon_url))
        .header("Content-Type", "application/clerm")
        .header("Clerm-Target", target)
        .body(body.to_vec())
        .send()
        .await
    {
        Ok(response) => match response.json().await {
            Ok(value) => value,
            Err(error) => return (StatusCode::BAD_GATEWAY, error.to_string()).into_response(),
        },
        Err(error) => return (StatusCode::BAD_GATEWAY, error.to_string()).into_response(),
    };

    let method = command.get("method").and_then(|value| value.as_str()).unwrap_or("");
    let outputs = match method {
        "@global.healthcare.search_providers.v1" => json!({
            "request_id": "123e4567-e89b-12d3-a456-426614174000",
            "providers": [{"id": "provider-1", "name": "Cardio Clinic"}]
        }),
        "@verified.healthcare.book_visit.v1" => json!({
            "order_id": "visit-001",
            "status": "confirmed"
        }),
        _ => return (StatusCode::NOT_FOUND, format!("no handler for {method}")).into_response(),
    };

    match state
        .client
        .post(format!("{}/v1/responses/encode", state.daemon_url))
        .header("Content-Type", "application/json")
        .json(&json!({"method": method, "outputs": outputs}))
        .send()
        .await
    {
        Ok(response) => {
            let status = response.status();
            let bytes = match response.bytes().await {
                Ok(value) => value,
                Err(error) => return (StatusCode::BAD_GATEWAY, error.to_string()).into_response(),
            };
            let mut out_headers = HeaderMap::new();
            out_headers.insert("Content-Type", HeaderValue::from_static("application/clerm"));
            out_headers.insert("Clerm-Method", HeaderValue::from_str(method).unwrap());
            (status, out_headers, bytes).into_response()
        }
        Err(error) => (StatusCode::BAD_GATEWAY, error.to_string()).into_response(),
    }
}
