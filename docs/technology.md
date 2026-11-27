# 技术选型

原则：**每引入一个依赖，都要有一个只有它才能验证的验收用例。** 例如 PostgreSQL 对应"进程崩溃后恢复"，Kubernetes 对应"真实资源被清理干净"。在那之前保持零外部服务，改完代码几秒内就能跑完测试。

架构见[总体设计](design.md)，各版本范围见[迭代计划](iteration-plan.md)。

## 1. 当前选型

| 层次 | 现在用 | 以后换成 | 什么时候换 |
| --- | --- | --- | --- |
| 语言与 HTTP | Go 标准库 `net/http` + JSON | 暂不更换 | 执行协议稳定后再考虑 gRPC 等代码生成方案 |
| 状态存储 | 不存储：memory 用进程内 map，kube 直接查集群 | PostgreSQL | v3，需要配额、幂等键和租约的跨进程事务时 |
| 编排 | Kubernetes Pod，通过 `kubectl` 调用 | client-go + 共享 watch | v4，多集群且轮询请求量成为问题时 |
| 执行与文件 | 无（验收时借用 `kubectl exec`） | 沙箱内 Go agent + HTTP 流 | v2 |
| 测试 | `testing` + `httptest`，kubectl 用假实现替换 | 增加真实集群回归 | 涉及网络策略、进程回收等只能在 Linux 集群验证的能力时 |

两点说明：

- **为什么先用 kubectl 而不是 client-go。** 单集群阶段 kubectl 零依赖，出问题可以把同一条命令拿到终端里复现。代价是每次调用启动一个子进程，且只能轮询。换成 client-go 时两者不能同时写同一资源。
- **为什么不自己做调度。** 节点选择、镜像拉取、健康检查都交给 Kubernetes，项目只关注沙箱本身的生命周期。

## 2. 隔离技术怎么选

隔离强度和启动速度、兼容性之间需要取舍，目前主流是四类：

