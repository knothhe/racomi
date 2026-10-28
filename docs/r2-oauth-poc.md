# Cloudflare R2 OAuth 接入与 PoC

OAuth 用于自动配置用户自己的 R2 桶。对象读写、加密、同步、冲突和历史恢复继续使用 `internal/cloudsync` 的 S3 引擎；手动 S3 配置独立保留。普通用户无需创建 OAuth 应用或填写 Client ID，也无需复制任何 API Token。发行者注册、授权及真实 PoC 操作由维护者执行。

## 发行者配置

在 Cloudflare **Manage Account → OAuth clients** 创建 racomi 的应用：

| 字段 | 值 |
| --- | --- |
| Client Name | `racomi` |
| Grant type | Authorization Code（`authorization_code`）和 Refresh Token（`refresh_token`） |
| Response type | Code（`code`） |
| Token endpoint authentication | None（`none`） |
| Redirect URL | `http://localhost:18796/oauth/cloudflare/callback` |
| Client URL | 私有 PoC 可留空；公开发行时填自己控制的应用网站 |

使用 PKCE S256，不生成或内置 Client Secret。授权请求附带 `offline_access` 以获取 Refresh Token，用于静默续期 R2 凭据，因此注册时必须同时勾选 Refresh Token grant；未勾选时 Cloudflare 可能以 `invalid_scope` 拒绝授权。`offline_access` 不是必需 scope：若未返回 Refresh Token，客户端按仅授权码模式继续工作，只是续期需要重新授权。PoC 先使用 private visibility，仅父账号成员能够授权。面向其他用户发行前，需要提供应用名称、Logo、Client URL，完成域名验证，再设为 public visibility；该公开操作不可回退。当前实现不使用 Wrangler 的 Client ID 或设备授权流。

点击 Continue 后选择以下四项，全部保持 **Required**：

| 控制台权限名称 | Scope ID | 用途 |
| --- | --- | --- |
| Account Settings Read | `account-settings.read` | 获取用户可访问的账号 |
| Workers R2 Storage Read | `workers-r2.read` | 列出存储桶和读取桶配置 |
| Workers R2 Storage Bucket Item Write | `workers-r2-bucket-item.write` | 授予桶内对象读写能力 |
| Account API Tokens Write | `account-api-tokens.write` | 自动创建和删除专用桶凭据 |

即使注册时四项都设为 Required，Cloudflare 同意页仍会把 `account-api-tokens.write` 归入 Additional access → Sensitive scopes，并且默认不勾选。用户直接点 Authorize 时，同意页只提交三项，服务端返回 400（code 1420，"Required scopes cannot be omitted from the grant"），页面显示授权失败，也不会回调 racomi。用户必须先点 Edit Permissions 打开 Account API Tokens Write。racomi 在连接前和等待授权期间都会显示这条提示；回调收到 `access_denied` 时也会给出同样的指引。

