# 🔥 网络热词 RAG 系统

<div align="center">

**基于火山引擎 Ark 和 Eino 框架的智能检索增强生成系统**

[![Version](https://img.shields.io/badge/version-1.0.0--ark-blue.svg)](https://github.com/yourusername/go-eino-agent)
[![Go](https://img.shields.io/badge/Go-1.23-00ADD8.svg)](https://golang.org/)
[![Eino](https://img.shields.io/badge/Eino-0.7.13-purple.svg)](https://www.cloudwego.io/docs/eino/)
[![License](https://img.shields.io/badge/license-MIT-green.svg)](LICENSE)

[功能特性](#-功能特性) • [快速开始](#-快速开始) • [配置说明](#-配置说明) • [API 文档](#-api-文档) • [技术栈](#-技术栈)

</div>

---

## 📖 项目介绍

这是一个基于火山引擎 Ark（豆包大模型）和 CloudWeGo Eino 框架构建的智能 RAG（检索增强生成）系统。系统支持文档上传、向量化存储、语义搜索和智能问答，并提供**流式实时响应**的现代化 Web 界面。

### 🎯 核心优势

- **🌊 流式响应**: 采用 SSE 技术，实时逐字显示 AI 回答（打字机效果）
- **🚀 高性能**: 基于 CloudWeGo Eino 框架，性能优异
- **💎 现代化 UI**: 渐变主题、流畅动画、响应式设计
- **🧠 智能问答**: RAG 技术结合豆包大模型，准确理解上下文
- **🌐 智能知识增强**: 本地知识不足时自动联网搜索，知识库自动积累
- **📚 文档管理**: 支持 Markdown 和 PDF 文档上传和索引
- **🔍 语义搜索**: 基于向量相似度的智能检索

---

## ✨ 功能特性

### 核心功能

| 功能        | 说明                        | 状态 |
| ----------- | --------------------------- | ---- |
| 📁 文档上传 | 支持 Markdown、PDF 文件上传 | ✅   |
| 🔄 自动索引 | 文档自动分块、向量化并存储  | ✅   |
| 🔍 向量搜索 | 基于语义的相似度检索        | ✅   |
| 💬 智能问答 | RAG + 豆包大模型的问答系统  | ✅   |
| 🌊 流式返回 | 实时逐字显示 AI 回答        | ✅   |
| 🌐 **智能知识增强** | **自动联网搜索并积累知识**  | ✅   |
| 🎨 美观界面 | 现代化 Web UI，带动画效果   | ✅   |
| 🛠️ 工具支持 | 热词搜索、趋势分析等工具    | 🚧   |
| 🤖 Agent 模式 | ReAct、多Agent协作         | 🚧   |
| 📊 Graph 模式 | 多阶段推理流程             | 🚧   |

**说明**:
- ✅ 已完成并测试
- 🚧 组件已开发，待集成（Eino v0.7.13 API 兼容性调整中）

### 界面特性

- **流式打字机效果**: AI 回答逐字实时显示
- **闪烁光标动画**: 跟随最新输出的视觉反馈
- **自动滚动**: 内容自动滚动到底部
- **拖拽上传**: 支持文件拖放上传
- **响应式设计**: 完美适配桌面和移动设备
- **动画效果**: 淡入、滑动、弹跳等流畅动画

---

## 🚀 快速开始

### 前置要求

- Go 1.23+
- Docker（用于运行 Milvus）
- 火山引擎 Ark API Key

### 1️⃣ 克隆项目

```bash
git clone https://github.com/yourusername/go-eino-agent.git
cd go-eino-agent
```

### 2️⃣ 配置环境变量

复制并编辑 `.env` 文件：

```bash
cp .env.example .env
```

**必须修改的配置**:

```bash
# 火山引擎 API 配置
ARK_API_KEY=你的实际API密钥
EMBEDDER=ep-xxxxxxxx-xxxx  # 你的文本 Embedding 端点 ID
```

**完整配置示例**:

```bash
# 火山引擎 API 配置
ARK_API_KEY=xxx
ARK_BASE_URL=https://ark.cn-beijing.volces.com/api/v3
ARK_REGION=cn-beijing

# 模型配置
MODEL=doubao-1-5-pro-32k-250115        # ChatModel 模型
EMBEDDER=ep-xxx-xxx       # Embedding 端点 ID

# Milvus 向量数据库
MILVUS_URI=localhost:19530
MILVUS_DB_NAME=hotwords

# 服务器配置
SERVER_PORT=8080
SERVER_HOST=0.0.0.0

# RAG 配置
EMBEDDING_DIM=2048      # 多模态模型维度
CHUNK_SIZE=500
CHUNK_OVERLAP=50
TOP_K=5

# 智能知识增强配置
ENABLE_AUTO_SEARCH=true          # 是否启用自动联网搜索
SIMILARITY_THRESHOLD=1.0         # 相似度阈值（L2距离，越小越相似）
AUTO_SAVE_SEARCH_RESULT=true    # 是否自动保存搜索结果到知识库

# LLM 配置
LLM_TEMPERATURE=0.7
LLM_MAX_TOKENS=2000
```

### 3️⃣ 启动 Milvus

使用 Docker 启动 Milvus 向量数据库：

```bash
docker run -d --name milvus \
  -p 19530:19530 \
  -p 9091:9091 \
  milvusdb/milvus:v2.4.0
```

### 4️⃣ 安装依赖

```bash
go mod download
```

### 5️⃣ 启动服务器

```bash
# 方式一：直接运行
go run cmd/server/main.go

# 方式二：编译后运行
go build -o server.exe cmd/server/main.go
./server.exe
```

### 6️⃣ 访问系统

打开浏览器访问：**http://localhost:8080**

---

## ⚙️ 配置说明

### 火山引擎 Ark 配置

#### 获取 API Key

1. 访问 [火山引擎控制台](https://console.volcengine.com/ark)
2. 创建 API Key
3. 复制 API Key 到 `.env` 的 `ARK_API_KEY` 字段

#### 配置 Embedding 端点

**⚠️ 重要**: `EMBEDDER` 字段需要填写**端点 ID**，而不是模型名称。

**正确配置步骤**:

1. 登录 [火山引擎控制台](https://console.volcengine.com/ark)
2. 进入 "推理接入" → "端点管理"
3. 创建或查找绑定了**文本 Embedding 模型**的端点：
   - ✅ 推荐: `doubao-embedding-large`
   - ✅ 可选: `doubao-embedding`
   - ❌ 不要使用: `doubao-embedding-vision-*`（视觉模型不支持纯文本）
4. 复制端点 ID（格式: `ep-xxxxxxxx-xxxx`）
5. 填入 `.env` 的 `EMBEDDER` 字段

**示例**:

```bash
# ✅ 正确
EMBEDDER=ep-xxx-xxx

# ❌ 错误（这是模型名称，不是端点 ID）
EMBEDDER=doubao-embedding
```

#### 多模态端点配置

如果使用多模态 Embedding 端点（支持文本+图像），需要：

1. 确保端点绑定的模型支持纯文本输入
2. 代码已自动配置 `APIType: MultiModal`
3. 向量维度设置为 **2048**（而非 1536）

### Milvus 配置

| 参数             | 说明            | 默认值            |
| ---------------- | --------------- | ----------------- |
| `MILVUS_URI`     | Milvus 服务地址 | `localhost:19530` |
| `MILVUS_DB_NAME` | 数据库名称      | `hotwords`        |
| `EMBEDDING_DIM`  | 向量维度        | `2048`            |

**注意**: 向量维度必须与 Embedding 模型输出维度一致：

- 标准文本模型: 1536 维
- 多模态模型: 2048 维

### 服务器配置

| 参数              | 说明         | 默认值            |
| ----------------- | ------------ | ----------------- |
| `SERVER_PORT`     | 服务器端口   | `8080`            |
| `SERVER_HOST`     | 监听地址     | `0.0.0.0`         |
| `MAX_UPLOAD_SIZE` | 最大上传大小 | `10485760` (10MB) |

---

## 📡 API 文档

### 1. 健康检查

```http
GET /api/health
```

**响应**:

```json
{
  "status": "ok",
  "version": "1.0.0-ark"
}
```

---

### 2. 文档上传

```http
POST /api/upload
Content-Type: multipart/form-data
```

**参数**:

- `file`: 文件（.md 或 .pdf）

**响应**:

```json
{
  "message": "文件上传并索引成功",
  "filename": "1234567890_hotwords_knowledge.md"
}
```

**示例**:

```bash
curl -X POST http://localhost:8080/api/upload \
  -F "file=@docs/hotwords_knowledge.md"
```

---

### 3. 智能问答（流式）

```http
POST /api/query
Content-Type: application/json
```

**请求体**:

```json
{
  "query": "什么是YYDS？"
}
```

**响应** (SSE 流式):

```
event:message
data:{"content":"\""}

event:message
data:{"content":"YYDS"}

event:message
data:{"content":"\""}

...

event:done
data:{"message":"完成"}
```

**示例**:

```bash
curl -N -X POST http://localhost:8080/api/query \
  -H "Content-Type: application/json" \
  -d '{"query":"什么是YYDS？"}'
```

---

### 4. 向量搜索

```http
POST /api/search
Content-Type: application/json
```

**请求体**:

```json
{
  "query": "网络流行语"
}
```

**响应**:

```json
{
  "results": [
    {
      "content": "网络热词知识库...",
      "metadata": "filename:hotwords.md",
      "score": 0.95
    }
  ]
}
```

**示例**:

```bash
curl -X POST http://localhost:8080/api/search \
  -H "Content-Type: application/json" \
  -d '{"query":"网络流行语"}'
```

---

## 🛠️ 技术栈

### 后端框架

| 技术                                        | 版本    | 用途         |
| ------------------------------------------- | ------- | ------------ |
| [Go](https://golang.org/)                   | 1.23    | 编程语言     |
| [Eino](https://www.cloudwego.io/docs/eino/) | v0.7.13 | LLM 应用框架 |
| [Gin](https://github.com/gin-gonic/gin)     | v1.10.0 | Web 框架     |

### AI 服务

| 服务                                                  | 版本           | 用途       |
| ----------------------------------------------------- | -------------- | ---------- |
| [火山引擎 Ark](https://www.volcengine.com/docs/82379) | -              | LLM 平台   |
| Ark Embedding                                         | v0.1.1         | 文本向量化 |
| Ark ChatModel                                         | v0.1.65        | 对话生成   |
| 豆包大模型                                            | doubao-1-5-pro | 智能问答   |

### 数据存储

| 技术                         | 版本   | 用途       |
| ---------------------------- | ------ | ---------- |
| [Milvus](https://milvus.io/) | v2.4.0 | 向量数据库 |

### 前端技术

- 原生 JavaScript (ES6+)
- Server-Sent Events (SSE)
- CSS3 动画和渐变
- 响应式设计

---

## 📂 项目结构

```
.
├── cmd/
│   └── server/
│       └── main.go              # 服务器入口
│
├── internal/
│   ├── handler/
│   │   └── handler.go           # HTTP 请求处理（含流式返回）
│   ├── rag/
│   │   └── service.go           # RAG 核心逻辑
│   ├── agent/
│   │   └── agent.go             # Agent 实现（ReAct, MultiAgent, RAGAgent）
│   ├── graph/
│   │   └── graph.go             # Graph 实现（RAGGraph, MultiStageGraph）
│   └── tool/
│       └── hotword_tools.go     # 工具实现（热词搜索、趋势分析、解释）
│
├── pkg/
│   ├── embedding/
│   │   └── service.go           # Ark Embedding 服务
│   ├── vectordb/
│   │   └── milvus.go            # Milvus 客户端
│   └── parser/
│       └── parser.go            # 文档解析（纯 Go）
│
├── config/
│   └── config.go                # 配置管理
│
├── web/
│   ├── templates/
│   │   └── index.html           # 主页面（流式 UI + 模式选择器）
│   └── static/                  # 静态资源
│
├── docs/
│   └── test_hotwords.md         # 测试文档
│
├── .env.example                 # 环境变量模板
├── .env                         # 环境变量（需配置）
├── CONFIG.md                    # 详细配置指南
├── go.mod                       # Go 依赖
├── go.sum                       # 依赖校验
└── README.md                    # 本文件
```

**说明**:
- `internal/agent/`, `internal/graph/`, `internal/tool/` 包含高级功能组件
- 当前版本只有 `handler.go`, `rag/`, `embedding/`, `vectordb/` 完全集成
- Agent 和 Graph 组件待适配 Eino v0.7.13 API 后集成

---

## 🎨 使用指南

### 上传文档

1. 准备 Markdown 或 PDF 文件
2. 在界面点击"文档上传"区域或拖拽文件
3. 等待上传和索引完成

### 智能问答

1. 在"智能问答"区域选择查询模式（当前支持基础 RAG）
2. 输入你的问题
3. 点击"🚀 开始查询"按钮
4. 观看实时流式回答：
   - 文字逐字显示（打字机效果）
   - 闪烁光标跟随
   - 自动滚动

### 查询模式说明

系统提供多种查询模式接口，当前版本支持：

| 模式 | 说明 | 状态 |
|------|------|------|
| **🎯 基础 RAG** | 快速检索回答 | ✅ 可用 |
| **🤖 ReAct Agent** | 智能工具调用 | 🚧 开发中 |
| **📚 RAG Agent** | 增强型检索 | 🚧 开发中 |
| **📊 Multi-Stage** | 多阶段推理 | 🚧 开发中 |

**当前实现**:
- ✅ 基础 RAG 模式已完全实现，支持流式响应
- 🚧 其他模式的前端选择器和后端接口已准备，核心组件正在适配 Eino v0.7.13 API

**基础 RAG 模式**工作流程：
1. 接收用户查询
2. 从 Milvus 向量数据库检索相关文档片段（智能检索）
3. **如果本地知识充足**：将检索结果和问题一起发送给豆包大模型
4. **如果本地知识不足**：自动触发联网搜索，获取最新信息并保存到知识库
5. 流式返回 AI 生成的回答

> 💡 **智能知识增强**: 系统会自动判断本地知识是否充足，必要时联网搜索并积累知识。详见 [智能知识增强系统使用指南](docs/智能知识增强系统使用指南.md)

### 推荐问题示例

```
• 什么是YYDS？
• emo是什么意思？
• 解释一下网络热词的含义
• 有哪些常见的网络流行语？
• 内卷这个词是什么意思？
• 破防的起源和用法
```

---

## 🐛 常见问题

### 1. "ARK_API_KEY is required"

**原因**: 缺少火山引擎 API Key

**解决**:

```bash
# 编辑 .env 文件
ARK_API_KEY=你的实际API密钥
```

---

### 2. "model doubao-embedding-xxx does not support this api"

**原因**: EMBEDDER 配置错误

**解决**:

1. 确保 `EMBEDDER` 填写的是**端点 ID**（格式: `ep-xxxxxxxx-xxxx`）
2. 确保端点绑定的是**文本 Embedding 模型**，不是视觉模型
3. 在火山引擎控制台查看端点详情确认

**正确示例**:

```bash
EMBEDDER=ep-xxx-xxx  # ✅ 端点 ID
```

**错误示例**:

```bash
EMBEDDER=doubao-embedding         # ❌ 模型名称
```

---

### 3. "failed to create milvus client"

**原因**: Milvus 未启动或端口被占用

**解决**:

```bash
# 检查 Milvus 是否运行
docker ps | grep milvus

# 如未运行，启动 Milvus
docker run -d --name milvus \
  -p 19530:19530 \
  milvusdb/milvus:v2.4.0
```

---

### 4. 向量维度不匹配

**原因**: EMBEDDING_DIM 配置与模型输出不一致

**解决**:

```bash
# 标准文本模型
EMBEDDING_DIM=1536

# 多模态模型
EMBEDDING_DIM=2048
```

重建 Milvus 集合：

```bash
# 删除旧数据
docker exec -it milvus bash
# 在容器内删除数据后重启服务器
```

---

### 5. 流式返回不显示

**原因**: 浏览器缓存或代理问题

**解决**:

1. 清除浏览器缓存
2. 使用 Ctrl+Shift+R 强制刷新
3. 检查浏览器控制台是否有错误

## 📈 性能优化

### 推荐配置

**开发环境**:

```bash
CHUNK_SIZE=500
CHUNK_OVERLAP=50
TOP_K=5
LLM_TEMPERATURE=0.7
```

**生产环境**:

```bash
CHUNK_SIZE=1000        # 更大的分块
CHUNK_OVERLAP=100      # 更多重叠
TOP_K=10              # 更多检索结果
LLM_TEMPERATURE=0.5    # 更确定的输出
```

### 缓存策略

- Milvus 自带索引缓存
- 考虑添加 Redis 缓存热门查询
- 使用 CDN 加速静态资源

---

## 🔐 安全建议

1. **API Key 管理**
   - 不要将 API Key 提交到版本控制
   - 使用环境变量或密钥管理服务

2. **访问控制**
   - 生产环境添加身份验证
   - 限制上传文件大小和类型
   - 实施 API 速率限制

3. **数据安全**
   - 敏感文档加密存储
   - 定期备份向量数据库
   - 使用 HTTPS 加密传输

---

## 📝 版本历史

### v1.0.0-ark-extended (2026-03-03)

**🎉 扩展版本**

**已完成功能**:
- ✅ 完整集成火山引擎 Ark（豆包大模型）
- ✅ 实现流式 SSE 响应
- ✅ 美化 UI 界面，添加查询模式选择器
- ✅ 支持多模态 Embedding (2048维)
- ✅ 完整的基础 RAG 功能
- ✅ **智能知识增强系统**（自动联网搜索 + 知识积累）
- ✅ 工具系统（热词搜索、趋势分析、热词解释、联网搜索）
- ✅ Tool 接口适配 Eino v0.7.13 API

**开发中功能**:
- 🚧 ReAct Agent 模式（工具调用）
- 🚧 Multi-Agent 协作系统
- 🚧 RAG Agent 增强检索
- 🚧 Multi-Stage Graph 多阶段推理
- 🚧 前端与后端 Agent/Graph 组件集成

**技术细节**:
- Eino v0.7.13
- Ark Embedding v0.1.1 (多模态端点)
- Ark ChatModel v0.1.65 (doubao-1-5-pro-32k-250115)
- Milvus v2.4.0
- 向量维度: 2048

**已知问题**:
- Agent 和 Graph 组件需要适配 Eino v0.7.13 的新 API
- 当前只有基础 RAG 模式完全可用
- 其他查询模式会自动降级到 RAG 模式

### v1.0.0-ark (2026-03-02)

**🎉 首个 Ark 版本**

- ✅ 完整集成火山引擎 Ark（豆包大模型）
- ✅ 实现流式 SSE 响应
- ✅ 美化 UI 界面
- ✅ 支持多模态 Embedding
- ✅ 完整的 RAG 功能

**技术细节**:
- Eino v0.7.13
- Ark Embedding v0.1.1
- Ark ChatModel v0.1.65
- Milvus v2.4.0

---

## 🗺️ 开发路线图

### 短期计划 (Q1 2026)

- [ ] 适配 Eino v0.7.13 Agent API
- [ ] 集成 ReAct Agent 模式
- [ ] 集成 Multi-Agent 协作系统
- [ ] 集成 Multi-Stage Graph 推理流程
- [ ] 完善工具调用功能

### 中期计划 (Q2 2026)

- [ ] 添加更多专业领域工具
- [ ] 支持更多文档格式（Word、Excel等）
- [ ] 实现会话历史管理
- [ ] 添加用户认证和权限管理
- [ ] 优化向量检索性能

### 长期计划 (H2 2026)

- [ ] 多租户支持
- [ ] 分布式部署方案
- [ ] 企业级监控和日志
- [ ] API 速率限制和配额管理
- [ ] 移动端适配

---

## 🤝 贡献指南

欢迎贡献！请遵循以下步骤：

1. Fork 本仓库
2. 创建特性分支 (`git checkout -b feature/AmazingFeature`)
3. 提交更改 (`git commit -m 'Add some AmazingFeature'`)
4. 推送到分支 (`git push origin feature/AmazingFeature`)
5. 开启 Pull Request

---

## 📄 License

本项目采用 MIT 许可证。详见 [LICENSE](LICENSE) 文件。

---

## 📚 扩展文档

- [智能知识增强系统使用指南](docs/智能知识增强系统使用指南.md) - 详细的功能说明、配置指南和使用示例
- [智能知识增强系统技术实现](docs/智能知识增强系统技术实现.md) - 架构设计、核心组件和技术细节

## 🔗 相关链接

- [Eino 官方文档](https://www.cloudwego.io/docs/eino/)
- [火山引擎 Ark 文档](https://www.volcengine.com/docs/82379)
- [Milvus 文档](https://milvus.io/docs)
- [Gin 框架文档](https://gin-gonic.com/docs/)

---

## 💬 联系方式

- 问题反馈: [GitHub Issues](https://github.com/yourusername/go-eino-agent/issues)
- 功能建议: [GitHub Discussions](https://github.com/yourusername/go-eino-agent/discussions)

---

<div align="center">

**⭐ 如果这个项目对你有帮助，请给个星标！**

Made with ❤️ by CloudWeGo Community

</div>
---

## 查询模式对照表

| mode 值        | 能力说明                               | 返回形式        |
| -------------- | -------------------------------------- | --------------- |
| ag (默认)   | 传统 RAG，上下文来自 Milvus + Ark      | SSE             |
| eact        | ReAct Agent，可调用热词检索/趋势工具   | SSE             |
| ag_agent    | 先检索再走 ReAct Agent，一次性推送结果 | SSE（单次推送） |
| multi-agent  | 搜索/分析/解读多 Agent 协作            | SSE（单次推送） |
| graph_rag    | 基于 compose.Graph 的 RAG Pipeline     | SSE             |
| graph_multi  | 多阶段 Graph（意图识别+工具+总结）     | SSE（单次推送） |

所有模式均复用 /api/query，只需在请求体内设置 mode 字段即可切换。
