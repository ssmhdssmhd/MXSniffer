<div align="center">
  <img src="https://raw.githubusercontent.com/ssmhdssmhd/MXLOGO/main/web/web-logo.png" alt="MX Logo" width="240">
  <h1>MXSniffer</h1>
  <p>基于 Go 与 Lightpanda 无头浏览器的视频媒体地址嗅探服务</p>
</div>

## 更新日志

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
- 支持 `.m3u8`、`.mp4` 及常见流媒体 MIME 类型，直链输入可直接返回。
- 增加请求超时、一次自动重载、并发限制、健康检查和优雅退出。
- 提供单元测试、Lightpanda 端到端测试、Dockerfile 与 Docker Compose 配置。

当前版本：**v.0.0.3**

## 迁移分析

原压缩包中的 JavaScript 项目已使用 REA `analyze-javascript-application` 进行静态分析。REA 识别出 `/node.js` 网络端点、`server.js` 入口和 Puppeteer 依赖；结合源码确认原流程为：视频页 URL → 第三方解析页 → 浏览器网络响应 → 媒体 URL。

Go 版保留这一调用方式，并修复原实现中监听注册过晚、异常时浏览器可能未关闭、向客户端返回堆栈等问题。旧 PHP 项目中的数据库、缓存和硬编码凭据不属于本服务运行依赖，也未迁移到本仓库。

## 解析器

默认解析器：`https://bfq.txnp.cn/player?url={url}`，实测稳定返回真实 HLS（`#EXTM3U`）播放列表：

| 站点 | 结果 | 耗时 |
| --- | --- | --- |
| B 站 | 200，真实 HLS | 约 4.4s |
| 腾讯视频 | 200，真实 HLS | 约 4.4s |
| 优酷 | 200，真实 HLS | 约 7.8s |

其余候选解析器批量实测无法稳定返回真实媒体地址，已不再保留。

## 接口

### 嗅探媒体地址

```http
GET /node.js?url={经过URL编码的视频页或媒体地址}
```

成功响应：

```json
{"code":200,"url":"https://cdn.example.com/media/master.m3u8"}
```

未找到媒体地址时返回 `404`，浏览器连接失败时返回 `502`，超时时返回 `504`。错误响应使用 `code` 和 `message` 字段，不返回内部堆栈。

```bash
curl --get \
  --data-urlencode 'url=https://example.com/watch?v=1' \
  http://127.0.0.1:1314/node.js
```

如果输入本身就是 `.m3u8` 或 `.mp4` HTTP(S) 地址，服务会直接返回该地址，不启动浏览器会话。

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
| `PARSER_URL_TEMPLATE` | `https://bfq.txnp.cn/player?url={url}` | 解析页模板，`{url}` 会被替换为编码后的输入地址 |
| `USER_AGENT` | Chrome 131 浏览器 UA | 浏览器 User-Agent；Lightpanda 会忽略包含 `Mozilla` 的自定义值 |
| `REQUEST_TIMEOUT` | `45s` | 单次 HTTP 嗅探总超时 |
| `ATTEMPT_WAIT` | `8s` | 每次页面加载后的媒体监听时长 |
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
