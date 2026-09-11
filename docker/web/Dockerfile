# syntax=docker/dockerfile:1
# 全景相册前端镜像：nginx 托管 vite 构建产物 + API 同源反代
# 构建上下文为 src/frontend（须先 npm run build 生成 dist/）
# 反代路径与 src/backend/cmd/api/main.go 注册路由保持一致；
# 前端经同源相对路径调用 API（src/api/http.js baseURL 为空串），真机经隧道访问时无需感知后端地址
#
# 路径冲突处理：前端 SPA 路由 /search /albums /spaces /folders 与 API 同前缀。
# 判别规则——浏览器页面导航（GET 且 Accept 含 text/html）回退 SPA index.html；
# 其余请求（axios: application/json、fetch/hls.js/img: */*、curl）一律透传 API。

FROM nginx:1.27-alpine

COPY dist /usr/share/nginx/html

COPY <<'EOF' /etc/nginx/conf.d/default.conf
# 浏览器导航判别：GET 且 Accept 含 text/html → 视为页面访问
map $request_method:$http_accept $spa_nav {
    default 0;
    ~*^GET:.*text/html 1;
}

server {
    listen 80;
    server_name _;
    root /usr/share/nginx/html;
    index index.html;

    gzip on;
    gzip_types text/css application/javascript application/json image/svg+xml;
    gzip_min_length 1k;

    # 分块上传（Stage 2 实测 9MB 块），HLS 大响应放宽超时
    client_max_body_size 100m;

    # 与 SPA 路由冲突的 API 前缀：导航回退 SPA，API 调用透传后端
    location ~ ^/(search|albums|spaces|folders)(/|$) {
        error_page 418 = @spa;
        recursive_error_pages on;
        if ($spa_nav) { return 418; }
        proxy_pass http://api:8080;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_read_timeout 300s;
        proxy_send_timeout 300s;
    }

    # 纯 API 路由（URI 原样透传，无路径剥离）
    location ~ ^/(auth|media|transcode|shares|public|admin|health|ready|metrics|tags|geo|tiles|preferences)(/|$) {
        proxy_pass http://api:8080;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_read_timeout 300s;
        proxy_send_timeout 300s;
    }

    # maplibre worker（.mjs 必须可执行 MIME，否则 module worker 被浏览器拒绝 → GeoJSON 图层静默不渲染）
    location ~ \.mjs$ {
        types { }
        default_type application/javascript;
        try_files $uri =404;
    }

    # SPA 页面回退目标
    location @spa {
        try_files /index.html =404;
    }

    # Vue SPA history 路由回退（/timeline /player/:id /share/:token /login 等）
    location / {
        try_files $uri $uri/ /index.html;
    }
}
EOF
