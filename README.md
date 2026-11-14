<div align="center">
  <img src="https://raw.githubusercontent.com/ssmhdssmhd/MXLOGO/main/web/web-logo.png" alt="MX Logo" width="240">
  <h1>MXSniffer</h1>
  <p>基于 Go 与 Lightpanda 无头浏览器的视频媒体地址嗅探服务</p>
</div>

## 更新日志

### MXSniffer.0.0.12 202611140811

- 默认 `CACHE_TTL=0`，每次调用都重新解析，不使用跨请求结果缓存；请求内重复清单下载仍复用，避免额外网络请求。
- 默认解析器直连 `https://art.txnp.cn/?url={url}`，去掉旧入口到最终页面的重定向。
- B 站官方接口先直接请求，只有被拦截时才访问首页补 cookie 并重试，减少一次首页请求。
- 媒体监听不再等待 `body` 就绪，保留媒体优先级、时长校验、源站验证和失败重试。
- `CACHE_TTL=0` 三轮真实测速：B 站 `0.161s/0.143s/0.114s`，腾讯 `5.607s/3.137s/3.291s`，优酷 `7.591s/7.228s/7.416s`，均返回 200；腾讯清单约 30.2 分钟、优酷约 7.7 分钟，B 站代理 Range 返回 206。

### MXSniffer.0.0.11 202611140736

- 优化 `/node.js` 响应速度：同一请求中的时长校验与源站还原复用清单下载，不再重复请求。
- 成功结果默认短期缓存 `2m`，相同视频重复调用无需重新启动浏览器，实测缓存响应约 `0.0005s`。
- 缓存自动过期清理，最多保留 256 条；`CACHE_TTL=0` 可关闭，失败结果不缓存。
- 真实链接三轮测试：腾讯首次约 6.47s、优酷首次约 8.08s、B 站首次约 0.41s，后续相同请求均低于 1ms，返回内容保持一致。

### MXSniffer.0.0.10 202611140722

- 修复第三方 HLS 中转地址过期后跳转到 `banquan.mp4`，导致只播放简短版权提示的问题。
- 对包含外部绝对分片地址的清单，自动验证并还原分片同目录的源站 `index.m3u8`。
- 仅当源站清单引用相同分片时才替换；源站不存在或内容不匹配时保留原地址。
- 腾讯真实链接回归：返回源站清单，811 个分片，总时长约 53.95 分钟，分片 Range 请求为 206。

### MXSniffer.0.0.9 202611140700

- 新增 Web 播放包装页解析：自动解码 `ssmhd.com/player/?url=<Base64>` 并对真实地址继续嗅探。
- 支持最多 3 层嵌套包装，解码结果非 HTTP(S) 时保持原样。
- 兼容 Base64 中 `+` 被查询串转成空格的情况。

### MXSniffer.0.0.8 202611140630

- 所有接口增加 CORS 响应头，支持 `OPTIONS` 预检，前端可直接跨域调用。
- 新增 `/play` 播放代理，透传 `Range` 分段并自动补充 B 站 `Referer`，解决浏览器 403。
- `/node.js` 默认把 B 站 CDN 地址包装为代理地址，代理域名按请求动态生成，不写死。
- 支持 `?proxy=0` 关闭代理、`?proxy=1` 强制代理；代理拒绝内网与本机地址。
- 流式代理场景取消服务端写超时，避免大文件播放被中断。

### MXSniffer.0.0.7 202611140605

- 新增官方直链解析：B 站视频优先调用官方接口获取真实 MP4 地址，不再经过第三方解析页。
- 官方直链约 0.4 秒返回，内容与页面一致，支持 `b23.tv` 短链。
- 其他站点仍回退到解析器流程；官方接口失败时自动回退，不影响原有行为。

### MXSniffer.0.0.6 202611140600

- 修复返回单个 `.ts` / `.m4s` 分片导致播放只有几秒的问题：开启时长过滤时不再把分片作为结果。
- 候选地址改为按优先级逐个校验，前一个时长不足时继续尝试后续地址。
- 增加时长过滤日志，便于排查被丢弃的候选。

### MXSniffer.0.0.5 202611140612

- 新增播放时长过滤：HLS 总时长低于 `MIN_MEDIA_DURATION`（默认 `60s`）的地址不再返回。
- 支持主清单自动跟随最高码率子清单统计时长，直播流不做拦截。

### MXSniffer.0.0.4 202611140542

- 扩展播放地址识别，不再只支持 `.m3u8` / `.mp4`。
- 新增 DASH、FLV、WebM、MKV、TS/M4S 分片、音频格式及对应 MIME 类型。
- 清单类地址优先于分片地址，避免先抓到 `.ts` / `.m4s` 而错过真实播放列表。

### MXSniffer.0.0.3 202611140525

- 默认解析器更换为实测唯一稳定可用的 `bfq.txnp.cn`。
- 批量实测 21 个候选解析器，仅 `bfq.txnp.cn` 在 B 站、腾讯视频、优酷三个站点全部返回真实 HLS。
- README 移除不可用解析器，仅保留可用线路与实测数据。