选择 **Workers R2 Storage Read**；不要选成 **Workers R2 Storage Bucket Item Read**。之前推荐的 Metadata Read 已修正，[列桶 API 明确接受的是 Storage Read / Write](https://developers.cloudflare.com/api/resources/r2/subresources/buckets/methods/list/)。

官方文档对部分旧 scope 的写法不同。注册前用 `GET /oauth/scopes` 核对点分名称；不要照抄旧的 `account_api_tokens:create`。localhost 固定端口必须与注册值完整一致；不假设通配端口或自定义 URL scheme 可用。

注册完成后，把公开 Client ID 填入 [internal/desktop/cloudflare-registration.json](../internal/desktop/cloudflare-registration.json)：

```json
{
  "ClientID": "这里填写 racomi 的公开 Client ID",
  "RedirectURL": "http://localhost:18796/oauth/cloudflare/callback"
}
```

该文件通过 `go:embed` 内置到客户端，填写后重新构建即可。不要填写 Client Secret、API Token 或 S3 密钥。不要复制 brass 的 Client ID 或其 `8977` 回调；racomi 使用独立端口 `18796`。回调地址必须与控制台完整一致，`localhost` 和 `127.0.0.1` 不能混用；若需要修改回调，必须在本文件和控制台同步修改。

回调服务仅在点击连接后启动，授权完成、取消或五分钟超时后关闭；应用刚启动时没有监听该端口属于正常情况。

开发验证也可先用环境变量覆盖 Client ID：

```sh
RACOMI_CLOUDFLARE_CLIENT_ID='<public-client-id>' make dev
```

macOS / Windows 构建入口也支持将该变量嵌入二进制。配置优先使用运行时环境变量，再使用 ldflags，最后使用 JSON 中的值；没有新的发行配置时仍兼容此前保存的公开 Client ID。发布后的用户不需要环境变量或 Client ID。没有 Client ID 的构建会明确显示 OAuth 未配置，手动 S3 仍可用。

## 授权与凭据边界

系统浏览器 → Cloudflare 登录、账号授权 → 同时监听 `127.0.0.1` 与 `[::1]` 的 localhost 回调（主机没有 IPv6 时只监听 IPv4） → Go 后端校验 state 并换票 → 获取账号与桶 → 创建桶范围 Account API Token → 以 `id` 与 `SHA256(value)` 得到 S3 凭据。

后端查询可选权限组，生成的 Token 仅有 `Workers R2 Storage Bucket Item Write`，资源严格限定为 `com.cloudflare.edge.r2.bucket.<account>_<jurisdiction>_<bucket>`，有效期 30 天。授权用户需具备 API Token Provisioning 或 Super Administrator 能力；Token 权限受用户权限与 OAuth 授权范围共同限制。PoC 选择已有桶，不自动开通 R2 或创建桶。

OAuth Token 和派生的 S3 秘密进入系统凭据存储；磁盘上的 OAuth 元数据只保留公开 Client ID、连接身份、桶、Token ID 和到期时间。同步加密口令同样存入系统凭据存储。前端只获取是否已保存的状态，不获取 OAuth code/verifier/access token/refresh token 或派生的 key pair。

macOS 拒绝、取消或不允许凭据访问后，本次运行会停止后续自动读取、保存和删除请求，包括轮询、后台同步与失败补偿。仅用户主动点击连接或断开 Cloudflare 才允许重新尝试；状态查询不会解除该限制。授权进行中不读取旧账号凭据，账号加载失败后也不由轮询再次请求。手动 S3 模式不会为加载账号而访问 R2 Keychain。

系统凭据成功读取后仅在当前进程内复用，按资料库、service 和 key 隔离；并发读取共享结果，失败不缓存。拒绝访问、写入、删除、重新连接、主动断开、退出及 SDK 元数据变化会清除缓存；写入后首次读取仍访问系统存储。缓存仍受 SDK 的拒绝访问限制，每次对象操作仍通过 Manager.Credentials 检查到期及续期。直接在 Keychain 中修改条目或访问控制不保证立即被内存副本感知，可重启或主动重新连接。此优化未迁移 Keychain 后端，也不代表首次双弹窗已消除。

同步前检查 S3 凭据有效期；有效的 S3 凭据可继续同步，无需每次进行 OAuth。授权带 Refresh Token 时，R2 凭据剩余不足 7 天即在同步时静默刷新 Access Token 并换发新的 30 天凭据；提前续期失败不影响仍有效的凭据，一小时后再试，只有剩余不足 5 分钟仍失败才报错。刷新串行进行并保存旋转后的令牌；元数据只记录授权是否可续期，不在磁盘保存令牌。只有 Access Token 的授权（包括 Refresh Token 失效后）仅在凭据即将到期时尝试续期，Access Token 过期后选择存储桶、续期 R2 凭据或断开连接需要重新授权。R2 凭据轮换后清理旧 Token；断开连接显式删除应用创建的 Token 并撤销 OAuth，不能假定 OAuth 撤销会自动删除独立 S3 凭据。

## 验证范围

Dev 包将脱敏诊断写入数据目录下的 `logs/cloudflare-oauth.jsonl`，默认位置为 `~/Library/Application Support/racomi-dev/logs/cloudflare-oauth.jsonl`。文件权限为 `0600`，单文件上限 512 KiB，保留一份轮转文件；正式包不开启。日志记录 UTC 时间、进程、授权轮次、监听/取消/超时、回调校验布尔值、标准 OAuth error 码（非标准值记为 `other`，不记录 error_description），以及 Cloudflare 请求的 HTTP 状态、数字错误码、CF-Ray 和耗时，不记录 URL 查询、code/state/PKCE、令牌、密钥、账号或桶名。

出现授权页通用错误时，先区分 `listen_started`、`callback_received`、`code_exchange_started`：只有启动记录但没有回调记录，表示该轮监听期间未收到 HTTP 回调；收到回调后才进入 token 交换。Cloudflare 授权页自身的请求失败仍需从浏览器检查，客户端日志不能代替该阶段的网络证据。权限目录的 `is_selectable` 仅作诊断提示，创建凭据以 Cloudflare POST 的实际权限检查为准。

本地集成测试以 HTTP 替身覆盖真实 OAuth 回调、state/PKCE、换票、桶范围 Token 创建、系统凭据存储接口、保存和重开、S3 条件写入、双向同步及备份。替身测试不会连接真实 Cloudflare、系统 Keychain 或用户日常资料库。

真实 PoC 必须另用发行者自己的 Client ID 和已开通 R2 的测试账号完成，不能用替身通过代替：

1. 填写 JSON 中的 Client ID 后重新构建，在隔离 `RACOMI_DATA_DIR` 下启动客户端，使用专用测试桶或独立测试前缀。
2. 浏览器完成授权，验证回调、实际授予的 scopes 与账号/辖区/桶列表。
3. 选择桶并检查生成的 Account API Token 的权限、资源及有效期。新权限传播可能短暂延迟；连接测试失败时保留输入并重试。
4. 测试连接、保存并同步；另一隔离资料库用手动 S3 模式连接同一 endpoint/bucket/prefix/passphrase，验证资料、冲突和备份历史一致。
5. 切换方式、取消编辑、重启、离线重试；确认两套设置保留，切换未保存时原目标继续生效。
6. 断开 Cloudflare，验证专用 Token 被删除；单独检查从 Cloudflare 面板撤销 OAuth 后 S3 凭据的实际失效行为。

真实 PoC 当前仍需有效 Client ID 和账号授权；尚不能将 Cloudflare 端的权限交集、localhost 注册接受情况或撤销语义标为已通过。

## 官方依据

- [OAuth Client 注册与 PKCE](https://developers.cloudflare.com/fundamentals/oauth/create-an-oauth-client/)
- [OAuth 创建 Account API Token](https://developers.cloudflare.com/changelog/post/2026-10-01-account-api-token-provisioning/)
- [R2 桶范围权限与 S3 凭据派生](https://developers.cloudflare.com/r2/api/tokens/)
- [Cloudflare 官方当前 scope 目录](https://github.com/cloudflare/mcp/blob/main/src/auth/derived-oauth-scopes.json)
