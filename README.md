# InstallClaw - 智能安装助手

> **基于 Go 语言跨平台零依赖特性，打造的 AI Agent 智能安装助手**
>
> 帮助用户解决复杂的环境部署、软件安装、配置等场景，让软件安装从"手动执行命令"升级为"智能对话解决"。

[![Go Version](https://img.shields.io/badge/Go-1.21+-00ADD8?style=flat&logo=go)](https://golang.org)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Platform](https://img.shields.io/badge/Platform-Linux%20%7C%20macOS%20%7C%20Windows-lightgrey)](https://github.com)

---

## 🎯 为什么选择 InstallClaw？

### 传统安装方式的痛点

```
❌ 手动查文档、复制命令 → 环境不同导致失败
❌ 依赖缺失 → 一层层安装，耗时耗力
❌ 系统版本兼容性 → CentOS 7 vs Ubuntu 22.04，命令完全不同
❌ 错误无法理解 → 只能搜索解决方案，试错成本高
❌ 配置复杂 → 多个软件协同配置，容易遗漏
```

### InstallClaw 的解决方案

```
✅ 一句话描述需求 → AI 自动规划安装方案
✅ 依赖树递归检测 → 自动安装缺失依赖
✅ 环境预检 → 提前发现兼容性问题
✅ LLM 智能错误恢复 → 理解错误根因，自动尝试修复
✅ 跨平台一致性 → 同一命令，适配所有系统
```

---

## ✨ 核心特性

### 🤖 AI Agent 智能安装

```bash
# 传统方式：查文档、复制命令、解决依赖、处理错误...
# InstallClaw：一句话搞定

installer install claude-code    # 自动检测并安装 Node.js 依赖
installer install docker         # 自动适配 CentOS/Ubuntu/macOS
installer install "开发环境"     # 理解语义，安装常用开发工具
```

### 🔍 环境预检与依赖树验证

安装前自动递归检测依赖兼容性：

```
用户请求安装 Claude Code (CentOS 7)
        │
        ▼
┌─────────────────────────────────┐
│ 依赖树验证:                      │
│ Claude Code                     │
│   └── Node.js >= 18             │
│         └── glibc >= 2.28 ❌    │
│               当前: 2.17        │
└─────────────────────────────────┘
        │
        ▼
提前警告并提供替代方案：
"您的系统 glibc 2.17 不支持 Node.js 18+
 建议：使用 Node.js 16.x 或升级系统"
```

### 🧠 LLM 驱动的智能错误恢复

错误处理从"硬编码规则"升级为"LLM 智能决策"：

```
命令失败 → LLM 分析错误 + 历史上下文 → 返回决策:
  - should_continue: 是否继续尝试
  - next_action: try_fix | try_alternative | abort | skip
  - commands: 要执行的修复命令
  - confidence: 置信度

修复失败 → 递归调用 LLM (带更多历史) → 直到成功或 LLM 决定 abort
```

### 💬 多轮自然语言对话

安装失败时，用户可以直接用自然语言描述解决方案：

```
❌ Installation step failed: install nodejs
   Command: yum install nodejs

How would you like to proceed?
  [1] Apply fix #1 - Use vault.centos.org (risk: low)
  [2] Apply fix #2 - Skip installation (risk: low)
  [n] Describe your solution in natural language  ← 选择此项
  [c] Enter custom commands
  [r] Retry the failed step
  [a] Abort installation

Your choice: n

💬 Describe your solution: try using nvm to install node

🤖 AI 解析并执行:
   → curl -o- https://raw.githubusercontent.com/nvm-sh/nvm/v0.39.0/install.sh | bash
   → source ~/.nvm/nvm.sh && nvm install node

⚠️ Command failed: nvm: command not found

How would you like to proceed?
  [c] Continue with more natural language input
  [r] Retry (let AI try again)
  [a] Abort the installation

Your choice: c

💬 Provide more guidance: source nvm.sh first

🤖 AI 根据历史上下文重新生成:
   → source ~/.nvm/nvm.sh && nvm install node

✅ Success! Retrying original command...
```

**特性：**
- AI 自动将自然语言转换为可执行命令
- 支持多轮对话，逐步调试
- 保留完整历史上下文
- AI 学习失败的尝试，不重复错误

### 🛡️ 命令安全验证

所有通过 Agent 执行的命令都会经过安全检查：

**安全级别：**

| 级别 | 行为 | 示例 |
|------|------|------|
| 🚫 **禁止** | 永不执行 | `rm -rf /`, `dd if=/dev/zero of=/dev/sda` |
| ⚠️ **危险** | 需用户确认 | `apt remove`, `systemctl disable` |
| ⚡ **警告** | 记录后执行 | `sudo`, `rm -rf`, `curl \| bash` |
| ✅ **安全** | 自动执行 | 普通安装命令 |

**禁止的命令类型：**
```
┌─────────────────────────────────────────────────────────┐
│  磁盘/系统破坏                                           │
│  • rm -rf /, rm -rf /*, rm -rf /bin /etc /usr ...      │
│  • dd if=/dev/zero of=/dev/sda                         │
│  • mkfs /dev/sda, fdisk /dev/sda                       │
├─────────────────────────────────────────────────────────┤
│  下载并执行 (无验证)                                     │
│  • curl ... | bash, wget ... | sh                      │
├─────────────────────────────────────────────────────────┤
│  关键系统文件                                            │
│  • > /etc/passwd, chmod 777 /etc/shadow                │
├─────────────────────────────────────────────────────────┤
│  网络攻击                                                │
│  • iptables -F, iptables -P INPUT DROP                 │
└─────────────────────────────────────────────────────────┘
```

**保护路径：** `/bin`, `/etc`, `/usr`, `/boot`, `/dev`, `/proc`, `/sys`, `/root`, `/home`

### 🌍 跨平台零依赖

| 特性 | 说明 |
|------|------|
| **单文件部署** | 编译后一个二进制文件，无需运行时环境 |
| **跨平台支持** | Linux / macOS / Windows 原生支持 |
| **零外部依赖** | 不依赖 Python、Node.js 等任何运行时 |
| **嵌入式配置** | 默认配置打包到二进制，开箱即用 |

### 📦 功能清单

- 🤖 **智能安装** - AI Agent 自主规划并执行软件安装流程
- 🔍 **智能查询** - AI 驱动的软件信息搜索
- 📦 **本地检测** - 自动检测已安装软件状态
- 🔒 **安全验证** - 自动验证下载源安全性
- 📦 **依赖解析** - 自动解析并安装前置依赖
- 🌍 **发行版适配** - 自动识别 CentOS/Ubuntu/Debian 等，使用正确的包管理器
- 🔧 **智能错误恢复** - LLM 分析错误原因，自动尝试修复
- 💬 **多轮自然语言对话** - 用户可用自然语言描述解决方案，支持多轮对话
- 🛡️ **命令安全验证** - 防止执行危险命令，保护系统核心文件
- ⏱️ **智能超时** - 监控进程活动，避免误杀长时间运行的命令
- 🔎 **多搜索引擎** - 支持 Tavily、DuckDuckGo 搜索
- 📚 **自动学习** - 成功安装后自动生成记忆文件

---

## 🚀 快速开始

### 安装

从 [Releases](https://github.com/kyd-w/installclaw/releases) 下载对应平台的二进制文件，或自行编译：

```bash
# 克隆仓库
git clone https://github.com/kyd-w/installclaw.git
cd installclaw

# 编译
go build -o installer ./cmd/universal-installer

# 安装到系统路径 (Linux/macOS)
sudo mv installer /usr/local/bin/
```

### 基本使用

```bash
# 安装软件
installer install nodejs
installer install python
installer install docker

# 模拟安装（查看将要执行的操作）
installer install redis --dry-run

# 搜索软件
installer search database
installer search 微信

# 查看软件详情
installer info nodejs
```

---

## 📖 使用场景

### 场景 1：新服务器环境初始化

```bash
# 一键安装开发环境
installer install nodejs
installer install python
installer install golang
installer install docker
installer install git
```

### 场景 2：复杂依赖软件安装

```bash
# Claude Code 需要 Node.js 18+
# InstallClaw 自动检测并安装依赖
installer install claude-code

# 输出示例：
# ✅ Checking dependencies...
# 📦 Node.js not found, installing...
# ✅ Node.js 20.x installed
# ✅ Claude Code installed successfully
```

### 场景 3：老旧系统兼容性处理

```bash
# CentOS 7 安装 Node.js
installer install nodejs

# 输出示例：
# ⚠️ Environment Pre-check:
#    CentOS 7 has glibc 2.17, Node.js 18+ requires glibc 2.28
#    Using Node.js 16.x (compatible version)
# ✅ Node.js 16.x installed via nvm
```

### 场景 4：安装失败智能恢复

```
安装失败 → LLM 分析：
  "错误类型: repo_unavailable"
  "根因: CentOS 7 EOL，mirrorlist.centos.org 已下线"
  "建议: 替换为 vault.centos.org"

→ 自动执行修复命令
→ 重试安装
→ 成功 ✅
```

---

## ⚙️ 配置

### 配置文件

支持以下位置（按优先级）：
1. `./installer.yaml` - 当前目录
2. `<exe-dir>/installer.yaml` - 可执行文件目录
3. `--config /path/to/config.yaml` - 命令行指定

### AI Provider 配置

```yaml
ai:
  primary: openai

  openai:
    api_key: your-api-key
    model: gpt-4o-mini
    base_url: https://api.openai.com/v1

  claude:
    api_key: ${ANTHROPIC_API_KEY}
    model: claude-3-haiku-20240307
```

### Web Search 配置

```yaml
web_search:
  primary: tavily  # 或 duckduckgo

  tavily:
    api_key: ${TAVILY_API_KEY}

  duckduckgo:
    enabled: true
```

---

## 🏗️ 架构设计

```
┌─────────────────────────────────────────────────────────────┐
│                        CLI (main.go)                        │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│                    AI Agent (agent/)                        │
│  ┌─────────────┐ ┌─────────────┐ ┌─────────────────────────┐│
│  │   Install   │ │   Tool      │ │    Provider Adapter     ││
│  │   Agent     │ │   Registry  │ │    (OpenAI/Claude/etc)  ││
│  └─────────────┘ └─────────────┘ └─────────────────────────┘│
└─────────────────────────────────────────────────────────────┘
        │                    │                    │
        ▼                    ▼                    ▼
┌──────────────┐    ┌──────────────┐    ┌──────────────┐
│ Dependencies │    │   System     │    │   Metadata   │
│   Validator  │    │   Detector   │    │   Registry   │
│              │    │              │    │              │
│ - 递归验证    │    │ - Windows    │    │ - 内置知识库  │
│ - 依赖树     │    │ - Linux      │    │ - 用户扩展   │
│ - 环境预检   │    │ - macOS      │    │ - 自动学习   │
└──────────────┘    └──────────────┘    └──────────────┘
```

---

## 📁 项目结构

```
installclaw/
├── cmd/universal-installer/main.go    # CLI 入口
├── pkg/
│   ├── core/
│   │   ├── agent/                     # AI Agent 核心
│   │   │   ├── agent.go               # 安装 Agent
│   │   │   ├── tools.go               # 工具实现
│   │   │   ├── types.go               # 类型定义
│   │   │   ├── safety.go              # 命令安全验证 🆕
│   │   │   └── process_monitor_*.go   # 进程监控
│   │   ├── dependencies/              # 依赖验证模块
│   │   │   ├── types.go               # 类型定义
│   │   │   ├── validator.go           # 环境验证器
│   │   │   ├── loader.go              # 多源加载器
│   │   │   ├── learner.go             # 自动学习
│   │   │   └── configs/               # 内置知识库
│   │   ├── config/                    # 配置管理
│   │   ├── installer/                 # 安装器
│   │   ├── system/                    # 系统检测
│   │   └── ...
│   ├── providers/                     # LLM Provider
│   └── tools/                         # 工具实现
└── configs/packages/                  # 预定义软件包
```

---

## 🔧 开发

### 构建

```bash
# 当前平台
go build -o installer ./cmd/universal-installer

# 所有平台
GOOS=linux GOARCH=amd64 go build -o installer-linux-amd64 ./cmd/universal-installer
GOOS=darwin GOARCH=arm64 go build -o installer-darwin-arm64 ./cmd/universal-installer
GOOS=windows GOARCH=amd64 go build -o installer-windows-amd64.exe ./cmd/universal-installer
```

### 测试

```bash
go test ./...
```

---

## 📝 License

[MIT License](LICENSE)

---

## 🙏 致谢

- Go 语言 - 跨平台零依赖的基础
- OpenAI / Claude / Ollama - AI 能力支持
- 所有开源软件的贡献者

---

## ⚠️ 安全说明

- 本工具会从互联网下载并执行软件安装脚本，请确保在可信环境中使用
- 所有命令执行前都会经过**安全验证模块**检查
- 危险命令（如 `rm -rf /`）会被**自动拦截**
- 系统核心文件（`/etc/passwd`, `/etc/shadow` 等）受**写入保护**
- 可通过配置自定义安全策略和受保护路径

---

**Made with ❤️ by kyd-w**