### MXSniffer.0.0.2 202611140515

- 默认解析器由已停止服务的 `jx.xmflv.com` 更换为 `video.isyour.love`。
- 基于真实站点实测选择解析器：B 站、优酷可返回可播放的 HLS 地址，响应速度优于其他候选。
- `ATTEMPT_WAIT` 默认调整为 `8s`，`REQUEST_TIMEOUT` 调整为 `45s`，以覆盖优酷等首屏加载较慢的站点。
- README 增加解析器实测结果与选择说明。

### MXSniffer.0.0.1 202611140319

- 首个 Go 版本，兼容原项目 `GET /node.js?url=...` 接口和 `1314` 端口。
- 使用 Lightpanda CDP 替代 Puppeteer/Chromium，在页面加载前注册网络监听。
- 支持 HLS、DASH、FLV、WebM、MP4 及音频等常见播放地址，直链输入可直接返回。
- 增加请求超时、一次自动重载、并发限制、健康检查和优雅退出。
- 提供单元测试、Lightpanda 端到端测试、Dockerfile 与 Docker Compose 配置。

当前版本：**v.0.0.12**

## 迁移分析

原压缩包中的 JavaScript 项目已使用 REA `analyze-javascript-application` 进行静态分析。REA 识别出 `/node.js` 网络端点、`server.js` 入口和 Puppeteer 依赖；结合源码确认原流程为：视频页 URL → 第三方解析页 → 浏览器网络响应 → 媒体 URL。

Go 版保留这一调用方式，并修复原实现中监听注册过晚、异常时浏览器可能未关闭、向客户端返回堆栈等问题。旧 PHP 项目中的数据库、缓存和硬编码凭据不属于本服务运行依赖，也未迁移到本仓库。

## 解析器

默认解析器：`https://art.txnp.cn/?url={url}`，实测稳定返回真实 HLS（`#EXTM3U`）播放列表：

| 站点 | 结果 | 耗时 |
| --- | --- | --- |
| B 站 | 200，真实 HLS | 约 4.4s |
| 腾讯视频 | 200，真实 HLS | 约 4.4s |
| 优酷 | 200，真实 HLS | 约 7.8s |

其余候选解析器批量实测无法稳定返回真实媒体地址，已不再保留。

## 播放地址识别

- 清单：`.m3u8`、`.m3u`、`.mpd`、`.f4m`、`.ism`
- 媒体文件：`.mp4`、`.m4v`、`.webm`、`.mkv`、`.flv`、`.mov`、`.avi`、`.wmv`、`.3gp`、`.ogv`、`.f4v`、`.m2ts`、`.mts`
- 音频：`.m4a`、`.aac`、`.mp3`、`.ogg`、`.oga`、`.opus`、`.wav`、`.flac`
- 分片：`.m4s`、`.ts`，仅在关闭时长过滤（`MIN_MEDIA_DURATION=0`）时兜底返回
- MIME：HLS、DASH、Smooth Streaming、FLV，以及 `video/*`、`audio/*`

清单类地址优先级最高，命中后立即返回；普通媒体文件次之；分片最低，避免把单个 `.ts` / `.m4s` 分片误当成完整播放地址。

返回前会校验 HLS 清单总时长：低于 `MIN_MEDIA_DURATION`（默认 `60s`）的地址会被丢弃并继续尝试下一个候选，避免返回版权提示、广告等短视频。单个分片（`.ts` / `.m4s`）通常只包含几秒内容，开启过滤时不会返回；直播流和无法确定时长的格式不做拦截；直接传入的媒体地址原样返回。

对带时效签名的 HLS 中转地址，服务会根据绝对分片地址查找同目录的源站 `index.m3u8`，并核对源站清单是否引用相同分片。验证通过后返回源站清单，避免中转地址过期后跳转到版权提示片；验证失败则保留原地址。

## 官方直链

B 站视频优先走官方接口，不经过第三方解析页：

- 识别 `bilibili.com/video/BV...` 与 `b23.tv` 短链
- 依次调用官方 `view` 与 `playurl` 接口，返回与页面时长一致的真实 MP4 地址
- 官方接口失败或站点不支持时，自动回退到解析器流程

返回的 B 站地址带防盗链，播放时需携带 `Referer: https://www.bilibili.com/`，否则会返回 403。

## 接口

### 嗅探媒体地址

```http
GET /node.js?url={经过URL编码的视频页或媒体地址}
```

成功响应：

```json
{"code":200,"url":"https://cdn.example.com/media/master.m3u8"}
```

未找到媒体地址（含时长不足被过滤）时返回 `404`，浏览器连接失败时返回 `502`，超时时返回 `504`。错误响应使用 `code` 和 `message` 字段，不返回内部堆栈。

```bash
curl --get \
  --data-urlencode 'url=https://example.com/watch?v=1' \
  http://127.0.0.1:1314/node.js
```

