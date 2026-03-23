# go-eino-agent

<div align="center">

Go + Eino + Ark + Milvus 的工程化 RAG 项目，支持流式问答、Agent/Graph 模式、自动联网知识增强、限流与可观测能力。

[![Go](https://img.shields.io/badge/Go-1.24+-00ADD8?logo=go)](https://go.dev/)
[![Gin](https://img.shields.io/badge/Gin-1.10-00B386)](https://github.com/gin-gonic/gin)
[![Eino](https://img.shields.io/badge/Eino-0.7.13-5B6EFF)](https://www.cloudwego.io/docs/eino/)
[![Milvus](https://img.shields.io/badge/Milvus-2.4.x-0099CC)](https://milvus.io/)

</div>

## 1. 项目定位

这个项目把 RAG 的核心链路完整打通：
- 文档上传（Markdown / PDF）-> 分块 -> Embedding -> Milvus 入库
- 查询检索 -> 上下文构建 -> LLM 生成
- 支持 `SSE` 流式输出、`ReAct` / `Multi-Agent` / `Graph` 多模式
- 本地知识不足时自动触发联网搜索，并可回写知识库

## 2. 核心能力

| 模块 | 能力 |
| --- | --- |
| 检索 | 向量检索 + 去重 + 相关度过滤 + 上下文预算 |
| 问答 | 基础 RAG、ReAct、RAG Agent、Multi-Agent、Graph |
| 安全 | CORS 白名单、上传 MIME 校验、IP 限流 |
| 运维 | 启动参数校验、优雅停机、请求日志、内存指标接口 |

## 3. 架构概览

```text
User
  -> Gin API (/api/upload, /api/query, /api/search)
  -> Handler
     -> RAG Service
        -> Embedding (Ark)
        -> Vector DB (Milvus)
        -> Optional Web Search Tool
     -> LLM ChatModel (Ark)
```

## 4. 快速启动

### 4.1 克隆仓库

```bash
git clone https://github.com/Butt3rcup/Agent-Eino.git
cd Agent-Eino
```

### 4.2 配置环境变量

```bash
cp env.example .env
```

最小必填项：

```bash
ARK_API_KEY=your_api_key
MODEL=doubao-1-5-pro-32k-250115
EMBEDDER=ep-xxxxxxxx-xxxx
MILVUS_URI=localhost:19530
MILVUS_DB_NAME=hotwords
MILVUS_COLLECTION_NAME=hotwords_collection
```

### 4.3 启动 Milvus

```bash
docker run -d --name milvus ^
  -p 19530:19530 ^
  -p 9091:9091 ^
  milvusdb/milvus:v2.4.0
```

Linux / macOS 请将 `^` 改为 `\`。

### 4.4 启动服务

```bash
go mod tidy
go run cmd/server/main.go
```

也可以直接使用仓库内脚本：

```powershell
powershell -ExecutionPolicy Bypass -File scripts/run-server.ps1
```

默认访问：
- Web: `http://localhost:8080`
- Health: `http://localhost:8080/api/health`
- Metrics: `http://localhost:8080/api/metrics`

## 5. API 概览

### `GET /api/health`
服务健康状态，返回整体 `ready`、组件可用性、模式开关，以及后台任务队列与查询缓存状态。

### `GET /api/metrics`
内存指标快照（请求总量、状态码分布、延迟、在途请求、运行时长）。

### `POST /api/upload`
文档上传并索引（支持 `.md` / `.markdown` / `.pdf`，含 MIME 校验）。

### `POST /api/search`
向量检索接口。检索不到相关内容时会返回 `matched:false` 且附带 `reason:no_relevant_match`，前端可据此提示用户并避免展示误导性片段。

空结果示例：

```json
{
  "matched": false,
  "reason": "no_relevant_match",
  "count": 0,
  "results": []
}
```

### `POST /api/query`（SSE）
统一问答入口。

请求示例：

```json
{
  "query": "什么是 YYDS？",
  "mode": "rag"
}
```

`mode` 取值：
- `rag`（默认）
- `react`
- `rag_agent`
- `multi-agent`
- `graph_rag`
- `graph_multi`

## 6. 关键配置

### 6.1 服务与安全

| 变量 | 默认值 | 说明 |
| --- | --- | --- |
| `SERVER_PORT` | `8080` | 服务端口 |
| `SERVER_HOST` | `0.0.0.0` | 监听地址 |
| `SERVER_READ_TIMEOUT_SEC` | `15` | 读超时 |
| `SERVER_WRITE_TIMEOUT_SEC` | `60` | 写超时 |
| `SERVER_SHUTDOWN_TIMEOUT_SEC` | `10` | 优雅停机超时 |
| `CORS_ALLOWED_ORIGINS` | `*` | CORS 白名单，逗号分隔 |
| `TRUSTED_PROXIES` | 空 | Gin 可信代理 |
| `RATE_LIMIT_RPS` | `20` | 每 IP 每秒令牌补充 |
| `RATE_LIMIT_BURST` | `40` | 每 IP 桶容量 |

### 6.2 RAG 检索质量

| 变量 | 默认值 | 说明 |
| --- | --- | --- |
| `TOP_K` | `5` | 向量召回数量 |
| `MAX_CONTEXT_DOCS` | `5` | 上下文最多文档数 |
| `MAX_CONTEXT_CHARS` | `4000` | 上下文字符预算 |
| `MAX_SCORE_DELTA` | `1.0` | 相对最佳分数的允许差值（L2） |
| `SIMILARITY_THRESHOLD` | `1.5` | 自动联网触发阈值（L2） |
| `AUTO_SAVE_MIN_CHARS` | `120` | 联网结果自动入库的最小内容长度 |

### 6.3 Milvus 隔离与自动回写

| 变量 | 默认值 | 说明 |
| --- | --- | --- |
| `MILVUS_DB_NAME` | `hotwords` | Milvus 数据库名 |
| `MILVUS_COLLECTION_NAME` | `hotwords_collection` | Milvus 集合名 |

- 自动联网结果会附带 `source_type`、`answer_hash`、`review_status` 等元数据后再入库
- 已存在的联网补充内容会基于稳定哈希去重，避免重复写入和重复向量索引
- 过短或明显不可靠的联网结果会被跳过，不直接污染知识库

## 7. 观测与运维

- 请求日志：方法、路径、状态码、延迟、IP
- 指标接口：`/api/metrics`
- 生命周期：`http.Server + signal` 优雅停机
- 启动时配置强校验，避免运行期再炸

## 8. 本地开发检查

```bash
go test ./...
go vet ./...
```

推荐优先使用仓库脚本统一测试缓存目录：

```powershell
powershell -ExecutionPolicy Bypass -File scripts/test.ps1
```

```bash
bash scripts/test.sh
```

当前仓库已补充部分 `config/parser/rag/middleware/handler` 单测，建议后续继续增加 `graph` 与真实依赖下的集成测试。

## 9. 目录结构

```text
cmd/server/                 # 入口与中间件
config/                     # 配置加载与校验
internal/agent/             # Agent 逻辑
internal/graph/             # Graph 工作流
internal/handler/           # HTTP 接口
internal/rag/               # 检索增强服务
internal/tool/              # 工具实现
pkg/embedding/              # Embedding 封装
pkg/parser/                 # 文档解析
pkg/vectordb/               # Milvus 封装
web/templates/              # 页面模板
```

## 10. 辅助脚本

| 脚本 | 说明 |
| --- | --- |
| `scripts/run-server.ps1` | Windows 下启动服务，并统一设置 `GOPATH`、`GOMODCACHE`、`GOCACHE` |
| `scripts/test.ps1` / `scripts/test.sh` | 运行 `go test ./...` 与 `go vet ./...` |
| `scripts/bench.ps1` / `scripts/bench.sh` | 运行压测与基准相关命令 |
| `scripts/eval.ps1` / `scripts/eval.sh` | 执行评测流程并生成报告 |

对应的命令行工具入口位于 `cmd/agenteval`、`cmd/agentevalreport`、`cmd/benchreport`、`cmd/loadtest`。

## 11. 路线图

- 增加标准化测试基线（单测 + 集成）
- 接入外部监控（Prometheus / OpenTelemetry）
- 多环境配置模板与容器编排优化

---

如果这个项目对你有帮助，欢迎 Star。
