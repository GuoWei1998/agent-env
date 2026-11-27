# 总体设计

agent-env 给 Agent 提供一个**用完即弃的隔离环境**：申请 → 等它可用 → 在里面跑命令、读写文件 → 释放。

本文先讲现在已经做了什么，再讲最终要做成什么样。交付顺序见[迭代计划](iteration-plan.md)，为什么这么选型见[技术选型](technology.md)。

## 1. 三个核心概念

| 概念 | 含义 | 现状 |
| --- | --- | --- |
| Sandbox（沙箱） | 一个隔离环境，由平台生成的 32 位十六进制 ID 标识 | 已实现 |
| Template（模板） | 沙箱的"出厂配置"：镜像、资源上限、安全设置 | 只有 `linux` 一种 |
| Provider（后端） | 真正创建和销毁沙箱的实现，对上层暴露同一套接口 | `memory`（模拟）、`kube`（真实 Pod） |

## 2. 当前架构（v1）

```
调用方（curl / pkg/client）
        │  HTTP + JSON
        ▼
   internal/api          只做 HTTP ↔ Service 的翻译
        │
        ▼
 internal/sandbox        Service：校验模板、生成 ID；定义 Provider 接口和状态
        │
   ┌────┴─────────────┐
   ▼                  ▼
provider/memory   provider/kube
进程内 map         kubectl → Kubernetes Pod
```

| 目录 | 职责 |
| --- | --- |
| `cmd/manager` | 解析参数、选择 Provider、启动 HTTP 服务，收到退出信号后优雅停止 |
| `internal/api` | 路由、JSON 编解码、把错误映射成状态码 |
| `internal/sandbox` | 数据模型、状态常量、`Provider` 接口、`Service` |
| `internal/provider/memory` | 只保存记录，不启动任何东西；用于无集群时测试接口契约 |
| `internal/provider/kube` | 一个沙箱对应一个加固过的 Pod |
| `pkg/client` | Go 客户端：`Create`、`Get`、`WaitReady`、`Close` |

依赖方向只有一条：`api → sandbox ← provider`。新增后端只需实现 `Provider` 的三个方法，不用改 API 层。

## 3. HTTP 接口

| 接口 | 成功 | 说明 |
| --- | --- | --- |
| `GET /healthz` | 200 | 只表示 manager 进程活着，不代表任何沙箱可用 |
| `POST /v1/sandboxes` | 201 + `Location` | 请求体 `{"template":"linux"}`。201 表示对象已建立，**不代表可以执行命令** |
| `GET /v1/sandboxes/{id}` | 200 | 返回当前状态；不存在返回 404 |
| `DELETE /v1/sandboxes/{id}` | 204 | 幂等，重复删除或删除不存在的 ID 都返回 204 |

错误统一为 `{"error":"<code>"}`：`invalid_json`（400）、`unsupported_template`（400）、`not_found`（404）、`internal_error`（500）。

## 4. 沙箱状态

```
创建 ──► pending ──► ready ──► closing ──► （查询返回 404）
            │          │          ▲
            │          └──────────┤  DELETE
            ▼                     │
        terminated ───────────────┘
```

| 状态 | 含义 | kube 后端的判定 |
| --- | --- | --- |
| `pending` | 已创建，还不能用 | Pod 尚未 Running，或 Ready 条件不为 True |
| `ready` | 可以使用 | Pod Running 且 Ready=True |
| `terminated` | 自己结束了，不会再恢复 | Pod 阶段为 Failed 或 Succeeded（例如超过一小时寿命） |
| `closing` | 已接受删除，正在回收 | Pod 带有 deletionTimestamp |
| `simulated` | memory 后端专用，背后没有真实环境 | — |

两点需要注意：

- 状态**每次都从后端实时读取**，manager 自己不保存。好处是不会和真实情况不一致，代价是 manager 无法发现"创建到一半就中断"的残留对象（v3 解决）。
- 镜像拉取失败时 Pod 会一直停在 `pending`，所以 `WaitReady` 必须传带超时的 context。

## 5. 隔离基线（kube 后端）

每个沙箱 Pod 默认带上这些限制：

| 设置 | 作用 |
| --- | --- |
| 非 root 运行（UID 65534） | 降低容器逃逸后的权限 |
| 丢弃全部 capabilities、禁止提权 | 去掉不需要的内核特权 |
| seccomp `RuntimeDefault` | 过滤危险系统调用 |
| 不挂载 ServiceAccount 令牌 | 沙箱内拿不到集群 API 凭据 |
| CPU 1 核、内存 128Mi 上限 | 防止拖垮节点 |
| 最长运行一小时、不自动重启 | 防止泄漏的沙箱长期占用资源 |
| 只操作带管理标签且名称匹配的 Pod | manager 不会误删命名空间里的其他 Pod |

这是"普通容器"级别的隔离，和宿主机共享内核，**只适合运行自己信任的代码**。还缺出网限制、磁盘配额和更强的内核隔离，分别安排在 v3 和 v6。

## 6. v2 实现契约（规划）

生命周期仍由 manager 管理。每个 manager 启动时只选择 kube 或 docker；memory 继续用于模拟。两种真实后端运行**同一镜像、同一个 Go agent**。SDK 只接收沙箱 ID，不选择节点，不拼 Pod IP，不持有 kubeconfig 或 Docker socket。