如果输入本身就是可识别的媒体地址（如 `.m3u8`、`.mpd`、`.mp4`、`.flv`），服务会直接返回该地址，不启动浏览器会话。

### Web 播放包装页

部分站点把真实地址放在包装页的 Base64 参数里，例如：

```text
https://ssmhd.com/player/?url=<Base64 编码的地址>
```

服务会自动解码该参数并对解码后的地址继续嗅探，最多展开 3 层嵌套。解码结果必须是 `http` 或 `https` 地址，否则保持原样处理。

```bash
curl --get \
  --data-urlencode 'url=https://ssmhd.com/player/?url=xxxxx' \
  http://127.0.0.1:1314/node.js
```

### 跨域与播放代理

所有接口均返回以下 CORS 头，允许任意来源跨域调用：

```http
Access-Control-Allow-Origin: *
Access-Control-Allow-Methods: GET, HEAD, OPTIONS
Access-Control-Allow-Headers: Content-Type, Range, If-Range
Access-Control-Expose-Headers: Content-Length, Content-Range, Accept-Ranges, Content-Type
```

`OPTIONS` 预检请求返回 `204`。

B 站官方地址有防盗链，浏览器直接播放会 403。接口默认把 B 站 CDN 地址包装成本服务地址：

```json
{"code":200,"url":"http://你的服务地址/play?url=https%3A%2F%2F...bilivideo.com%2F...mp4"}
```

代理地址根据请求的 `Host`（支持 `X-Forwarded-Host` / `X-Forwarded-Proto`）动态生成，不写死域名。播放代理支持 `Range` 分段，可直接拖动进度条，并自动补上 `Referer`。

可通过 `proxy` 参数控制：

- `?proxy=0`：返回原始媒体地址
- `?proxy=1`：强制所有地址走代理
- 默认：仅 B 站 CDN 地址走代理，其他站点原样返回

直接访问代理：

```http
GET /play?url={经过URL编码的媒体地址}
```

代理仅允许公网 HTTP(S) 地址，会拒绝本机、内网、链路本地等地址，避免被当作内网探测工具。

### 健康检查

```bash
curl http://127.0.0.1:1314/healthz
```

```json
{"code":200,"status":"ok"}
```

## Docker Compose 运行

需要 Docker 与 Docker Compose。Compose 会启动 Lightpanda 和 MXSniffer，Lightpanda 仅在容器网络内暴露 CDP。

```bash
docker compose up --build -d
curl http://127.0.0.1:1314/healthz
```

停止服务：

```bash
docker compose down
```

## 本地运行

要求 Go 1.24+，并单独安装 [Lightpanda](https://github.com/lightpanda-io/browser)。

```bash
LIGHTPANDA_DISABLE_TELEMETRY=true lightpanda serve \
  --host 127.0.0.1 --port 9222 --log-level info
```

另一个终端运行：

```bash
go run ./cmd/mxsniffer
```

## 配置

所有配置均为环境变量，无需修改源码。

| 变量 | 默认值 | 说明 |
| --- | --- | --- |
| `LISTEN_ADDR` | `:1314` | HTTP 监听地址 |
| `LIGHTPANDA_WS_URL` | `ws://127.0.0.1:9222` | Lightpanda CDP WebSocket 地址 |
| `PARSER_URL_TEMPLATE` | `https://art.txnp.cn/?url={url}` | 解析页模板，`{url}` 会被替换为编码后的输入地址 |
| `USER_AGENT` | Chrome 131 浏览器 UA | 浏览器 User-Agent；Lightpanda 会忽略包含 `Mozilla` 的自定义值 |
| `REQUEST_TIMEOUT` | `45s` | 单次 HTTP 嗅探总超时 |
| `ATTEMPT_WAIT` | `8s` | 每次页面加载后的媒体监听时长 |
| `MIN_MEDIA_DURATION` | `60s` | HLS 播放列表最短总时长，低于该值不返回；设为 `0` 关闭过滤并允许分片兜底 |
| `CACHE_TTL` | `0` | 默认不使用跨请求结果缓存；需要时设为 `2m` 等时长开启成功结果缓存 |
| `MAX_CONCURRENT_REQUESTS` | `2` | 同时执行的浏览器会话数 |

## 测试

```bash
go test ./...
go vet ./...
go build ./cmd/mxsniffer
```

真实 Lightpanda 端到端测试：

```bash
LIGHTPANDA_INTEGRATION=1 \
LIGHTPANDA_WS_URL=ws://127.0.0.1:9222 \
go test ./internal/sniffer -run TestLightpandaIntegration -v
```

测试使用本地临时页面和本地模拟 `.m3u8` 响应，不依赖第三方视频站或解析服务。

## 使用边界

请仅处理自己拥有或获授权访问的内容。Lightpanda 与本服务不绕过 DRM、付费、登录或访问控制；第三方解析页的可用性及合规性由其运营方决定。
