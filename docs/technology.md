# 技术选型与取舍

v1 更新：单集群 provider 先通过 kubectl 调用 Kubernetes API，以最少依赖验证冷创建。下表的 client-go 是后续多集群与共享 watch 的目标选型；具体版本与验收见[迭代计划](iteration-plan.md)。

| 层次 | 选择与状态 | 原因与替换条件 |
| --- | --- | --- |
| 语言与 HTTP | 已用 Go、标准库 net/http、JSON | 同一种语言编写管理服务和节点 agent；1.22 路由足够支撑首版。协议稳定前不引入 RPC 代码生成。 |
| 状态 | 已用内存 map + RWMutex；计划 PostgreSQL | 内存适合单进程契约测试，不提供持久性。引入配额、幂等、租约时需要跨进程事务；届时分离状态仓库和真实 provider，避免让 Pod 创建承担数据库写入职责。 |
| 编排 | 计划 kind + client-go | 先验证单集群，复用 Kubernetes 的 Pod 生命周期和调度；通过共享 watch 观察就绪，并处理断线重列。首版不自建节点调度器。 |
| 执行与文件 | 计划 Go agent + HTTP 字节流 | 在 workload 内执行 argv；文本日志和二进制文件分开。先验证限时、取消、目录边界和输出上限，再增加交互式终端。 |
| 测试与指标 | 已用 testing、httptest；计划真实集群回归 | HTTP 契约测试不依赖 Docker；Pod 生命周期、网络策略和进程回收必须在 Linux 集群验证。分开测 SDK 等待、provider 耗时与执行耗时。 |

## 沙箱技术参照

- [Kubernetes Agent Sandbox](https://github.com/kubernetes-sigs/agent-sandbox)：参考模板、领取和预热池的资源建模。个人项目先写最小池，理解控制器如何处理并发领取，再评估采用 CRD。
- [gVisor](https://gvisor.dev/docs/)：作为容器后端的隔离候选。在已配置对应运行时的节点验证系统调用兼容性、构建耗时和文件 I/O，再决定哪些模板启用。
- [Firecracker](https://github.com/firecracker-microvm/firecracker) 与 [E2B runtime](https://github.com/e2b-dev/runtime)：分别参考 KVM microVM 和利用快照交付 Agent 环境的工程方式。有合适 Linux 宿主后再开发 provider，不给当前内存或未来普通 Pod 后端承诺 VM 隔离、暂停恢复语义。

目前选择零外部服务启动，以缩短学习反馈周期。引入每个依赖时都应附一个只有它才能验证的验收用例，例如 PostgreSQL 对应进程崩溃恢复，Kubernetes 对应真实资源清理。
