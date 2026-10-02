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

    # Job000131 T3-A：api upstream 不可达（启动迁移窗口/容器重建/间歇挂起）时的启动兜底页。
    # 纯内联静态页 + sessionStorage 限频自动重试（5s 起步、40 次上限），与前端 BootGate
    # 互补：JS 未加载完成时用户也能看到带样式的等待页而非浏览器裸错误。
    error_page 502 503 504 = @boot;
    location @boot {
        internal;
        default_type text/html;
        # 只给页面导航返回等待页 HTML；API/探测请求（Accept 不含 text/html：axios/fetch/hls.js
        # 默认 */*）必须保持 502 —— 否则前端 BootGate 的 /ready 探测会拿到 200 HTML 误判就绪
        # （A 方案背刺 B 方案，实测缺陷），动态流与业务请求也应识别失败而非吞成 200。
        if ($http_accept !~* "text/html") { return 502; }
        return 200 '<!DOCTYPE html><html lang="zh"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>系统启动中</title><style>body{margin:0;min-height:100vh;display:flex;align-items:center;justify-content:center;background:#f7f7f5;font-family:system-ui,sans-serif}.c{max-width:420px;padding:32px;background:#fff;border-radius:12px;border:1px solid rgba(0,0,0,.08)}h1{margin:0 0 8px;font-size:20px;font-weight:500}p{margin:0;color:#666;font-size:14px}</style></head><body><div class="c"><h1>系统启动中</h1><p>服务正在启动，页面将自动重试；若长时间无响应请检查容器状态。</p></div><script>var n=+(sessionStorage.getItem("panomint_boot")||0);if(n<40){sessionStorage.setItem("panomint_boot",String(n+1));setTimeout(function(){location.reload()},5000)}</script></body></html>';
    }

    # 分块上传（Stage 2 实测 9MB 块），HLS 大响应放宽超时
    client_max_body_size 100m;

    # 与 SPA 路由冲突的 API 前缀：导航回退 SPA，API 调用透传后端
    # people / tags 既是前端路由（/people、/tags）又是 API 前缀（GET /people、GET /tags），
    # 故与 /albums 等同样处理：浏览器导航回退 SPA，axios（Accept: application/json）透传 API。
    # map：与 /people、/tags 同理 —— 前端路由 /map（MapView）与 API 前缀
    # /map/search、/map/providers 冲突，故进本组而非纯 API 组
    # admin：Job000052 管理后台 —— 前端路由 /admin（AdminView）与 API 前缀 /admin/* 冲突，
    # 同样进本组；漏配的话浏览器打开 /admin 会拿到 API 的 401 JSON 而不是页面
    # setup：Job000107 首次安装向导 —— 前端路由 /setup（SetupView）与 API 前缀
    # /setup/status、/setup 冲突，同型进本组；漏配则向导页拿不到后端状态（index.html 假 200）
    location ~ ^/(search|albums|spaces|folders|people|tags|map|admin|setup)(/|$) {
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

    # debug：Job000121 转码远程调试 WS 通道（/debug/channel/:channel_id，设计 §5.5）。
    # WS Upgrade 必需（漏配 → 握手被 nginx 405 或拿回 index.html，坑位 20 同款）；
    # 调试会话长寿命（TTL 最长 72h），读写超时放宽到 3600s（nginx 默认 60s 会掐长空闲连接）。
    # 独立 location：Upgrade/Connection 头只落在 WS 路径上，不给全组 API 平摊。
    location ~ ^/debug/ {
        proxy_pass http://api:8080;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $fwd_proto;
        proxy_read_timeout 3600s;
        proxy_send_timeout 3600s;
    }

    # 纯 API 路由（URI 原样透传，无路径剥离）
    # ai：/ai/tags 与 /ai/faces（Job000010 Phase 4 零样本打标与人脸扫描触发）
    # bandwidth：/bandwidth、/bandwidth/probe、/bandwidth/self-test（Phase 4 P1 带宽自测）
    # compute-nodes：T6.2 算力节点。⚠️ 节点侧 /compute-nodes/agent/* 也在同一前缀下，
    #   必须整体透传（否则 POST 落到静态目录 → nginx 返回 405，实测踩过）
    # tags / map / admin 不在此列——它们同时是前端路由，已移入上面的导航判别组
    # dav：Job000055 WebDAV（PROPFIND/MKCOL/MOVE/LOCK 等扩展方法整体透传；Basic 认证不经 JWT，
    #   由后端自认证；漏配则 nginx 405 或落 SPA 回退）
    # version：Job000105 构建版本三元组（无鉴权，部署验收/排障第一现场；纯 API 无前缀冲突）
    # scan/fs/jobs：Job000123 成员扫描三端点（POST /scan、GET /fs/tree、GET /jobs/:id；
    #   漏配则成员扫描刷新/直连拿到 index.html 或 405）
    # debug 不在此列——WS Upgrade/长超时需要独立 location，见上方 ^/debug/ 块
    # setup 不在此列——它同时是前端路由，已移入上面的导航判别组
    location ~ ^/(auth|media|transcode|shares|public|health|ready|metrics|geo|tiles|preferences|user|ai|bandwidth|compute-nodes|dav|storage|version|scan|fs|jobs|agent)(/|$) {
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
        add_header X-Content-Type-Options "nosniff" always;
        add_header X-Frame-Options "SAMEORIGIN" always;
        add_header X-Content-Type-Options "nosniff" always;
        add_header X-Frame-Options "SAMEORIGIN" always;
        add_header Referrer-Policy "strict-origin-when-cross-origin" always;
        add_header Permissions-Policy "geolocation=(self), camera=(), microphone=()" always;
        add_header Content-Security-Policy "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline' https://fonts.googleapis.com; font-src 'self' https://fonts.gstatic.com data:; img-src 'self' data: blob: https://*.autonavi.com https://*.amap.com; connect-src 'self' https://*.autonavi.com https://*.is.autonavi.com https://dashscope.aliyuncs.com; worker-src 'self' blob:; object-src 'none'; base-uri 'self'; form-action 'self'; frame-ancestors 'self'" always;
        add_header Referrer-Policy "strict-origin-when-cross-origin" always;
        add_header Permissions-Policy "geolocation=(self), camera=(), microphone=()" always;
        try_files $uri =404;
    }

    # PWA：Web App Manifest 需正确 MIME（application/manifest+json）
    location = /manifest.webmanifest {
        types { }
        default_type application/manifest+json;
        add_header X-Content-Type-Options "nosniff" always;
        add_header X-Frame-Options "SAMEORIGIN" always;
        add_header X-Content-Type-Options "nosniff" always;
        add_header X-Frame-Options "SAMEORIGIN" always;
        add_header Referrer-Policy "strict-origin-when-cross-origin" always;
        add_header Permissions-Policy "geolocation=(self), camera=(), microphone=()" always;
        add_header Content-Security-Policy "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline' https://fonts.googleapis.com; font-src 'self' https://fonts.gstatic.com data:; img-src 'self' data: blob: https://*.autonavi.com https://*.amap.com; connect-src 'self' https://*.autonavi.com https://*.is.autonavi.com https://dashscope.aliyuncs.com; worker-src 'self' blob:; object-src 'none'; base-uri 'self'; form-action 'self'; frame-ancestors 'self'" always;
        add_header Referrer-Policy "strict-origin-when-cross-origin" always;
        add_header Permissions-Policy "geolocation=(self), camera=(), microphone=()" always;
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
        # Vary: Accept —— 防 Chrome 把 document 导航缓存的 index.html 复用给同 URL 的
        # XHR（导航组路径 /albums/:id 等的 getAlbum 与页面同 URL；无 Vary 时缓存键
        # 不含 Accept → 刷新详情页时 XHR 命中 HTML 缓存 → 相册详情页整页坏）。
        add_header Vary Accept always;
        add_header X-Content-Type-Options "nosniff" always;
        add_header X-Frame-Options "SAMEORIGIN" always;
        add_header X-Content-Type-Options "nosniff" always;
        add_header X-Frame-Options "SAMEORIGIN" always;
        add_header Referrer-Policy "strict-origin-when-cross-origin" always;
        add_header Permissions-Policy "geolocation=(self), camera=(), microphone=()" always;
        add_header Content-Security-Policy "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline' https://fonts.googleapis.com; font-src 'self' https://fonts.gstatic.com data:; img-src 'self' data: blob: https://*.autonavi.com https://*.amap.com; connect-src 'self' https://*.autonavi.com https://*.is.autonavi.com https://dashscope.aliyuncs.com; worker-src 'self' blob:; object-src 'none'; base-uri 'self'; form-action 'self'; frame-ancestors 'self'" always;
        add_header Referrer-Policy "strict-origin-when-cross-origin" always;
        add_header Permissions-Policy "geolocation=(self), camera=(), microphone=()" always;
        try_files /index.html =404;
    }

    # Vue SPA history 路由回退（/timeline /player/:id /share/:token /login 等）
    location / {
        add_header Vary Accept always;
        add_header X-Content-Type-Options "nosniff" always;
        add_header X-Frame-Options "SAMEORIGIN" always;
        add_header X-Content-Type-Options "nosniff" always;
        add_header X-Frame-Options "SAMEORIGIN" always;
        add_header Referrer-Policy "strict-origin-when-cross-origin" always;
        add_header Permissions-Policy "geolocation=(self), camera=(), microphone=()" always;
        add_header Content-Security-Policy "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline' https://fonts.googleapis.com; font-src 'self' https://fonts.gstatic.com data:; img-src 'self' data: blob: https://*.autonavi.com https://*.amap.com; connect-src 'self' https://*.autonavi.com https://*.is.autonavi.com https://dashscope.aliyuncs.com; worker-src 'self' blob:; object-src 'none'; base-uri 'self'; form-action 'self'; frame-ancestors 'self'" always;
        add_header Referrer-Policy "strict-origin-when-cross-origin" always;
        add_header Permissions-Policy "geolocation=(self), camera=(), microphone=()" always;
        try_files $uri $uri/ /index.html;
    }

    # ===== 安全响应头（Job000141，审查报告 L-1）=====
    # ⚠️⚠️ **nginx add_header 的继承规则是本条生效的前提，务必读懂**：
    #     「子 location 只要出现**任意一条** add_header，就完全不再继承父级
    #      server 块的 add_header」—— 不是合并，是**整体覆盖**。
    #     本文件里 `location /`、`@spa`、`= /sw.js`、`= /manifest.webmanifest`
    #     都各自带了 add_header（Vary / Cache-Control / nosniff 四条），
    #     所以**把安全头写在 server 块里一条都不会下发**（实测：容器内 grep 到
    #     5 处配置，HTTP 响应里一条都没有，只剩 Vary: Accept）。
    #     正解：每个 location **重复声明** 下面这四条。
    #
    # 用 include 片段而非复制粘贴：nginx 的 include 在同一层级展开，
    # 写在 server 块里对「**本块内没有 add_header 的** location」有效，
    # 但对上面那些自带 add_header 的 location 依然无效 —— 故这里用
    # `$secure_headers` 变量无法解决（变量不能承载 add_header 指令）。
    # 因此采用**显式重复**（4 条 × 5 处），并在下面每处都写明「勿删」。
    #
    # 为什么选这四条（逐条理由）：
    #   nosniff            —— 禁 MIME 嗅探；本项目严格 serve 已构建产物，不依赖嗅探
    #   SAMEORIGIN         —— 禁第三方 iframe 嵌入（点击劫持）。
    #                        /public/shares/:token 是**无鉴权公开页**，最需要这条
    #   Referrer-Policy    —— URL 里可带 ?at=<access_token>（Job000129 的 video
    #                        读路径），strict-origin 可避免 token 经 Referer 外泄
    #   Permissions-Policy —— 显式关闭不需要的传感器（geolocation 仅自用）
    #
    # 5 条头（含 Content-Security-Policy）逐 location 重复声明，勿删任何一个。
    #
    # CSP 的放行依据（Job000142 实测得出，非猜）：
    #   style-src  fonts.googleapis.com — index.html 用 <link> 引 Google Fonts
    #                        （Outfit + Noto Sans SC），且 :style 动态绑定需要
    #                        'unsafe-inline'（地图/图表按数据算样式）
    #   font-src   fonts.gstatic.com    — 字体文件实际托管处（实测根路径 404
    #                        属正常，字体文件路径由 CSS 给出）
    #   img-src    *.autonavi.com       — 地图瓦片（实测 webrd01 200）
    #   connect-src dashscope.aliyuncs.com — LLM 上游（实测 401=可达仅需鉴权；
    #                        page-agent 在浏览器侧直连，服务端代理见 agentllm）
    #   worker-src blob:                — maplibre-gl worker（实测 200）
    #
    # ⚠️ CSP 与 add_header 覆盖规则叠加：**同样必须逐 location 写**。
    #    已用 headless CDP e2e 真实打开全部路由验证无阻断（见
    #    文档/代码审查报告_v1.9.2.md 的 L-1 闭环段）。
}
EOF