| 技术 | 原理 | 隔离强度 | 启动 | 兼容性 | 代表用户 |
| --- | --- | --- | --- | --- | --- |
| 普通容器（runc） | namespace + cgroup + seccomp，共享宿主内核 | 弱：内核漏洞可逃逸 | 快 | 最好 | Daytona、本项目 v1 |
| [gVisor](https://gvisor.dev/docs/) | 用户态内核拦截系统调用，极少直接接触宿主内核 | 较强 | 快 | 部分系统调用不支持，文件 I/O 较慢 | Modal |
| microVM（[Firecracker](https://github.com/firecracker-microvm/firecracker)、[Kata Containers](https://github.com/kata-containers/kata-containers)） | 每个沙箱有独立内核，靠硬件虚拟化隔离 | 强 | 冷启动较慢，配合快照可以很快 | 好（完整 Linux） | E2B、Vercel Sandbox |
| 进程级沙箱（bubblewrap、Seatbelt） | 直接限制本机进程能访问的文件和网络 | 取决于规则 | 几乎无开销 | 就是本机环境 | [Claude Code sandbox-runtime](https://github.com/anthropic-experimental/sandbox-runtime) |

本项目的路线：

1. **v1 用加固的普通容器。** 任何 Kubernetes 集群都能跑，足够验证生命周期。只运行自己信任的代码。
2. **v6 优先尝试 gVisor 或 Kata。** 两者在 Kubernetes 里都只是一个 `RuntimeClass`，Pod 清单加一个字段即可，kube provider 几乎不用改。先在已配置好运行时的节点上测系统调用兼容性、构建耗时和文件 I/O。
3. **Firecracker 需要独立的 provider。** 它要求支持 KVM 的 Linux 宿主，并且要自己管理镜像、网络和快照。有合适的机器后再做，在此之前不承诺 VM 级隔离和暂停恢复。
4. **进程级沙箱不在范围内。** 它解决的是"在开发者本机限制 Agent"，和本项目"远端托管环境"的方向不同。但它的网络模型值得借鉴：所有出网流量经过代理，按域名白名单放行。

## 3. 主流沙箱产品参照

| 产品 | 隔离方式 | 值得参考的设计 |
| --- | --- | --- |
| [E2B](https://github.com/e2b-dev/infra) | Firecracker microVM | 沙箱内守护进程 envd 提供执行与文件接口；模板由 Dockerfile 构建后做成快照；超时自动销毁、可续期；支持暂停与恢复 |
| [Modal Sandbox](https://modal.com/docs/guide/sandbox) | gVisor | 以 SDK 为中心的 `create` / `exec` 接口；文件系统快照；空闲超时；可限制出网 |
| [Daytona](https://github.com/daytonaio/daytona) | OCI 容器 | 面向 Agent 的进程、文件、Git 接口；停止、归档等多级休眠状态；自动停止间隔 |
| [Kubernetes Agent Sandbox](https://github.com/kubernetes-sigs/agent-sandbox) | Pod，可叠加 gVisor / Kata | 用 CRD 建模：`Sandbox`、`SandboxTemplate`、`SandboxClaim`、`SandboxWarmPool`，把模板、领取和预热池分开 |
| [Cloudflare Sandbox](https://developers.cloudflare.com/sandbox/) | 容器 | 每个沙箱绑定一个有状态对象作为入口，天然解决"请求路由到哪个实例" |
| [Vercel Sandbox](https://vercel.com/docs/vercel-sandbox) | Firecracker microVM | 短生命周期、按运行时长计费的一次性环境 |

共同点可以归纳为五条，本项目都会遵循：

1. **控制面和数据面分开。** 管理接口只负责生命周期，命令和文件直接走到沙箱。
2. **沙箱里放一个常驻 agent。** 上层协议不依赖底层是容器还是虚拟机。
3. **默认会过期。** 每个沙箱都有超时，靠续期保活，而不是靠用户记得删除。
4. **模板先构建好。** 启动快慢主要取决于镜像是否已在节点上，以及有没有现成的空闲实例或快照。
5. **出网可以收紧。** 都提供禁止出网或按白名单放行的手段；本项目更进一步，计划默认禁止。

与 Kubernetes Agent Sandbox 的关系：它和本项目目标最接近。本项目先自己写最小实现，弄清并发领取、预热池补充这些问题，到 v5 再评估是否直接采用它的 CRD。

## 4. v2 的具体选择

- **后端驱动：** kube 暂时沿用 kubectl 管生命周期；docker 用 Go 调用 Linux Docker Engine API，仅 manager 访问本机 socket。容器沿用非 root、禁止提权、CPU/内存限额等基线，绝不挂载 Docker socket。两边都不使用各自的 exec API 实现 SDK 命令；业务协议只有 agent 的一套 HTTP 接口。K8s 与 Docker 是两种供应方式，均可运行普通容器，v6 讨论的是更换隔离机制。
- **部署配置：** 生命周期由 `-provider=kube|docker` 选择；数据面另设访问模式（建议 `direct|gateway`）。同机 Docker 的回环映射不能发给远程 SDK；选择代理时，gateway 自己必须能访问上游。一个集群共用一个 gateway，无需为每个 Pod 分配公网入口。
- **镜像内容：** 新增 `cmd/agent` 和模板 Dockerfile，把 agent 作为容器入口，约定工作目录、非 root 用户、监听端口和健康端点。agent 直接打进模板，同一个镜像在两个后端启动；init 容器不作为跨后端前提。
- **镜像放置：** Git 只存 Dockerfile、agent 源码和模板清单；OCI 镜像推到已有且两边可达的 registry（例如团队 Harbor）。日常发布可打版本 tag，验收模板固定到实际的 `仓库/镜像@sha256:摘要`。架构不同时发布多架构索引，并在 amd64/arm64 各自验证对应镜像。镜像拉取凭据分别配置在 Docker 宿主与 K8s imagePullSecrets，不注入工作负载。
- **开发捷径：** 可以先本地 build，kind 场景可显式导入；本机镜像缓存不是远端节点的分发机制。无需在 v2 开发 registry、镜像构建服务或运行时热池。

一个模板例子（以下为拟新增字段，摘要由实际发布流程填写）：

```yaml
name: linux
image: registry.example.com/agent-env/linux-basic@sha256:<digest>
agentPort: 8081
workspace: /workspace
```

设计参照：[OpenSandbox](https://github.com/opensandbox-group/OpenSandbox) 的共同协议与双后端、[Kubernetes Agent Sandbox](https://agent-sandbox.sigs.k8s.io/docs/python-client/) 的多种连接路径、[E2B runtime](https://github.com/e2b-dev/runtime/blob/main/docs/ARCHITECTURE.md) 的节点路由与 guest agent。它们分别回答后端、连通性和执行边界三个问题。

## 5. 后续版本再决定

| 问题 | 倾向 | 在哪个版本决定 |
| --- | --- | --- |
| 出网限制用什么实现 | 先用 NetworkPolicy 默认拒绝；需要按域名放行时再加出口代理 | v3 |
| 预热池自己写还是用 CRD | 先写固定水位的最小池，再与 Agent Sandbox 对比 | v5 |
