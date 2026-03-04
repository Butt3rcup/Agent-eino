# go-eino-agent

基于 Go + CloudWeGo Eino + 火山引擎 Ark + Milvus 的网络热词 RAG 系统，支持文档上传、语义检索、SSE 流式回答、Agent/Graph 多模式问答与自动联网知识增强。

## 项目描述

`go-eino-agent` 是一个可运行的 RAG 工程化示例，目标是把「本地知识库检索 + LLM 生成 + 工具调用」串成一条完整链路，提供可扩展的后端服务与 Web 交互页面。

核心能力：
- 文档上传与入库（`.md` / `.markdown` / `.pdf`）
- 文档分块、Embedding 向量化、Milvus 检索
- `SSE` 流式响应
- `ReAct`、`RAG Agent`、`Multi-Agent`、`Graph` 多种问答模式
- 本地知识不足时自动联网搜索，并可回写知识库

## 技术栈

- Go `1.23+`
- Gin
- CloudWeGo Eino `v0.7.x`
- 火山引擎 Ark（ChatModel + Embedding）
- Milvus `v2.4.x`

## 项目结构

```text
.
├─ cmd/server/main.go             # 服务入口
├─ config/config.go               # 配置加载
├─ internal/
│  ├─ agent/agent.go              # ReAct / Multi-Agent / RAGAgent
│  ├─ graph/graph.go              # RAGGraph / MultiStageGraph
│  ├─ handler/handler.go          # HTTP API + SSE
│  ├─ rag/service.go              # RAG 检索与智能增强
│  └─ tool/                       # 热词工具与联网搜索工具
├─ pkg/
│  ├─ embedding/service.go        # Embedding 服务
│  ├─ parser/parser.go            # Markdown / PDF 解析与分块
│  └─ vectordb/milvus.go          # Milvus 客户端
├─ web/templates/index.html       # Web 页面
└─ docs/                          # 示例知识文档
```

## 快速开始

### 1. 克隆仓库

```bash
git clone https://github.com/Butt3rcup/Agent-eino.git
cd Agent-eino
```

### 2. 准备环境变量

```bash
cp env.example .env
```

至少需要配置：

```bash
ARK_API_KEY=你的 Ark API Key
EMBEDDER=ep-xxxxxxxx-xxxx
MODEL=doubao-1-5-pro-32k-250115
MILVUS_URI=localhost:19530
SERVER_PORT=8080
```

说明：
- `EMBEDDER` 必须是端点 ID（`ep-...`），不是模型名。
- `EMBEDDING_DIM` 需要和你使用的 Embedding 端点维度一致。

### 3. 启动 Milvus

```bash
docker run -d --name milvus ^
  -p 19530:19530 ^
  -p 9091:9091 ^
  milvusdb/milvus:v2.4.0
```

> Linux / macOS 可将 `^` 改为 `\`。

### 4. 启动服务

```bash
go mod tidy
go run cmd/server/main.go
```

默认访问地址：
- Web 页面：`http://localhost:8080`
- 健康检查：`http://localhost:8080/api/health`

## API 概览

### `GET /api/health`

服务健康检查。

### `POST /api/upload`

上传文档并入库。

`multipart/form-data` 参数：
- `file`: `.md` / `.markdown` / `.pdf`

### `POST /api/search`

语义检索。

请求示例：

```json
{
  "query": "网络流行语"
}
```

### `POST /api/query`（SSE）

统一问答入口，支持多模式。

请求示例：

```json
{
  "query": "什么是 YYDS？",
  "mode": "rag"
}
```

`mode` 可选值：
- `rag`（默认）：基础 RAG
- `react`：ReAct Agent
- `rag_agent`：RAG + Agent
- `multi-agent`：多 Agent 协作
- `graph_rag`：Graph 版 RAG
- `graph_multi`：多阶段 Graph

## 智能知识增强

在 `rag` 流程里，系统会先查本地知识库；当相似度低于阈值时可自动触发联网搜索，并可将搜索结果写回知识库。

相关配置（`.env`）：

```bash
ENABLE_AUTO_SEARCH=true
SIMILARITY_THRESHOLD=1.5
AUTO_SAVE_SEARCH_RESULT=true
```

## 常见问题

### 1) `ARK_API_KEY is required`

未配置 `ARK_API_KEY`，请检查 `.env`。

### 2) Embedding 调用失败

重点确认：
- `EMBEDDER` 是否是 `ep-...`
- 端点能力是否支持文本向量化
- `EMBEDDING_DIM` 是否与端点输出维度一致

### 3) Milvus 连接失败

检查 `MILVUS_URI` 与容器状态：

```bash
docker ps
```

## 开发与测试

当前仓库暂无单元测试文件，基础检查建议：

```bash
go test ./...
go vet ./...
```

## 路线图

- 完善 Agent / Graph 模式的前后端联动体验
- 增加更稳健的输入校验与错误分级
- 补充单元测试与集成测试
- 增加容器化部署与生产配置模板

## 贡献

欢迎提交 Issue / PR：

1. Fork 仓库
2. 新建分支：`feature/xxx`
3. 提交改动并推送
4. 发起 Pull Request

---

如果这个项目对你有帮助，欢迎点个 Star。