| 边界 | kube | 本地 Linux docker |
| --- | --- | --- |
| Provider 的 Create / Get / Close | 创建 Pod，以标签和平台 ID 查找，以 Pod UID 标识实例 | 用 Docker Engine 创建容器，按标签和平台 ID 查找，以容器 ID 标识实例；删除只作用于受管容器 |
| 内部端点解析 | Pod IP + 模板指定的 agent 端口 | 仅供同机客户端直连时，将 agent 端口映射到宿主回环地址和动态端口 |
| 就绪 | readinessProbe 请求 agent 的 `/readyz` | 检查容器仍在运行并探测同一 `/readyz`；只有进程启动还不够 |

单独定义内部端点解析能力（例如 `ResolveEndpoint(ctx, sandboxID)`），返回实例身份和上游地址。公开的 `POST /v1/sandboxes/{id}/route` 再将其转换成 SDK 可访问的 `base_url`、短期凭据和过期时间。未就绪返回冲突，模拟后端明确不支持数据面。内部地址与公开地址不可混用。

- **直连：** SDK → agent。前提是实际运行 SDK 的机器能访问该地址；manager 签发绑定 sandbox ID、实例和有效期的凭据，agent 在执行和文件接口校验。不同实例不共用可签发其他实例凭据的密钥。
- **代理：** SDK → 共享 gateway → agent。gateway 只能按受信任的实例映射选择上游，不能接受调用方指定的 IP 或 URL。外部链路使用 TLS；gateway 校验凭据，agent 仍校验实例绑定。v2 先验证直连；首批调用方不能访问 Pod 网段时，代理必须在交付前可用。
- **联调：** port-forward 可帮助定位连通性问题；它依赖集群权限，不作为 SDK 的默认传输。

### 数据面最小协议（相对于 route.base_url）

| 接口 | 行为 |
| --- | --- |
| `POST /execs` | 接收 argv、工作区内的 cwd、执行超时，启动后返回 exec ID。不能因为提交响应丢失而自动重试有副作用的命令。 |
| `GET /execs/{id}/events?after=N` | HTTP 流按序返回 stdout、stderr 和含退出码的结束事件；输出缓存有限，游标已淘汰时明确报错。客户端断线仅结束订阅，不等于命令失败。 |
| `DELETE /execs/{id}` | 显式取消并回收该执行的进程；执行超时也要终止。等待请求的 context 取消与这个操作分开。 |
| `PUT /files/{path}`、`GET /files/{path}` | 在 workspace 内传输原始字节；拒绝越界路径和逃出目录的符号链接，限制大小，上传中断不留下半成品。 |

`pkg/client` 封装取路由及上述调用。路由到期可重新取；重连输出应带游标，不能重新启动命令。v2 限个人可信环境，项目级身份与配额仍属于 v3。

## 7. 目标架构

主流 Agent 沙箱（E2B、Modal、Daytona、Kubernetes Agent Sandbox 等）结构很相似，本项目沿用同样的分层：

```
              ┌────────────── 控制面：管"有哪些沙箱" ──────────────┐
SDK / CLI ──► manager ──► 数据库（意图、配额、租约）──► controller ──► provider
    │
    │         ┌────────────── 数据面：管"在沙箱里干活" ────────────┐
    └───────► 可选 gateway（校验短期凭据）──► 沙箱内 agent（执行命令、读写文件）
```

- **控制面**负责创建、查询、销毁，请求少、要求可靠。数据库记录"想要什么"，controller 持续把真实资源对齐到这个意图。
- **数据面**负责命令输出和文件传输，流量大、要求低延迟。它不经过 manager，这样大文件和长时间的日志流不会拖慢管理接口。
- **沙箱内 agent** 是一个常驻小进程，对外提供执行和文件接口。这样执行能力不依赖 `kubectl exec`，换成 microVM 等后端时上层协议不变。

### 从主流沙箱借鉴的设计

| 设计 | 谁在用 | 解决什么问题 | 本项目落在 |
| --- | --- | --- | --- |
| 沙箱内常驻 agent | E2B（envd）、Modal、Daytona | 统一的执行与文件协议，与底层隔离技术解耦 | v2 |
| 每次执行有独立 ID，输出带序号 | E2B、Modal | 断线后可续读输出；区分"连接断了"和"命令失败" | v2 |
| 超时自动销毁，可续期 | E2B、Modal、Daytona | Agent 崩溃或忘记释放时不泄漏资源 | v3（租约） |
| 限制出网，按白名单放行 | Claude Code 沙箱、Modal | 防止数据外泄和被提示注入后乱访问网络 | v3 |
| 模板 / 领取 / 预热池分离 | Kubernetes Agent Sandbox | 提前备好空闲实例，领取即用，缩短首条命令延迟 | v5 |
| 快照、暂停与恢复 | E2B、Modal、Daytona | 保留现场、按需唤醒，空闲时不占计算资源 | v6 之后，依赖 microVM 或 gVisor |
| 更强的内核隔离 | E2B / Vercel（Firecracker）、Modal（gVisor） | 运行不可信代码时不与宿主机共享内核 | v6 |

### 几条贯穿各版本的原则

1. **以真实资源为准。** 沙箱 ID 是平台身份，Pod UID 是某一次实例的身份；状态以后端观测为准，数据库只存意图。
2. **创建和删除都可重复。** 创建用幂等键去重，删除天然幂等，调用方可以放心重试。
3. **先确认再释放。** 确认远端对象确实消失后才归还配额；首次创建结果未知时不换集群重试，避免重复创建。
4. **用完即毁。** 沙箱关闭后直接销毁，不回收到池里给下一个用户，避免环境残留。
5. **换后端不换契约。** 任何新后端都要通过同一套就绪、执行、文件、回收测试。
