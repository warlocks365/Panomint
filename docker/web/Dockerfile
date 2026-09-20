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

# 社交/搜索抓取器判别：它们不执行 JS，只看初始 HTML → 需要服务端渲染 OG 标签；
# 普通浏览器仍走 SPA（行为不变）。
#
# 注意：微信**内置浏览器（真人）**的 UA 同样含 MicroMessenger，会被本规则命中 →
# 真人拿到 OG 页。解决办法不是把 MicroMessenger 从名单里删掉（那会一起丢掉微信卡片
# 能力：实测抓取器正是命中本规则才出的卡片），而是让 OG 页自带 JS 跳转，真人再带
# ?spa=1 回到 @spa 拿 SPA —— 见下面 /share/ location 的「$arg_spa 必须在前」。
map $http_user_agent $og_bot {
    default 0;
    ~*(MicroMessenger|WeChat|Weixin|Twitterbot|facebookexternalhit|Facebot|TelegramBot|Slackbot|LinkedInBot|WhatsApp|Discordbot|Googlebot|bingbot|Applebot|Pinterest|SkypeUriPreview|redditbot|Embedly) 1;
}

# 上游协议透传：web 容器只监听 80，若直接用 $scheme 会把 Caddy/cloudflared 传来的 https
# 覆盖成 http → 分享链接与 og:image 会变成 http://（微信抓图可能失败）。
# 故优先取上游的 X-Forwarded-Proto，缺省才回落 $scheme。
map $http_x_forwarded_proto $fwd_proto {
    default $http_x_forwarded_proto;
    ""      $scheme;
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
    # people / tags 既是前端路由（/people、/tags）又是 API 前缀（GET /people、GET /tags），
    # 故与 /albums 等同样处理：浏览器导航回退 SPA，axios（Accept: application/json）透传 API。
    # map：与 /people、/tags 同理 —— 前端路由 /map（MapView）与 API 前缀
    # /map/search、/map/providers 冲突，故进本组而非纯 API 组
    # admin：Job000052 管理后台 —— 前端路由 /admin（AdminView）与 API 前缀 /admin/* 冲突，
    # 同样进本组；漏配的话浏览器打开 /admin 会拿到 API 的 401 JSON 而不是页面
    location ~ ^/(search|albums|spaces|folders|people|tags|map|admin)(/|$) {
        error_page 418 = @spa;
        recursive_error_pages on;
        if ($spa_nav) { return 418; }
        proxy_pass http://api:8080;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $fwd_proto;
        proxy_read_timeout 300s;
        proxy_send_timeout 300s;
    }

    # 纯 API 路由（URI 原样透传，无路径剥离）
    # ai：/ai/tags 与 /ai/faces（Job000010 Phase 4 零样本打标与人脸扫描触发）
    # bandwidth：/bandwidth、/bandwidth/probe、/bandwidth/self-test（Phase 4 P1 带宽自测）
    # compute-nodes：T6.2 算力节点。⚠️ 节点侧 /compute-nodes/agent/* 也在同一前缀下，
    #   必须整体透传（否则 POST 落到静态目录 → nginx 返回 405，实测踩过）
    # tags / map / admin 不在此列——它们同时是前端路由，已移入上面的导航判别组
    # dav：Job000055 WebDAV（PROPFIND/MKCOL/MOVE/LOCK 等扩展方法整体透传；Basic 认证不经 JWT，
    #   由后端自认证；漏配则 nginx 405 或落 SPA 回退）
    location ~ ^/(auth|media|transcode|shares|public|health|ready|metrics|geo|tiles|preferences|user|ai|bandwidth|compute-nodes|dav)(/|$) {
        proxy_pass http://api:8080;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $fwd_proto;
        proxy_read_timeout 300s;
        proxy_send_timeout 300s;
    }

    # PWA：Service Worker 必须 no-cache（否则旧 SW 长期驻留，升级不生效）
    location = /sw.js {
        add_header Cache-Control "no-cache, no-store, must-revalidate";
        try_files $uri =404;
    }

    # PWA：Web App Manifest 需正确 MIME（application/manifest+json）
    location = /manifest.webmanifest {
        types { }
        default_type application/manifest+json;
        try_files $uri =404;
    }

    # maplibre worker（.mjs 必须可执行 MIME，否则 module worker 被浏览器拒绝 → GeoJSON 图层静默不渲染）
    location ~ \.mjs$ {
        types { }
        default_type application/javascript;
        try_files $uri =404;
    }

    # 分享页 /share/<token>：三个出口，优先次序即正确性
    #   ① ?spa=1 —— OG 中转页里的 JS 跳回来的第二次请求 → 强制 SPA（能播放）
    #   ② 抓取器 UA → 服务端渲染的 OG HTML（保卡片；抓取器不执行 JS，不会被脚本带走）
    #   ③ 其余 → SPA
    #
    # ⚠️ 两条 if 的**书写顺序是功能正确性的一部分，不是风格问题**：nginx 的 if 属于
    #    ngx_http_rewrite_module，在 rewrite 阶段**按配置里出现的先后依次求值**，命中即
    #    执行 return 并终结该请求。$arg_spa 必须写在 $og_bot 之前：
    #    微信内置浏览器（真人）UA 含 MicroMessenger，必然命中 $og_bot；若 $og_bot 在前，
    #    它带着 ?spa=1 回来时会被再次判成抓取器 → 又发 OG 页 → 而 OG 页的脚本见 search
    #    里已有 spa=1 便不再跳转 → 用户被永久困在极简文字页（正是本次要修的 bug 的加强版）。
    #
    # 借用本文件既有的 error_page 写法，避开 nginx 对「location 内 if + proxy_pass」的告警。
    location ~ ^/share/(?<share_token>[A-Za-z0-9_-]+)/?$ {
        error_page 418 = @share_og;
        error_page 419 = @spa;
        recursive_error_pages on;
        if ($arg_spa) { return 419; }   # ① 必须在前：带 ?spa=1 者一律给 SPA，不再判 UA
        if ($og_bot)  { return 418; }   # ② 抓取器：只给 OG HTML
        try_files $uri $uri/ /index.html;
    }

    location @share_og {
        proxy_pass http://api:8080/public/shares/$share_token/og;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $fwd_proto;
        proxy_read_timeout 30s;
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
