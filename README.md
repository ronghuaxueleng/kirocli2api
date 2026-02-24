# KiroCLI2API

将 Amazon Q (Kiro CLI) 后端转换为兼容 OpenAI 和 Anthropic API 格式的代理服务。

## 功能特性

- **OpenAI API 兼容** - `/v1/chat/completions`，支持流式/非流式响应
- **Anthropic API 兼容** - `/v1/messages`，支持流式/非流式响应
- **Token 计数** - `/v1/messages/count_tokens`
- **模型列表** - `/v1/models`，自动识别 OpenAI/Anthropic 客户端并返回对应格式
- **Thinking 模式** - 支持扩展推理（模型名加 `-thinking` 后缀或通过请求参数启用）
- **Tool Calling** - 支持函数调用（OpenAI 和 Anthropic 格式）
- **Web Search** - 通过 MCP 端点实现网页搜索工具
- **图片理解** - 支持 Base64 图片输入
- **多账号管理** - 支持 CSV 文件或远程 API 加载账号，自动轮换和刷新 Token
- **TLS 指纹** - 使用 uTLS 模拟 Chrome 浏览器 TLS 指纹
- **代理支持** - 支持 HTTP/SOCKS5 代理
- **Docker 部署** - 提供 Dockerfile 和 docker-compose 配置
- **CI/CD** - GitHub Actions 自动构建并推送 Docker 镜像到 GHCR

## 快速开始

### 环境要求

- Go 1.24+

### 本地运行

1. 复制并编辑配置文件：

```bash
cp .env.example .env
```

2. 准备账号文件（CSV 模式）：

创建 `resources/accounts.csv`，格式如下：

```csv
enabled,refresh_token,client_id,client_secret
True,your-refresh-token,your-client-id,your-client-secret
```

3. 启动服务：

```bash
go run .
```

服务默认监听 `4000` 端口。

### Docker 部署

```bash
docker compose up -d
```

## 环境变量

| 变量 | 说明 | 默认值 |
|------|------|--------|
| `PORT` | 监听端口 | `4000` |
| `GIN_MODE` | Gin 运行模式（`release`/`debug`） | `release` |
| `BEARER_TOKEN` | API 访问密钥 | 必填 |
| `AMAZON_Q_URL` | Amazon Q 服务端点 | 必填 |
| `OIDC_URL` | OIDC Token 刷新端点 | 必填 |
| `PROXY_URL` | 代理地址（如 `http://127.0.0.1:7890`） | 空 |
| `ACCOUNT_SOURCE` | 账号来源（`csv` 或 `api`） | `csv` |
| `ACCOUNTS_CSV_PATH` | CSV 账号文件路径 | `resources/accounts.csv` |
| `ACTIVE_TOKEN_COUNT` | 同时活跃的 Token 数量 | `5` |
| `MAX_REFRESH_ATTEMPT` | Token 刷新最大重试次数 | `5` |
| `ACCOUNT_API_URL` | 远程账号 API 地址（`api` 模式） | - |
| `ACCOUNT_API_TOKEN` | 远程账号 API 密钥（`api` 模式） | - |

## API 端点

### 业务接口（需要认证）

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/v1/chat/completions` | OpenAI 格式对话补全 |
| POST | `/v1/messages` | Anthropic 格式消息 |
| POST | `/v1/messages/count_tokens` | Anthropic Token 计数 |
| GET | `/v1/models` | 获取可用模型列表 |

### 调试接口（无需认证）

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/debug/token` | 测试 Token 刷新 |
| POST | `/debug/anthropic2q` | 查看 Anthropic 到 Q 的请求映射结果 |

### 认证方式

请求时在 Header 中携带 `BEARER_TOKEN` 配置的密钥：

- `Authorization: Bearer sk-xxx`
- 或 `x-api-key: sk-xxx`

## 使用示例

### OpenAI 格式

```bash
curl http://localhost:4000/v1/chat/completions \
  -H "Authorization: Bearer sk-1234567890" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "claude-sonnet-4-20250514",
    "messages": [{"role": "user", "content": "Hello"}],
    "stream": true
  }'
```

### Anthropic 格式

```bash
curl http://localhost:4000/v1/messages \
  -H "x-api-key: sk-1234567890" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "claude-sonnet-4-20250514",
    "max_tokens": 1024,
    "messages": [{"role": "user", "content": "Hello"}],
    "stream": true
  }'
```

## 项目结构

```
.
├── main.go                 # 程序入口
├── router.go               # 路由定义
├── API/
│   ├── ChatCompletions.go  # OpenAI 格式对话补全
│   ├── Messages.go         # Anthropic 格式消息（含 Web Search）
│   ├── Models.go           # 模型列表
│   ├── CountTokens.go      # Token 计数
│   ├── DebugToken.go       # Token 调试
│   ├── DebugAnthropic2Q.go # 请求映射调试
│   └── NotFound.go         # 404 处理
├── Middleware/
│   └── Auth.go             # Bearer Token 认证中间件
├── Models/
│   ├── Anthropic.go        # Anthropic 数据模型
│   ├── OpenAI.go           # OpenAI 数据模型
│   ├── Q.go                # Amazon Q 数据模型
│   └── Tokens.go           # Token 相关模型
├── Utils/
│   ├── Anthropic2Q.go      # Anthropic → Q 请求转换
│   ├── Openai2Q.go         # OpenAI → Q 请求转换
│   ├── Q2Openai.go         # Q 响应 → OpenAI 格式转换
│   ├── GetBearer.go        # 多账号 Token 管理与轮换
│   ├── Proxy.go            # 代理与 uTLS 配置
│   ├── Validation.go       # 请求校验
│   └── Logger.go           # 日志管理
├── Dockerfile
├── docker-compose.yml
└── .github/workflows/      # CI/CD
```

## License

MIT
